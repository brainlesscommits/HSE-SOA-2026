package flight

import (
	"context"
	"log/slog"
	"time"
)

const reinvalidateDelay = 500 * time.Millisecond

type Service struct {
	repo  *Repository
	cache *Cache
	log   *slog.Logger
}

func NewService(repo *Repository, cache *Cache, log *slog.Logger) *Service {
	return &Service{repo: repo, cache: cache, log: log}
}

func (s *Service) GetFlight(ctx context.Context, id int64) (Flight, error) {
	if f, ok := s.cache.GetFlight(ctx, id); ok {
		return f, nil
	}
	f, err := s.repo.GetFlight(ctx, id)
	if err != nil {
		return Flight{}, err
	}
	s.cache.SetFlight(ctx, f)
	return f, nil
}

func (s *Service) SearchFlights(ctx context.Context, q SearchQuery) ([]Flight, error) {
	if flights, ok := s.cache.GetSearch(ctx, q); ok {
		return flights, nil
	}
	flights, err := s.repo.SearchFlights(ctx, q)
	if err != nil {
		return nil, err
	}
	s.cache.SetSearch(ctx, q, flights)
	return flights, nil
}

func (s *Service) ReserveSeats(ctx context.Context, flightID int64, seatCount int32, bookingID string) (Reservation, error) {
	res, f, created, err := s.repo.ReserveSeats(ctx, flightID, seatCount, bookingID)
	if err != nil {
		return Reservation{}, err
	}
	if created {
		s.log.Info("seats reserved", "booking_id", bookingID, "flight_id", flightID,
			"seat_count", seatCount, "available_seats", f.AvailableSeats)
		s.invalidate(f)
	} else {
		s.log.Info("idempotent replay of ReserveSeats, returning existing reservation",
			"booking_id", bookingID, "reservation_id", res.ID)
	}
	return res, nil
}

func (s *Service) ReleaseReservation(ctx context.Context, bookingID string) (Reservation, error) {
	res, f, released, err := s.repo.ReleaseReservation(ctx, bookingID)
	if err != nil {
		return Reservation{}, err
	}
	if released {
		s.log.Info("reservation released", "booking_id", bookingID, "flight_id", f.ID,
			"seat_count", res.SeatCount, "available_seats", f.AvailableSeats)
		s.invalidate(f)
	} else {
		s.log.Info("reservation already released", "booking_id", bookingID)
	}
	return res, nil
}

func (s *Service) UpdateFlight(ctx context.Context, id int64, u FlightUpdate) (Flight, error) {
	f, err := s.repo.UpdateFlight(ctx, id, u)
	if err != nil {
		return Flight{}, err
	}
	s.log.Info("flight updated", "flight_id", id)
	s.invalidate(f)
	return f, nil
}

func (s *Service) invalidate(f Flight) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	s.cache.InvalidateFlight(ctx, f)

	time.AfterFunc(reinvalidateDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		s.cache.InvalidateFlight(ctx, f)
	})
}
