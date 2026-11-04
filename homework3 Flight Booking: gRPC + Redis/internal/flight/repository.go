package flight

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Repository struct {
	pool *pgxpool.Pool
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{pool: pool}
}

const flightColumns = `
	f.id, f.airline_code, a.name, f.flight_no, f.origin, f.destination,
	f.departure_time, f.arrival_time, f.total_seats, f.available_seats,
	(f.price * 100)::bigint, f.currency, f.status`

const reservationColumns = `id, flight_id, booking_id::text, seat_count, status, created_at, updated_at`

func scanFlight(row pgx.Row) (Flight, error) {
	var f Flight
	err := row.Scan(&f.ID, &f.AirlineCode, &f.AirlineName, &f.FlightNo, &f.Origin, &f.Destination,
		&f.DepartureTime, &f.ArrivalTime, &f.TotalSeats, &f.AvailableSeats,
		&f.PriceMinor, &f.Currency, &f.Status)
	if errors.Is(err, pgx.ErrNoRows) {
		return Flight{}, ErrFlightNotFound
	}
	return f, err
}

func scanReservation(row pgx.Row) (Reservation, error) {
	var r Reservation
	err := row.Scan(&r.ID, &r.FlightID, &r.BookingID, &r.SeatCount, &r.Status, &r.CreatedAt, &r.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Reservation{}, ErrReservationNotFound
	}
	return r, err
}

func (r *Repository) GetFlight(ctx context.Context, id int64) (Flight, error) {
	return scanFlight(r.pool.QueryRow(ctx, `
		SELECT `+flightColumns+`
		FROM flights f JOIN airlines a ON a.code = f.airline_code
		WHERE f.id = $1`, id))
}

func (r *Repository) SearchFlights(ctx context.Context, q SearchQuery) ([]Flight, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT `+flightColumns+`
		FROM flights f JOIN airlines a ON a.code = f.airline_code
		WHERE f.status = 'SCHEDULED'
		  AND f.origin = $1 AND f.destination = $2
		  AND ($3::date IS NULL OR (f.departure_time AT TIME ZONE 'UTC')::date = $3::date)
		ORDER BY f.departure_time, f.id`, q.Origin, q.Destination, q.Date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	flights := []Flight{}
	for rows.Next() {
		f, err := scanFlight(rows)
		if err != nil {
			return nil, err
		}
		flights = append(flights, f)
	}
	return flights, rows.Err()
}

func (r *Repository) ReserveSeats(ctx context.Context, flightID int64, seatCount int32, bookingID string) (res Reservation, f Flight, created bool, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		f, err = lockFlight(ctx, tx, flightID)
		if err != nil {
			return err
		}

		existing, err := scanReservation(tx.QueryRow(ctx, `
			SELECT `+reservationColumns+` FROM seat_reservations WHERE booking_id = $1`, bookingID))
		switch {
		case err == nil:
			if existing.FlightID != flightID || existing.SeatCount != seatCount {
				return ErrReservationConflict
			}
			if existing.Status != ReservationActive {
				return ErrReservationNotActive
			}
			res = existing
			return nil
		case !errors.Is(err, ErrReservationNotFound):
			return err
		}

		if f.Status != StatusScheduled {
			return ErrFlightNotScheduled
		}
		if f.AvailableSeats < seatCount {
			return ErrNotEnoughSeats
		}

		if _, err := tx.Exec(ctx, `
			UPDATE flights SET available_seats = available_seats - $2, updated_at = now()
			WHERE id = $1`, flightID, seatCount); err != nil {
			return err
		}
		f.AvailableSeats -= seatCount

		res, err = scanReservation(tx.QueryRow(ctx, `
			INSERT INTO seat_reservations (flight_id, booking_id, seat_count, status)
			VALUES ($1, $2, $3, 'ACTIVE')
			RETURNING `+reservationColumns, flightID, bookingID, seatCount))
		if err != nil {
			var pgErr *pgconn.PgError
			if errors.As(err, &pgErr) && pgErr.Code == "23505" {
				return ErrReservationConflict
			}
			return err
		}
		created = true
		return nil
	})
	return res, f, created, err
}

func (r *Repository) ReleaseReservation(ctx context.Context, bookingID string) (res Reservation, f Flight, released bool, err error) {
	err = pgx.BeginFunc(ctx, r.pool, func(tx pgx.Tx) error {
		var flightID int64
		err := tx.QueryRow(ctx, `SELECT flight_id FROM seat_reservations WHERE booking_id = $1`, bookingID).Scan(&flightID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReservationNotFound
		}
		if err != nil {
			return err
		}

		if f, err = lockFlight(ctx, tx, flightID); err != nil {
			return err
		}
		res, err = scanReservation(tx.QueryRow(ctx, `
			SELECT `+reservationColumns+` FROM seat_reservations
			WHERE booking_id = $1 FOR UPDATE`, bookingID))
		if err != nil {
			return err
		}

		switch res.Status {
		case ReservationReleased:
			return nil
		case ReservationExpired:
			return ErrReservationExpired
		}

		if _, err := tx.Exec(ctx, `
			UPDATE flights SET available_seats = available_seats + $2, updated_at = now()
			WHERE id = $1`, flightID, res.SeatCount); err != nil {
			return err
		}
		f.AvailableSeats += res.SeatCount

		res, err = scanReservation(tx.QueryRow(ctx, `
			UPDATE seat_reservations SET status = 'RELEASED', updated_at = now()
			WHERE id = $1
			RETURNING `+reservationColumns, res.ID))
		if err != nil {
			return err
		}
		released = true
		return nil
	})
	return res, f, released, err
}

func (r *Repository) UpdateFlight(ctx context.Context, id int64, u FlightUpdate) (Flight, error) {
	return scanFlight(r.pool.QueryRow(ctx, `
		WITH f AS (
			UPDATE flights SET
				status     = COALESCE($2::flight_status, status),
				price      = COALESCE($3::bigint / 100.0, price),
				currency   = COALESCE($4, currency),
				updated_at = now()
			WHERE id = $1
			RETURNING *
		)
		SELECT `+flightColumns+` FROM f JOIN airlines a ON a.code = f.airline_code`,
		id, u.Status, u.PriceMinor, u.Currency))
}

func lockFlight(ctx context.Context, tx pgx.Tx, id int64) (Flight, error) {
	f, err := scanFlight(tx.QueryRow(ctx, `
		SELECT `+flightColumns+`
		FROM flights f JOIN airlines a ON a.code = f.airline_code
		WHERE f.id = $1
		FOR UPDATE OF f`, id))
	if err != nil && !errors.Is(err, ErrFlightNotFound) {
		return Flight{}, fmt.Errorf("lock flight %d: %w", id, err)
	}
	return f, err
}
