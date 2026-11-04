package booking

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const bookingColumns = `
	id::text, user_id, flight_id, passenger_name, passenger_email, seat_count,
	(total_price * 100)::bigint, currency, status, created_at, updated_at`

func scanBooking(row pgx.Row) (Booking, error) {
	var b Booking
	err := row.Scan(&b.ID, &b.UserID, &b.FlightID, &b.PassengerName, &b.PassengerEmail, &b.SeatCount,
		&b.TotalPriceMinor, &b.Currency, &b.Status, &b.CreatedAt, &b.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Booking{}, ErrBookingNotFound
	}
	return b, err
}

func (r *Repository) Create(ctx context.Context, b Booking) (Booking, error) {
	return scanBooking(r.pool.QueryRow(ctx, `
		INSERT INTO bookings (id, user_id, flight_id, passenger_name, passenger_email,
		                      seat_count, total_price, currency, status)
		VALUES ($1, $2, $3, $4, $5, $6, $7::bigint / 100.0, $8, $9)
		RETURNING `+bookingColumns,
		b.ID, b.UserID, b.FlightID, b.PassengerName, b.PassengerEmail,
		b.SeatCount, b.TotalPriceMinor, b.Currency, b.Status))
}

func (r *Repository) Get(ctx context.Context, id string) (Booking, error) {
	return scanBooking(r.pool.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings WHERE id = $1`, id))
}

func (r *Repository) ListByUser(ctx context.Context, userID string) ([]Booking, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+bookingColumns+` FROM bookings
		WHERE user_id = $1
		ORDER BY created_at DESC, id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	bookings := []Booking{}
	for rows.Next() {
		b, err := scanBooking(rows)
		if err != nil {
			return nil, err
		}
		bookings = append(bookings, b)
	}
	return bookings, rows.Err()
}

func (r *Repository) Cancel(ctx context.Context, id string, release func(Booking) error) (b Booking, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		b, err = scanBooking(tx.QueryRow(ctx, `SELECT `+bookingColumns+` FROM bookings WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if b.Status != StatusConfirmed {
			return ErrNotCancellable
		}
		if err := release(b); err != nil {
			return err
		}
		b, err = scanBooking(tx.QueryRow(ctx, `
			UPDATE bookings SET status = 'CANCELLED', updated_at = now()
			WHERE id = $1
			RETURNING `+bookingColumns, id))
		return err
	})
	return b, err
}
