package service

import (
	"context"
	"net/http"
	"strings"

	"github.com/google/uuid"

	"marketplace/internal/api"
	"marketplace/internal/apperr"
	"marketplace/internal/auth"
	"marketplace/internal/repository"
)

type AuthService struct {
	DB     *repository.DB
	Users  repository.UserRepository
	Tokens *auth.Service
}

type TokenPair struct{ Access, Refresh string }

func NewAuthService(db *repository.DB, tokens *auth.Service) *AuthService {
	return &AuthService{DB: db, Users: repository.UserRepository{}, Tokens: tokens}
}

func (s *AuthService) Register(ctx context.Context, email, password, role string) (TokenPair, error) {
	email = normalizeEmail(email)
	hash, err := auth.HashPassword(password)
	if err != nil {
		return TokenPair{}, err
	}
	u, err := s.Users.Create(ctx, s.DB.Pool, repository.User{ID: uuid.New(), Email: email, PasswordHash: hash, Role: role})
	if repository.IsUniqueViolation(err) {
		return TokenPair{}, apperr.New(http.StatusConflict, api.USERALREADYEXISTS, "user with this email already exists", nil)
	}
	if err != nil {
		return TokenPair{}, err
	}
	return s.issue(u)
}

func (s *AuthService) Login(ctx context.Context, email, password string) (TokenPair, error) {
	invalid := apperr.New(http.StatusUnauthorized, api.INVALIDCREDENTIALS, "invalid email or password", nil)
	u, err := s.Users.GetByEmail(ctx, s.DB.Pool, normalizeEmail(email))
	if repository.IsNotFound(err) {
		return TokenPair{}, invalid
	}
	if err != nil {
		return TokenPair{}, err
	}
	if auth.CheckPassword(u.PasswordHash, password) != nil {
		return TokenPair{}, invalid
	}
	return s.issue(u)
}

func (s *AuthService) Refresh(ctx context.Context, token string) (TokenPair, error) {
	invalid := apperr.New(http.StatusUnauthorized, api.REFRESHTOKENINVALID, "invalid refresh token", nil)
	c, err := s.Tokens.ParseRefreshToken(token)
	if err != nil {
		return TokenPair{}, invalid
	}
	id, err := uuid.Parse(c.UserID)
	if err != nil {
		return TokenPair{}, invalid
	}
	u, err := s.Users.GetByID(ctx, s.DB.Pool, id)
	if repository.IsNotFound(err) {
		return TokenPair{}, invalid
	}
	if err != nil {
		return TokenPair{}, err
	}
	return s.issue(u)
}

func (s *AuthService) EnsureAdmin(ctx context.Context, email, password string) (bool, error) {
	email = normalizeEmail(email)
	if _, err := s.Users.GetByEmail(ctx, s.DB.Pool, email); err == nil {
		return false, nil
	} else if !repository.IsNotFound(err) {
		return false, err
	}
	hash, err := auth.HashPassword(password)
	if err != nil {
		return false, err
	}
	_, err = s.Users.Create(ctx, s.DB.Pool, repository.User{ID: uuid.New(), Email: email, PasswordHash: hash, Role: RoleAdmin})
	if repository.IsUniqueViolation(err) {
		return false, nil
	}
	return err == nil, err
}

func (s *AuthService) issue(u repository.User) (TokenPair, error) {
	access, refresh, err := s.Tokens.Issue(u.ID.String(), u.Role)
	return TokenPair{Access: access, Refresh: refresh}, err
}

func normalizeEmail(email string) string { return strings.ToLower(strings.TrimSpace(email)) }
