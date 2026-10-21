package service

import (
	"context"
	"net/http"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/repository"
)

const (
	opCreateOrder = "CREATE_ORDER"
	opUpdateOrder = "UPDATE_ORDER"
	maxPercentDiscount = 70
)

var allowedTransitions = map[api.OrderStatus][]api.OrderStatus{
	api.CREATED:        {api.PAYMENTPENDING, api.CANCELED},
	api.PAYMENTPENDING: {api.PAID, api.CANCELED},
	api.PAID:           {api.SHIPPED},
	api.SHIPPED:        {api.COMPLETED},
}

func checkTransition(from, to api.OrderStatus) error {
	if slices.Contains(allowedTransitions[from], to) {
		return nil
	}
	return apperr.New(http.StatusConflict, api.INVALIDSTATETRANSITION, "invalid order state transition",
		map[string]any{"from": from, "to": to})
}

type ItemInput struct {
	ProductID uuid.UUID
	Quantity  int
}

type OrderView struct {
	repository.Order
	Items     []repository.OrderItem
	PromoCode *string
}

type OrderService struct {
	DB       *repository.DB
	Orders   repository.OrderRepository
	Products repository.ProductRepository
	Promos   repository.PromoRepository
	Users    repository.UserRepository
	Limit    time.Duration
	Now      func() time.Time
}

func NewOrderService(db *repository.DB, limit time.Duration) *OrderService {
	return &OrderService{
		DB: db, Orders: repository.OrderRepository{}, Products: repository.ProductRepository{},
		Promos: repository.PromoRepository{}, Users: repository.UserRepository{}, Limit: limit, Now: time.Now,
	}
}

func (s *OrderService) Get(ctx context.Context, actor Actor, id uuid.UUID) (OrderView, error) {
	order, err := s.Orders.GetByID(ctx, s.DB.Pool, id)
	if err != nil {
		return OrderView{}, orderLookupError(err)
	}
	if err := checkOwnership(actor, order); err != nil {
		return OrderView{}, err
	}
	return s.load(ctx, s.DB.Pool, id)
}

func (s *OrderService) Create(ctx context.Context, actor Actor, items []ItemInput, promoCode *string) (OrderView, error) {
	var view OrderView
	err := s.DB.InTx(ctx, func(tx pgx.Tx) error {
		if err := s.Users.LockByID(ctx, tx, actor.UserID); err != nil {
			return err
		}
		if err := s.checkRateLimit(ctx, tx, actor.UserID, opCreateOrder); err != nil {
			return err
		}
		if _, err := s.Orders.GetActiveByUser(ctx, tx, actor.UserID); err == nil {
			return apperr.New(http.StatusConflict, api.ORDERHASACTIVE, "user already has an active order", nil)
		} else if !repository.IsNotFound(err) {
			return err
		}
		products, err := s.lockProducts(ctx, tx, productIDs(items))
		if err != nil {
			return err
		}
		order := repository.Order{ID: uuid.New(), UserID: actor.UserID, Status: string(api.CREATED)}
		orderItems, subtotal, err := s.reserve(ctx, tx, products, order.ID, items)
		if err != nil {
			return err
		}
		discount := int64(0)
		if promoCode != nil {
			promo, err := s.Promos.GetForUpdateByCode(ctx, tx, *promoCode)
			if repository.IsNotFound(err) {
				return promoInvalid("promo code not found")
			}
			if err != nil {
				return err
			}
			if err := s.checkPromoUsable(promo, true); err != nil {
				return err
			}
			if subtotal < toCents(promo.MinOrderAmount) {
				return apperr.New(http.StatusUnprocessableEntity, api.PROMOCODEMINAMOUNT, "order amount is below promo code minimum",
					map[string]any{"min_order_amount": promo.MinOrderAmount, "order_amount": fromCents(subtotal)})
			}
			discount = calcDiscount(promo, subtotal)
			if err := s.Promos.IncrementUses(ctx, tx, promo.ID); err != nil {
				return err
			}
			order.PromoCodeID = &promo.ID
		}
		order.DiscountAmount = fromCents(discount)
		order.TotalAmount = fromCents(subtotal - discount)

		if err := s.Orders.Create(ctx, tx, order); err != nil {
			return err
		}
		for _, item := range orderItems {
			if err := s.Orders.AddItem(ctx, tx, item); err != nil {
				return err
			}
		}
		if err := s.Orders.AddOperation(ctx, tx, actor.UserID, opCreateOrder); err != nil {
			return err
		}
		view, err = s.load(ctx, tx, order.ID)
		return err
	})
	return view, err
}

