package repository

import (
	"context"
	"github.com/google/uuid"
	"time"
)

type PromoCode struct {
	ID                            uuid.UUID
	Code, DiscountType            string
	DiscountValue, MinOrderAmount float64
	MaxUses, CurrentUses          int
	ValidFrom, ValidUntil         time.Time
	Active                        bool
}

type PromoRepository struct{}

func (r PromoRepository) Create(ctx context.Context, q Querier, p PromoCode) (PromoCode, error) {
	var out PromoCode
	err := q.QueryRow(ctx, `INSERT INTO promo_codes(id,code,discount_type,discount_value,min_order_amount,max_uses,valid_from,valid_until,active) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id,code,discount_type,discount_value,min_order_amount,max_uses,current_uses,valid_from,valid_until,active`, p.ID, p.Code, p.DiscountType, p.DiscountValue, p.MinOrderAmount, p.MaxUses, p.ValidFrom, p.ValidUntil, p.Active).Scan(&out.ID, &out.Code, &out.DiscountType, &out.DiscountValue, &out.MinOrderAmount, &out.MaxUses, &out.CurrentUses, &out.ValidFrom, &out.ValidUntil, &out.Active)
	return out, err
}

func (r PromoRepository) GetForUpdateByCode(ctx context.Context, q Querier, code string) (PromoCode, error) {
	var p PromoCode
	err := q.QueryRow(ctx, `SELECT id,code,discount_type,discount_value,min_order_amount,max_uses,current_uses,valid_from,valid_until,active FROM promo_codes WHERE code=$1 FOR UPDATE`, code).Scan(&p.ID, &p.Code, &p.DiscountType, &p.DiscountValue, &p.MinOrderAmount, &p.MaxUses, &p.CurrentUses, &p.ValidFrom, &p.ValidUntil, &p.Active)
	return p, err
}
func (r PromoRepository) GetForUpdateByID(ctx context.Context, q Querier, id uuid.UUID) (PromoCode, error) {
	var p PromoCode
	err := q.QueryRow(ctx, `SELECT id,code,discount_type,discount_value,min_order_amount,max_uses,current_uses,valid_from,valid_until,active FROM promo_codes WHERE id=$1 FOR UPDATE`, id).Scan(&p.ID, &p.Code, &p.DiscountType, &p.DiscountValue, &p.MinOrderAmount, &p.MaxUses, &p.CurrentUses, &p.ValidFrom, &p.ValidUntil, &p.Active)
	return p, err
}
func (r PromoRepository) IncrementUses(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE promo_codes SET current_uses=current_uses+1 WHERE id=$1`, id)
	return err
}
func (r PromoRepository) DecrementUses(ctx context.Context, q Querier, id uuid.UUID) error {
	_, err := q.Exec(ctx, `UPDATE promo_codes SET current_uses=GREATEST(current_uses-1,0) WHERE id=$1`, id)
	return err
}
func (r PromoRepository) GetByID(ctx context.Context, q Querier, id uuid.UUID) (PromoCode, error) {
	var p PromoCode
	err := q.QueryRow(ctx, `SELECT id,code,discount_type,discount_value,min_order_amount,max_uses,current_uses,valid_from,valid_until,active FROM promo_codes WHERE id=$1`, id).Scan(&p.ID, &p.Code, &p.DiscountType, &p.DiscountValue, &p.MinOrderAmount, &p.MaxUses, &p.CurrentUses, &p.ValidFrom, &p.ValidUntil, &p.Active)
	return p, err
}
