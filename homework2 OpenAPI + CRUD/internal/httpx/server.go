package httpx

import (
	"encoding/json"
	"errors"
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/auth"
	"marketplace/internal/middleware"
	"marketplace/internal/service"
)

type Server struct {
	Auth     *service.AuthService
	Tokens   *auth.Service
	Products *service.ProductService
	Orders   *service.OrderService
	Promos   *service.PromoService
}

var _ api.ServerInterface = (*Server)(nil)

func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, api.HealthResponse{Status: "ok"})
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func decode(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		apperr.Write(w, apperr.ValidationField("body", "invalid JSON: "+err.Error()))
		return false
	}
	return true
}

func actor(w http.ResponseWriter, r *http.Request, roles ...string) (service.Actor, bool) {
	identity, ok := middleware.IdentityFrom(r.Context())
	if !ok {
		apperr.Write(w, apperr.New(http.StatusUnauthorized, api.TOKENINVALID, "missing bearer token", nil))
		return service.Actor{}, false
	}
	a := service.Actor{UserID: identity.UserID, Role: identity.Role}
	for _, role := range roles {
		if role == a.Role {
			return a, true
		}
	}
	apperr.Write(w, apperr.AccessDenied())
	return service.Actor{}, false
}

func ParamError(w http.ResponseWriter, _ *http.Request, err error) {
	field := "parameter"
	var formatErr *api.InvalidParamFormatError
	var requiredErr *api.RequiredParamError
	switch {
	case errors.As(err, &formatErr):
		field = formatErr.ParamName
	case errors.As(err, &requiredErr):
		field = requiredErr.ParamName
	}
	apperr.Write(w, apperr.ValidationField(field, err.Error()))
}
