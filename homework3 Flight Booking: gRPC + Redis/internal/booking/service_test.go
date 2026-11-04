package booking

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	flightv1 "flightbooking/gen/flight/v1"
)

type fakeFlights struct {
	flightv1.FlightServiceClient
	reserveErr error
	reserved   []string
	released   []string
}

func (f *fakeFlights) GetFlight(_ context.Context, req *flightv1.GetFlightRequest, _ ...grpc.CallOption) (*flightv1.GetFlightResponse, error) {
	return &flightv1.GetFlightResponse{Flight: &flightv1.Flight{
		Id:    req.GetFlightId(),
		Price: &flightv1.Money{CurrencyCode: "RUB", AmountMinor: 550000},
	}}, nil
}

func (f *fakeFlights) ReserveSeats(_ context.Context, req *flightv1.ReserveSeatsRequest, _ ...grpc.CallOption) (*flightv1.ReserveSeatsResponse, error) {
	if f.reserveErr != nil {
		return nil, f.reserveErr
	}
	f.reserved = append(f.reserved, req.GetBookingId())
	return &flightv1.ReserveSeatsResponse{}, nil
}

func (f *fakeFlights) ReleaseReservation(_ context.Context, req *flightv1.ReleaseReservationRequest, _ ...grpc.CallOption) (*flightv1.ReleaseReservationResponse, error) {
	f.released = append(f.released, req.GetBookingId())
	return &flightv1.ReleaseReservationResponse{}, nil
}

type fakeStore struct {
	Store
	createErr error
	created   []Booking
}

func (s *fakeStore) Create(_ context.Context, b Booking) (Booking, error) {
	if s.createErr != nil {
		return Booking{}, s.createErr
	}
	s.created = append(s.created, b)
	return b, nil
}

var validInput = CreateBookingInput{
	UserID: "user-1", FlightID: 1, PassengerName: "Ivan Petrov", PassengerEmail: "ivan@example.com", SeatCount: 2,
}

func newTestService(flights *fakeFlights, store *fakeStore) *Service {
	return NewService(store, flights, slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestCreateBookingSnapshotsPrice(t *testing.T) {
	flights, store := &fakeFlights{}, &fakeStore{}
	b, err := newTestService(flights, store).CreateBooking(context.Background(), validInput)
	if err != nil {
		t.Fatal(err)
	}
	if b.TotalPriceMinor != 2*550000 || b.Currency != "RUB" || b.Status != StatusConfirmed {
		t.Fatalf("booking = %+v, want total 11000.00 RUB CONFIRMED", b)
	}
	if len(flights.reserved) != 1 || flights.reserved[0] != b.ID {
		t.Fatalf("ReserveSeats booking_id = %v, want the booking id %s", flights.reserved, b.ID)
	}
}

func TestCreateBookingNotSavedWhenReservationFails(t *testing.T) {
	flights := &fakeFlights{reserveErr: status.Error(codes.ResourceExhausted, "not enough seats")}
	store := &fakeStore{}
	_, err := newTestService(flights, store).CreateBooking(context.Background(), validInput)
	if status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("err = %v, want RESOURCE_EXHAUSTED", err)
	}
	if len(store.created) != 0 {
		t.Fatal("booking was saved although reservation failed")
	}
}

func TestCreateBookingCompensatesWhenSaveFails(t *testing.T) {
	flights := &fakeFlights{}
	store := &fakeStore{createErr: errors.New("db is down")}
	_, err := newTestService(flights, store).CreateBooking(context.Background(), validInput)
	if err == nil {
		t.Fatal("expected error")
	}
	if len(flights.released) != 1 || flights.released[0] != flights.reserved[0] {
		t.Fatalf("released = %v, want reservation %v to be released", flights.released, flights.reserved)
	}
}

func TestCreateBookingValidation(t *testing.T) {
	cases := map[string]func(*CreateBookingInput){
		"no user":        func(in *CreateBookingInput) { in.UserID = " " },
		"bad flight id":  func(in *CreateBookingInput) { in.FlightID = 0 },
		"no name":        func(in *CreateBookingInput) { in.PassengerName = "" },
		"bad email":      func(in *CreateBookingInput) { in.PassengerEmail = "ivan@localhost" },
		"zero seats":     func(in *CreateBookingInput) { in.SeatCount = 0 },
		"negative seats": func(in *CreateBookingInput) { in.SeatCount = -1 },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			in := validInput
			mutate(&in)
			flights := &fakeFlights{}
			_, err := newTestService(flights, &fakeStore{}).CreateBooking(context.Background(), in)
			var verr *ValidationError
			if !errors.As(err, &verr) {
				t.Fatalf("err = %v, want ValidationError", err)
			}
			if len(flights.reserved) != 0 {
				t.Fatal("flight service was called for invalid input")
			}
		})
	}
}
