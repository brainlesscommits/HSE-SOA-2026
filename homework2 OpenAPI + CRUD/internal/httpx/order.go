package httpx

import (
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/service"
)

func (s *Server) CreateOrder(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r, service.RoleUser, service.RoleAdmin)
	if !ok {
		return
	}
	var req api.OrderCreate
	if !decode(w, r, &req) {
		return
	}
	order, err := s.Orders.Create(r.Context(), a, toItemInputs(req.Items), req.PromoCode)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, toOrderResponse(order))
}

func (s *Server) GetOrder(w http.ResponseWriter, r *http.Request, id api.OrderId) {
	a, ok := actor(w, r, service.RoleUser, service.RoleAdmin)
	if !ok {
		return
	}
	order, err := s.Orders.Get(r.Context(), a, id)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(order))
}

func (s *Server) UpdateOrder(w http.ResponseWriter, r *http.Request, id api.OrderId) {
	a, ok := actor(w, r, service.RoleUser, service.RoleAdmin)
	if !ok {
		return
	}
	var req api.OrderUpdate
	if !decode(w, r, &req) {
		return
	}
	order, err := s.Orders.Update(r.Context(), a, id, toItemInputs(req.Items))
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(order))
}

func (s *Server) CancelOrder(w http.ResponseWriter, r *http.Request, id api.OrderId) {
	a, ok := actor(w, r, service.RoleUser, service.RoleAdmin)
	if !ok {
		return
	}
	order, err := s.Orders.Cancel(r.Context(), a, id)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toOrderResponse(order))
}

func toItemInputs(items []api.OrderItem) []service.ItemInput {
	out := make([]service.ItemInput, 0, len(items))
	for _, item := range items {
		out = append(out, service.ItemInput{ProductID: item.ProductId, Quantity: item.Quantity})
	}
	return out
}

func toOrderResponse(o service.OrderView) api.OrderResponse {
	resp := api.OrderResponse{
		Id: o.ID, UserId: o.UserID, Status: api.OrderStatus(o.Status), PromoCode: o.PromoCode,
		TotalAmount: o.TotalAmount, DiscountAmount: o.DiscountAmount,
		Items:     make([]api.OrderItemResponse, 0, len(o.Items)),
		CreatedAt: o.CreatedAt, UpdatedAt: o.UpdatedAt,
	}
	for _, item := range o.Items {
		resp.Items = append(resp.Items, api.OrderItemResponse{
			Id: item.ID, ProductId: item.ProductID, Quantity: item.Quantity, PriceAtOrder: item.PriceAtOrder,
		})
	}
	return resp
}
