package repository

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type Order struct {
	ID, UserID                  uuid.UUID
	Status                      string
	PromoCodeID                 *uuid.UUID
	TotalAmount, DiscountAmount float64
	CreatedAt, UpdatedAt        time.Time
}
type OrderItem struct {
	ID, OrderID, ProductID uuid.UUID
	Quantity               int
	PriceAtOrder           float64
}
type Operation struct {
	ID, UserID uuid.UUID
	Type       string
	CreatedAt  time.Time
}

type OrderRepository struct{}

func (r OrderRepository) GetByID(ctx context.Context, q Querier, id uuid.UUID) (Order, error) {
	var o Order
	err := q.QueryRow(ctx, `SELECT id,user_id,status,promo_code_id,total_amount,discount_amount,created_at,updated_at FROM orders WHERE id=$1`, id).Scan(&o.ID, &o.UserID, &o.Status, &o.PromoCodeID, &o.TotalAmount, &o.DiscountAmount, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func (r OrderRepository) GetByIDForUpdate(ctx context.Context, q Querier, id uuid.UUID) (Order, error) {
	var o Order
	err := q.QueryRow(ctx, `SELECT id,user_id,status,promo_code_id,total_amount,discount_amount,created_at,updated_at FROM orders WHERE id=$1 FOR UPDATE`, id).Scan(&o.ID, &o.UserID, &o.Status, &o.PromoCodeID, &o.TotalAmount, &o.DiscountAmount, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func (r OrderRepository) GetActiveByUser(ctx context.Context, q Querier, userID uuid.UUID) (Order, error) {
	var o Order
	err := q.QueryRow(ctx, `SELECT id,user_id,status,promo_code_id,total_amount,discount_amount,created_at,updated_at FROM orders WHERE user_id=$1 AND status IN ('CREATED','PAYMENT_PENDING') LIMIT 1`, userID).Scan(&o.ID, &o.UserID, &o.Status, &o.PromoCodeID, &o.TotalAmount, &o.DiscountAmount, &o.CreatedAt, &o.UpdatedAt)
	return o, err
}
func (r OrderRepository) LastOperation(ctx context.Context, q Querier, userID uuid.UUID, typ string) (time.Time, error) {
	var t time.Time
	err := q.QueryRow(ctx, `SELECT created_at FROM user_operations WHERE user_id=$1 AND operation_type=$2 ORDER BY created_at DESC LIMIT 1`, userID, typ).Scan(&t)
	return t, err
}
func (r OrderRepository) ListItems(ctx context.Context, q Querier, orderID uuid.UUID) ([]OrderItem, error) {
	rows, err := q.Query(ctx, `SELECT id,order_id,product_id,quantity,price_at_order FROM order_items WHERE order_id=$1 ORDER BY id`, orderID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OrderItem
	for rows.Next() {
		var i OrderItem
		if err := rows.Scan(&i.ID, &i.OrderID, &i.ProductID, &i.Quantity, &i.PriceAtOrder); err != nil {
			return nil, err
		}
		out = append(out, i)
	}
	return out, rows.Err()
}
func (r OrderRepository) Create(ctx context.Context, q Querier, o Order) error {
	_, err := q.Exec(ctx, `INSERT INTO orders(id,user_id,status,promo_code_id,total_amount,discount_amount) VALUES($1,$2,$3,$4,$5,$6)`, o.ID, o.UserID, o.Status, o.PromoCodeID, o.TotalAmount, o.DiscountAmount)
	return err
}
func (r OrderRepository) AddItem(ctx context.Context, q Querier, i OrderItem) error {
	_, err := q.Exec(ctx, `INSERT INTO order_items(id,order_id,product_id,quantity,price_at_order) VALUES($1,$2,$3,$4,$5)`, i.ID, i.OrderID, i.ProductID, i.Quantity, i.PriceAtOrder)
	return err
}
func (r OrderRepository) DeleteItems(ctx context.Context, q Querier, orderID uuid.UUID) error {
	_, err := q.Exec(ctx, `DELETE FROM order_items WHERE order_id=$1`, orderID)
	return err
}
func (r OrderRepository) Update(ctx context.Context, q Querier, o Order) error {
	_, err := q.Exec(ctx, `UPDATE orders SET status=$2,promo_code_id=$3,total_amount=$4,discount_amount=$5 WHERE id=$1`, o.ID, o.Status, o.PromoCodeID, o.TotalAmount, o.DiscountAmount)
	return err
}
func (r OrderRepository) AddOperation(ctx context.Context, q Querier, userID uuid.UUID, typ string) error {
	_, err := q.Exec(ctx, `INSERT INTO user_operations(user_id,operation_type) VALUES($1,$2)`, userID, typ)
	return err
}
