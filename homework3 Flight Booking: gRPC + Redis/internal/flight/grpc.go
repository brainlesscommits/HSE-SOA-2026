package flight

import (
	"context"
	"errors"
	"log/slog"
	"regexp"

	"github.com/google/uuid"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/timestamppb"

	flightv1 "flightbooking/gen/flight/v1"
)

var (
	iataAirportRe = regexp.MustCompile(`^[A-Z]{3}$`)
	currencyRe    = regexp.MustCompile(`^[A-Z]{3}$`)
)

type GRPCServer struct {
	flightv1.UnimplementedFlightServiceServer
	svc *Service
	log *slog.Logger
}

func NewGRPCServer(svc *Service, log *slog.Logger) *GRPCServer {
	return &GRPCServer{svc: svc, log: log}
}

func (s *GRPCServer) SearchFlights(ctx context.Context, req *flightv1.SearchFlightsRequest) (*flightv1.SearchFlightsResponse, error) {
	if !iataAirportRe.MatchString(req.GetOrigin()) || !iataAirportRe.MatchString(req.GetDestination()) {
		return nil, status.Error(codes.InvalidArgument, "origin and destination must be IATA airport codes (3 uppercase letters)")
	}
	if req.GetOrigin() == req.GetDestination() {
		return nil, status.Error(codes.InvalidArgument, "origin and destination must differ")
	}
	q := SearchQuery{Origin: req.GetOrigin(), Destination: req.GetDestination()}
	if req.DepartureDate != nil {
		if err := req.DepartureDate.CheckValid(); err != nil {
			return nil, status.Errorf(codes.InvalidArgument, "invalid departure_date: %v", err)
		}
		d := truncateDay(req.DepartureDate.AsTime())
		q.Date = &d
	}

	flights, err := s.svc.SearchFlights(ctx, q)
	if err != nil {
		return nil, s.toStatus(err)
	}
	resp := &flightv1.SearchFlightsResponse{Flights: make([]*flightv1.Flight, 0, len(flights))}
	for _, f := range flights {
		resp.Flights = append(resp.Flights, flightToProto(f))
	}
	return resp, nil
}

func (s *GRPCServer) GetFlight(ctx context.Context, req *flightv1.GetFlightRequest) (*flightv1.GetFlightResponse, error) {
	if req.GetFlightId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "flight_id must be positive")
	}
	f, err := s.svc.GetFlight(ctx, req.GetFlightId())
	if err != nil {
		return nil, s.toStatus(err)
	}
	return &flightv1.GetFlightResponse{Flight: flightToProto(f)}, nil
}

func (s *GRPCServer) ReserveSeats(ctx context.Context, req *flightv1.ReserveSeatsRequest) (*flightv1.ReserveSeatsResponse, error) {
	if req.GetFlightId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "flight_id must be positive")
	}
	if req.GetSeatCount() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "seat_count must be positive")
	}
	bookingID, err := parseBookingID(req.GetBookingId())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.ReserveSeats(ctx, req.GetFlightId(), req.GetSeatCount(), bookingID)
	if err != nil {
		return nil, s.toStatus(err)
	}
	return &flightv1.ReserveSeatsResponse{Reservation: reservationToProto(res)}, nil
}

func (s *GRPCServer) ReleaseReservation(ctx context.Context, req *flightv1.ReleaseReservationRequest) (*flightv1.ReleaseReservationResponse, error) {
	bookingID, err := parseBookingID(req.GetBookingId())
	if err != nil {
		return nil, err
	}
	res, err := s.svc.ReleaseReservation(ctx, bookingID)
	if err != nil {
		return nil, s.toStatus(err)
	}
	return &flightv1.ReleaseReservationResponse{Reservation: reservationToProto(res)}, nil
}

func (s *GRPCServer) UpdateFlight(ctx context.Context, req *flightv1.UpdateFlightRequest) (*flightv1.UpdateFlightResponse, error) {
	if req.GetFlightId() <= 0 {
		return nil, status.Error(codes.InvalidArgument, "flight_id must be positive")
	}
	var u FlightUpdate
	if req.Status != nil {
		st, ok := flightStatusFromProto[req.GetStatus()]
		if !ok {
			return nil, status.Error(codes.InvalidArgument, "status must be specified")
		}
		u.Status = &st
	}
	if p := req.GetPrice(); p != nil {
		if p.GetAmountMinor() <= 0 {
			return nil, status.Error(codes.InvalidArgument, "price must be positive")
		}
		if !currencyRe.MatchString(p.GetCurrencyCode()) {
			return nil, status.Error(codes.InvalidArgument, "price.currency_code must be an ISO 4217 code")
		}
		amount, currency := p.GetAmountMinor(), p.GetCurrencyCode()
		u.PriceMinor, u.Currency = &amount, &currency
	}
	if u.Status == nil && u.PriceMinor == nil {
		return nil, status.Error(codes.InvalidArgument, "nothing to update: set status and/or price")
	}

	f, err := s.svc.UpdateFlight(ctx, req.GetFlightId(), u)
	if err != nil {
		return nil, s.toStatus(err)
	}
	return &flightv1.UpdateFlightResponse{Flight: flightToProto(f)}, nil
}

