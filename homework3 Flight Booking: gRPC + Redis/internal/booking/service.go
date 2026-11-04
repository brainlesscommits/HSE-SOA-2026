package booking

import (
	"context"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	flightv1 "flightbooking/gen/flight/v1"
)

var (
	iataAirportRe = regexp.MustCompile(`^[A-Z]{3}$`)
	emailRe = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)
)

type Store interface {
	Create(ctx context.Context, b Booking) (Booking, error)
	Get(ctx context.Context, id string) (Booking, error)
	ListByUser(ctx context.Context, userID string) ([]Booking, error)
	Cancel(ctx context.Context, id string, release func(Booking) error) (Booking, error)
}

type Service struct {
	store   Store
	flights flightv1.FlightServiceClient
	log     *slog.Logger
}

func NewService(store Store, flights flightv1.FlightServiceClient, log *slog.Logger) *Service {
	return &Service{store: store, flights: flights, log: log}
}

func (s *Service) SearchFlights(ctx context.Context, origin, destination, date string) ([]*flightv1.Flight, error) {
	origin, destination = strings.ToUpper(origin), strings.ToUpper(destination)
	if origin == "" || destination == "" {
		return nil, &ValidationError{"query parameters origin and destination are required"}
	}
	if !iataAirportRe.MatchString(origin) || !iataAirportRe.MatchString(destination) {
		return nil, &ValidationError{"origin and destination must be IATA airport codes, e.g. SVO"}
	}
	req := &flightv1.SearchFlightsRequest{Origin: origin, Destination: destination}
	if date != "" {
		d, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return nil, &ValidationError{"date must be in YYYY-MM-DD format"}
		}
		req.DepartureDate = timestamppb.New(d)
	}
	resp, err := s.flights.SearchFlights(ctx, req)
	if err != nil {
		return nil, err
	}
	return resp.GetFlights(), nil
}

func (s *Service) GetFlight(ctx context.Context, id int64) (*flightv1.Flight, error) {
	if id <= 0 {
		return nil, &ValidationError{"flight id must be a positive integer"}
	}
	resp, err := s.flights.GetFlight(ctx, &flightv1.GetFlightRequest{FlightId: id})
	if err != nil {
		return nil, err
	}
	return resp.GetFlight(), nil
}

func (s *Service) CreateBooking(ctx context.Context, in CreateBookingInput) (Booking, error) {
	if err := validateCreate(&in); err != nil {
		return Booking{}, err
	}

	flightResp, err := s.flights.GetFlight(ctx, &flightv1.GetFlightRequest{FlightId: in.FlightID})
	if err != nil {
		return Booking{}, err
	}
	price := flightResp.GetFlight().GetPrice()

	bookingID := uuid.NewString()
	if _, err := s.flights.ReserveSeats(ctx, &flightv1.ReserveSeatsRequest{
		FlightId:  in.FlightID,
		SeatCount: in.SeatCount,
		BookingId: bookingID,
	}); err != nil {
		return Booking{}, err
	}

	b, err := s.store.Create(ctx, Booking{
		ID:              bookingID,
		UserID:          in.UserID,
		FlightID:        in.FlightID,
		PassengerName:   in.PassengerName,
		PassengerEmail:  in.PassengerEmail,
		SeatCount:       in.SeatCount,
		TotalPriceMinor: int64(in.SeatCount) * price.GetAmountMinor(),
		Currency:        price.GetCurrencyCode(),
		Status:          StatusConfirmed,
	})
	if err != nil {
		s.log.Error("failed to save booking, compensating seat reservation", "booking_id", bookingID, "err", err)
		s.compensate(ctx, bookingID)
		return Booking{}, err
	}
	s.log.Info("booking created", "booking_id", b.ID, "flight_id", b.FlightID, "seat_count", b.SeatCount)
	return b, nil
}

func (s *Service) compensate(ctx context.Context, bookingID string) {
	// Компенсация должна дойти, даже если запрос клиента уже отменён.
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()
	if _, err := s.flights.ReleaseReservation(ctx, &flightv1.ReleaseReservationRequest{BookingId: bookingID}); err != nil {
		s.log.Error("compensation failed: reservation left active", "booking_id", bookingID, "err", err)
		return
	}
	s.log.Info("compensation done: reservation released", "booking_id", bookingID)
}

func (s *Service) GetBooking(ctx context.Context, id string) (Booking, error) {
	if uuid.Validate(id) != nil {
		return Booking{}, ErrBookingNotFound
	}
	return s.store.Get(ctx, id)
}

func (s *Service) ListBookings(ctx context.Context, userID string) ([]Booking, error) {
	if strings.TrimSpace(userID) == "" {
		return nil, &ValidationError{"query parameter user_id is required"}
	}
	return s.store.ListByUser(ctx, userID)
}

func (s *Service) CancelBooking(ctx context.Context, id string) (Booking, error) {
	if uuid.Validate(id) != nil {
		return Booking{}, ErrBookingNotFound
	}
	b, err := s.store.Cancel(ctx, id, func(b Booking) error {
		_, err := s.flights.ReleaseReservation(ctx, &flightv1.ReleaseReservationRequest{BookingId: b.ID})
		if status.Code(err) == codes.NotFound {
			s.log.Warn("no reservation found for booking, cancelling anyway", "booking_id", b.ID)
			return nil
		}
		return err
	})
	if err != nil {
		return Booking{}, err
	}
	s.log.Info("booking cancelled", "booking_id", b.ID)
	return b, nil
}

func validateCreate(in *CreateBookingInput) error {
	in.UserID = strings.TrimSpace(in.UserID)
	in.PassengerName = strings.TrimSpace(in.PassengerName)
	in.PassengerEmail = strings.TrimSpace(in.PassengerEmail)

	switch {
	case in.UserID == "":
		return &ValidationError{"user_id is required"}
	case len(in.UserID) > 64:
		return &ValidationError{"user_id must be at most 64 characters"}
	case in.FlightID <= 0:
		return &ValidationError{"flight_id must be a positive integer"}
	case in.PassengerName == "":
		return &ValidationError{"passenger_name is required"}
	case len(in.PassengerName) > 200:
		return &ValidationError{"passenger_name must be at most 200 characters"}
	case in.SeatCount <= 0:
		return &ValidationError{"seat_count must be a positive integer"}
	case len(in.PassengerEmail) > 254 || !emailRe.MatchString(in.PassengerEmail):
		return &ValidationError{"passenger_email must be a valid email address"}
	}
	return nil
}
