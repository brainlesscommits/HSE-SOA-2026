package httpx

import (
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/repository"
	"marketplace/internal/service"
)

func (s *Server) CreatePromoCode(w http.ResponseWriter, r *http.Request) {
	a, ok := actor(w, r, service.RoleSeller, service.RoleAdmin)
	if !ok {
		return
	}
	var req api.PromoCodeCreate
	if !decode(w, r, &req) {
		return
	}
	active := true
	if req.Active != nil {
		active = *req.Active
	}
	p, err := s.Promos.Create(r.Context(), a, repository.PromoCode{
		Code: req.Code, DiscountType: string(req.DiscountType), DiscountValue: req.DiscountValue,
		MinOrderAmount: req.MinOrderAmount, MaxUses: req.MaxUses,
		ValidFrom: req.ValidFrom, ValidUntil: req.ValidUntil, Active: active,
	})
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, api.PromoCodeResponse{
		Id: p.ID, Code: p.Code, DiscountType: api.DiscountType(p.DiscountType), DiscountValue: p.DiscountValue,
		MinOrderAmount: p.MinOrderAmount, MaxUses: p.MaxUses, CurrentUses: p.CurrentUses,
		ValidFrom: p.ValidFrom, ValidUntil: p.ValidUntil, Active: p.Active,
	})
}
