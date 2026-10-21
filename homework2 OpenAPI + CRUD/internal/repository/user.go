package repository

import (
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type User struct {
	ID                        uuid.UUID
	Email, PasswordHash, Role string
}

type UserRepository struct{}

func (r UserRepository) Create(ctx context.Context, q Querier, user User) (User, error) {
	var out User
	err := q.QueryRow(ctx, `INSERT INTO users(id,email,password_hash,role) VALUES($1,$2,$3,$4) RETURNING id,email,password_hash,role`, user.ID, user.Email, user.PasswordHash, user.Role).Scan(&out.ID, &out.Email, &out.PasswordHash, &out.Role)
	return out, err
}

func (r UserRepository) GetByEmail(ctx context.Context, q Querier, email string) (User, error) {
	var u User
	err := q.QueryRow(ctx, `SELECT id,email,password_hash,role FROM users WHERE email=$1`, email).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role)
	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, pgx.ErrNoRows
	}
	return u, err
}

func (r UserRepository) GetByID(ctx context.Context, q Querier, id uuid.UUID) (User, error) {
	var u User
	err := q.QueryRow(ctx, `SELECT id,email,password_hash,role FROM users WHERE id=$1`, id).Scan(&u.ID, &u.Email, &u.PasswordHash, &u.Role)
	return u, err
}

func (r UserRepository) LockByID(ctx context.Context, q Querier, id uuid.UUID) error {
	var locked uuid.UUID
	return q.QueryRow(ctx, `SELECT id FROM users WHERE id=$1 FOR UPDATE`, id).Scan(&locked)
}
