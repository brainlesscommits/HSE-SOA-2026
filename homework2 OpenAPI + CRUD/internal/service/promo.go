package service

import (
	"context"
	"net/http"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/repository"
)

type PromoService struct {
	DB   *repository.DB
	Repo repository.PromoRepository
}

func NewPromoService(db *repository.DB) *PromoService {
	return &PromoService{DB: db, Repo: repository.PromoRepository{}}
}

func (s *PromoService) Create(ctx context.Context, actor Actor, p repository.PromoCode) (repository.PromoCode, error) {
	if actor.Role != RoleSeller && actor.Role != RoleAdmin {
		return repository.PromoCode{}, apperr.AccessDenied()
	}
	if p.ValidUntil.Before(p.ValidFrom) {
		return repository.PromoCode{}, apperr.ValidationField("valid_until", "must be greater than or equal to valid_from")
	}
	if p.DiscountType == string(api.PERCENTAGE) && p.DiscountValue > 100 {
		return repository.PromoCode{}, apperr.ValidationField("discount_value", "percentage discount must be <= 100")
	}
	p.ID = uuid.New()
	p.DiscountValue = roundMoney(p.DiscountValue)
	p.MinOrderAmount = roundMoney(p.MinOrderAmount)
	out, err := s.Repo.Create(ctx, s.DB.Pool, p)
	if repository.IsUniqueViolation(err) {
		return out, apperr.New(http.StatusConflict, api.PROMOCODEEXISTS, "promo code already exists", map[string]any{"code": p.Code})
	}
	return out, err
}