func (s *OrderService) Update(ctx context.Context, actor Actor, id uuid.UUID, items []ItemInput) (OrderView, error) {
	var view OrderView
	err := s.DB.InTx(ctx, func(tx pgx.Tx) error {
		order, err := s.Orders.GetByIDForUpdate(ctx, tx, id)
		if err != nil {
			return orderLookupError(err)
		}
		if err := checkOwnership(actor, order); err != nil {
			return err
		}

		if api.OrderStatus(order.Status) != api.CREATED {
			return apperr.New(http.StatusConflict, api.INVALIDSTATETRANSITION, "order can be updated only in CREATED state",
				map[string]any{"status": order.Status})
		}
		if err := s.Users.LockByID(ctx, tx, actor.UserID); err != nil {
			return err
		}
		if err := s.checkRateLimit(ctx, tx, actor.UserID, opUpdateOrder); err != nil {
			return err
		}
		oldItems, err := s.Orders.ListItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		ids := productIDs(items)
		for _, item := range oldItems {
			ids = append(ids, item.ProductID)
		}
		products, err := s.lockProducts(ctx, tx, ids)
		if err != nil {
			return err
		}
		if err := s.returnStock(ctx, tx, products, oldItems); err != nil {
			return err
		}
		if err := s.Orders.DeleteItems(ctx, tx, order.ID); err != nil {
			return err
		}
		newItems, subtotal, err := s.reserve(ctx, tx, products, order.ID, items)
		if err != nil {
			return err
		}
		for _, item := range newItems {
			if err := s.Orders.AddItem(ctx, tx, item); err != nil {
				return err
			}
		}
		discount := int64(0)
		if order.PromoCodeID != nil {
			promo, err := s.Promos.GetForUpdateByID(ctx, tx, *order.PromoCodeID)
			if err != nil {
				return err
			}
			if s.checkPromoUsable(promo, false) == nil && subtotal >= toCents(promo.MinOrderAmount) {
				discount = calcDiscount(promo, subtotal)
			} else {
				if err := s.Promos.DecrementUses(ctx, tx, promo.ID); err != nil {
					return err
				}
				order.PromoCodeID = nil
			}
		}
		order.DiscountAmount = fromCents(discount)
		order.TotalAmount = fromCents(subtotal - discount)
		if err := s.Orders.Update(ctx, tx, order); err != nil {
			return err
		}
		if err := s.Orders.AddOperation(ctx, tx, actor.UserID, opUpdateOrder); err != nil {
			return err
		}
		view, err = s.load(ctx, tx, order.ID)
		return err
	})
	return view, err
}

