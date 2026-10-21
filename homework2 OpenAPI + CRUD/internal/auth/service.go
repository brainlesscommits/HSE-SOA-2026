package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrTokenExpired = errors.New("token expired")
	ErrTokenInvalid = errors.New("token invalid")
)

type Claims struct {
	UserID string
	Role   string
}

type Service struct {
	secret     []byte
	accessTTL  time.Duration
	refreshTTL time.Duration
}

func (s *Service) AccessTTL() time.Duration { return s.accessTTL }

func New(secret string, accessTTL, refreshTTL time.Duration) *Service {
	return &Service{secret: []byte(secret), accessTTL: accessTTL, refreshTTL: refreshTTL}
}

func HashPassword(password string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(b), err
}
func CheckPassword(hash, password string) error {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
}

func (s *Service) Issue(userID, role string) (string, string, error) {
	access, err := s.issue(userID, role, "access", s.accessTTL)
	if err != nil {
		return "", "", err
	}
	refresh, err := s.issue(userID, role, "refresh", s.refreshTTL)
	return access, refresh, err
}

func (s *Service) issue(userID, role, typ string, ttl time.Duration) (string, error) {
	now := time.Now()
	claims := jwt.MapClaims{"sub": userID, "role": role, "type": typ, "iat": now.Unix(), "exp": now.Add(ttl).Unix()}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.secret)
}

func (s *Service) ParseAccessToken(tokenString string) (Claims, error) {
	return s.parse(tokenString, "access")
}
func (s *Service) ParseRefreshToken(tokenString string) (Claims, error) {
	return s.parse(tokenString, "refresh")
}

func (s *Service) parse(tokenString, expectedType string) (Claims, error) {
	token, err := jwt.Parse(tokenString, func(token *jwt.Token) (any, error) {
		if token.Method != jwt.SigningMethodHS256 {
			return nil, errors.New("unexpected signing method")
		}
		return s.secret, nil
	})
	if errors.Is(err, jwt.ErrTokenExpired) {
		return Claims{}, ErrTokenExpired
	}
	if err != nil || !token.Valid {
		return Claims{}, ErrTokenInvalid
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return Claims{}, ErrTokenInvalid
	}
	typ, _ := claims["type"].(string)
	if typ != expectedType {
		return Claims{}, ErrTokenInvalid
	}
	uid, _ := claims["sub"].(string)
	role, _ := claims["role"].(string)
	if uid == "" || role == "" {
		return Claims{}, ErrTokenInvalid
	}
	return Claims{UserID: uid, Role: role}, nil
}
