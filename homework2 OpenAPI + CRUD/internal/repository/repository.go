package repository

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Querier interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

type DB struct{ Pool *pgxpool.Pool }

func (db *DB) Begin(ctx context.Context) (pgx.Tx, error) { return db.Pool.Begin(ctx) }

func (db *DB) InTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := db.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func IsNotFound(err error) bool { return errors.Is(err, pgx.ErrNoRows) }

func IsUniqueViolation(err error) bool { return pgCode(err) == "23505" }

func IsForeignKeyViolation(err error) bool { return pgCode(err) == "23503" }

func pgCode(err error) string {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code
	}
	return ""
}