func (s *OrderService) Cancel(ctx context.Context, actor Actor, id uuid.UUID) (OrderView, error) {
	var view OrderView
	err := s.DB.InTx(ctx, func(tx pgx.Tx) error {
		order, err := s.Orders.GetByIDForUpdate(ctx, tx, id)
		if err != nil {
			return orderLookupError(err)
		}
		if err := checkOwnership(actor, order); err != nil {
			return err
		}
		if err := checkTransition(api.OrderStatus(order.Status), api.CANCELED); err != nil {
			return err
		}
		items, err := s.Orders.ListItems(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		ids := make([]uuid.UUID, 0, len(items))
		for _, item := range items {
			ids = append(ids, item.ProductID)
		}
		products, err := s.lockProducts(ctx, tx, ids)
		if err != nil {
			return err
		}
		if err := s.returnStock(ctx, tx, products, items); err != nil {
			return err
		}
		if order.PromoCodeID != nil {
			if err := s.Promos.DecrementUses(ctx, tx, *order.PromoCodeID); err != nil {
				return err
			}
		}
		order.Status = string(api.CANCELED)
		if err := s.Orders.Update(ctx, tx, order); err != nil {
			return err
		}
		view, err = s.load(ctx, tx, order.ID)
		return err
	})
	return view, err
}

func (s *OrderService) checkRateLimit(ctx context.Context, q repository.Querier, userID uuid.UUID, op string) error {
	last, err := s.Orders.LastOperation(ctx, q, userID, op)
	if repository.IsNotFound(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if elapsed := s.Now().Sub(last); elapsed < s.Limit {
		retryAfter := (s.Limit - elapsed).Round(time.Second)
		return apperr.New(http.StatusTooManyRequests, api.ORDERLIMITEXCEEDED, "too many order operations, try later",
			map[string]any{"operation": op, "retry_after_seconds": int(retryAfter.Seconds())})
	}
	return nil
}

func (s *OrderService) lockProducts(ctx context.Context, q repository.Querier, ids []uuid.UUID) (map[uuid.UUID]*repository.Product, error) {
	ids = slices.Clone(ids)
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return slices.Compare(a[:], b[:]) })
	ids = slices.Compact(ids)
	products := make(map[uuid.UUID]*repository.Product, len(ids))
	for _, id := range ids {
		p, err := s.Products.GetForUpdate(ctx, q, id)
		if repository.IsNotFound(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		products[id] = &p
	}
	return products, nil
}

func (s *OrderService) reserve(ctx context.Context, q repository.Querier, products map[uuid.UUID]*repository.Product, orderID uuid.UUID, items []ItemInput) ([]repository.OrderItem, int64, error) {
	requested := make(map[uuid.UUID]int, len(items))
	var order []uuid.UUID
	for _, item := range items {
		p, ok := products[item.ProductID]
		if !ok {
			return nil, 0, productNotFound(item.ProductID)
		}
		if api.ProductStatus(p.Status) != api.ACTIVE {
			return nil, 0, apperr.New(http.StatusConflict, api.PRODUCTINACTIVE, "product is not active",
				map[string]any{"product_id": item.ProductID, "status": p.Status})
		}
		if _, seen := requested[item.ProductID]; !seen {
			order = append(order, item.ProductID)
		}
		requested[item.ProductID] += item.Quantity
	}
	var shortages []map[string]any
	for _, id := range order {
		if available := products[id].Stock; available < requested[id] {
			shortages = append(shortages, map[string]any{"product_id": id, "requested": requested[id], "available": available})
		}
	}
	if len(shortages) > 0 {
		return nil, 0, apperr.New(http.StatusConflict, api.INSUFFICIENTSTOCK, "insufficient stock", map[string]any{"items": shortages})
	}
	for _, id := range order {
		if err := s.Products.UpdateStock(ctx, q, id, -requested[id]); err != nil {
			return nil, 0, err
		}
		products[id].Stock -= requested[id]
	}
	out := make([]repository.OrderItem, 0, len(items))
	var subtotal int64
	for _, item := range items {
		price := products[item.ProductID].Price
		subtotal += toCents(price) * int64(item.Quantity)
		out = append(out, repository.OrderItem{ID: uuid.New(), OrderID: orderID, ProductID: item.ProductID, Quantity: item.Quantity, PriceAtOrder: price})
	}
	return out, subtotal, nil
}

func (s *OrderService) returnStock(ctx context.Context, q repository.Querier, products map[uuid.UUID]*repository.Product, items []repository.OrderItem) error {
	for _, item := range items {
		if err := s.Products.UpdateStock(ctx, q, item.ProductID, item.Quantity); err != nil {
			return err
		}
		if p, ok := products[item.ProductID]; ok {
			p.Stock += item.Quantity
		}
	}
	return nil
}

func (s *OrderService) checkPromoUsable(p repository.PromoCode, newUse bool) error {
	now := s.Now()
	switch {
	case !p.Active:
		return promoInvalid("promo code is not active")
	case now.Before(p.ValidFrom) || now.After(p.ValidUntil):
		return promoInvalid("promo code is expired or not yet valid")
	case newUse && p.CurrentUses >= p.MaxUses:
		return promoInvalid("promo code usage limit reached")
	}
	return nil
}

func calcDiscount(p repository.PromoCode, totalCents int64) int64 {
	switch api.DiscountType(p.DiscountType) {
	case api.PERCENTAGE:
		if p.DiscountValue > maxPercentDiscount {
			return 0
		}
		return (totalCents*toCents(p.DiscountValue) + 5000) / 10000
	case api.FIXEDAMOUNT:
		return min(toCents(p.DiscountValue), totalCents)
	}
	return 0
}

func (s *OrderService) load(ctx context.Context, q repository.Querier, id uuid.UUID) (OrderView, error) {
	order, err := s.Orders.GetByID(ctx, q, id)
	if err != nil {
		return OrderView{}, orderLookupError(err)
	}
	items, err := s.Orders.ListItems(ctx, q, id)
	if err != nil {
		return OrderView{}, err
	}
	view := OrderView{Order: order, Items: items}
	if order.PromoCodeID != nil {
		promo, err := s.Promos.GetByID(ctx, q, *order.PromoCodeID)
		if err != nil {
			return OrderView{}, err
		}
		view.PromoCode = &promo.Code
	}
	return view, nil
}

func checkOwnership(actor Actor, order repository.Order) error {
	if actor.IsAdmin() || order.UserID == actor.UserID {
		return nil
	}
	return apperr.New(http.StatusForbidden, api.ORDEROWNERSHIPVIOLATION, "order belongs to another user", nil)
}

func orderLookupError(err error) error {
	if repository.IsNotFound(err) {
		return apperr.New(http.StatusNotFound, api.ORDERNOTFOUND, "order not found", nil)
	}
	return err
}

func promoInvalid(message string) *apperr.Error {
	return apperr.New(http.StatusUnprocessableEntity, api.PROMOCODEINVALID, message, nil)
}

func productIDs(items []ItemInput) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(items))
	for _, item := range items {
		ids = append(ids, item.ProductID)
	}
	return ids
}
