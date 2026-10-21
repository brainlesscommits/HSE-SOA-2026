package middleware

import (
	"errors"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/auth"
)

func JWT(tokens *auth.Service) api.MiddlewareFunc {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Context().Value(api.BearerAuthScopes) == nil {
				next.ServeHTTP(w, r)
				return
			}
			header := r.Header.Get("Authorization")
			token, ok := strings.CutPrefix(header, "Bearer ")
			if !ok || strings.TrimSpace(token) == "" {
				apperr.Write(w, apperr.New(http.StatusUnauthorized, api.TOKENINVALID, "missing bearer token", nil))
				return
			}
			claims, err := tokens.ParseAccessToken(strings.TrimSpace(token))
			if errors.Is(err, auth.ErrTokenExpired) {
				apperr.Write(w, apperr.New(http.StatusUnauthorized, api.TOKENEXPIRED, "access token expired", nil))
				return
			}
			userID, parseErr := uuid.Parse(claims.UserID)
			if err != nil || parseErr != nil {
				apperr.Write(w, apperr.New(http.StatusUnauthorized, api.TOKENINVALID, "invalid access token", nil))
				return
			}
			ctx := WithIdentity(r.Context(), Identity{UserID: userID, Role: claims.Role})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
