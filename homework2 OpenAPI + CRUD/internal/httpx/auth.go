package httpx

import (
	"net/http"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/service"
)

func (s *Server) Register(w http.ResponseWriter, r *http.Request) {
	var req api.RegisterRequest
	if !decode(w, r, &req) {
		return
	}
	pair, err := s.Auth.Register(r.Context(), string(req.Email), req.Password, string(req.Role))
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, s.authResponse(pair))
}

func (s *Server) Login(w http.ResponseWriter, r *http.Request) {
	var req api.LoginRequest
	if !decode(w, r, &req) {
		return
	}
	pair, err := s.Auth.Login(r.Context(), string(req.Email), req.Password)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.authResponse(pair))
}

func (s *Server) Refresh(w http.ResponseWriter, r *http.Request) {
	var req api.RefreshRequest
	if !decode(w, r, &req) {
		return
	}
	pair, err := s.Auth.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		apperr.Write(w, err)
		return
	}
	writeJSON(w, http.StatusOK, s.authResponse(pair))
}

func (s *Server) authResponse(pair service.TokenPair) api.AuthResponse {
	return api.AuthResponse{
		AccessToken:  pair.Access,
		RefreshToken: pair.Refresh,
		TokenType:    "Bearer",
		ExpiresIn:    int(s.Tokens.AccessTTL().Seconds()),
	}
}