func parseBookingID(s string) (string, error) {
	id, err := uuid.Parse(s)
	if err != nil {
		return "", status.Error(codes.InvalidArgument, "booking_id must be a UUID")
	}
	return id.String(), nil
}

func (s *GRPCServer) toStatus(err error) error {
	var code codes.Code
	switch {
	case errors.Is(err, ErrFlightNotFound), errors.Is(err, ErrReservationNotFound):
		code = codes.NotFound
	case errors.Is(err, ErrNotEnoughSeats):
		code = codes.ResourceExhausted
	case errors.Is(err, ErrFlightNotScheduled), errors.Is(err, ErrReservationExpired):
		code = codes.FailedPrecondition
	case errors.Is(err, ErrReservationConflict), errors.Is(err, ErrReservationNotActive):
		code = codes.AlreadyExists
	case errors.Is(err, context.Canceled):
		return status.Error(codes.Canceled, err.Error())
	case errors.Is(err, context.DeadlineExceeded):
		return status.Error(codes.DeadlineExceeded, err.Error())
	default:
		s.log.Error("internal error", "err", err)
		return status.Error(codes.Internal, "internal error")
	}
	return status.Error(code, err.Error())
}

var flightStatusToProto = map[string]flightv1.FlightStatus{
	StatusScheduled: flightv1.FlightStatus_FLIGHT_STATUS_SCHEDULED,
	StatusDeparted:  flightv1.FlightStatus_FLIGHT_STATUS_DEPARTED,
	StatusCancelled: flightv1.FlightStatus_FLIGHT_STATUS_CANCELLED,
	StatusCompleted: flightv1.FlightStatus_FLIGHT_STATUS_COMPLETED,
}

var flightStatusFromProto = map[flightv1.FlightStatus]string{
	flightv1.FlightStatus_FLIGHT_STATUS_SCHEDULED: StatusScheduled,
	flightv1.FlightStatus_FLIGHT_STATUS_DEPARTED:  StatusDeparted,
	flightv1.FlightStatus_FLIGHT_STATUS_CANCELLED: StatusCancelled,
	flightv1.FlightStatus_FLIGHT_STATUS_COMPLETED: StatusCompleted,
}

var reservationStatusToProto = map[string]flightv1.ReservationStatus{
	ReservationActive:   flightv1.ReservationStatus_RESERVATION_STATUS_ACTIVE,
	ReservationReleased: flightv1.ReservationStatus_RESERVATION_STATUS_RELEASED,
	ReservationExpired:  flightv1.ReservationStatus_RESERVATION_STATUS_EXPIRED,
}

func flightToProto(f Flight) *flightv1.Flight {
	return &flightv1.Flight{
		Id:             f.ID,
		FlightNumber:   f.FlightNumber(),
		AirlineCode:    f.AirlineCode,
		AirlineName:    f.AirlineName,
		Origin:         f.Origin,
		Destination:    f.Destination,
		DepartureTime:  timestamppb.New(f.DepartureTime),
		ArrivalTime:    timestamppb.New(f.ArrivalTime),
		TotalSeats:     f.TotalSeats,
		AvailableSeats: f.AvailableSeats,
		Price:          &flightv1.Money{CurrencyCode: f.Currency, AmountMinor: f.PriceMinor},
		Status:         flightStatusToProto[f.Status],
	}
}

func reservationToProto(r Reservation) *flightv1.SeatReservation {
	return &flightv1.SeatReservation{
		Id:        r.ID,
		FlightId:  r.FlightID,
		BookingId: r.BookingID,
		SeatCount: r.SeatCount,
		Status:    reservationStatusToProto[r.Status],
		CreatedAt: timestamppb.New(r.CreatedAt),
		UpdatedAt: timestamppb.New(r.UpdatedAt),
	}
}
