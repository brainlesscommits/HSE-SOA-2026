package service

import (
	"math"
	"net/http"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
)

const (
	RoleUser   = "USER"
	RoleSeller = "SELLER"
	RoleAdmin  = "ADMIN"
)

type Actor struct {
	UserID uuid.UUID
	Role   string
}

func (a Actor) IsAdmin() bool { return a.Role == RoleAdmin }

func toCents(v float64) int64      { return int64(math.Round(v * 100)) }
func fromCents(c int64) float64    { return float64(c) / 100 }
func roundMoney(v float64) float64 { return fromCents(toCents(v)) }

func productNotFound(id uuid.UUID) *apperr.Error {
	return apperr.New(http.StatusNotFound, api.PRODUCTNOTFOUND, "product not found", map[string]any{"product_id": id})
}
