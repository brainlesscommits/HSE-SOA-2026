package flight

import (
	"errors"
	"time"
)

const (
	StatusScheduled = "SCHEDULED"
	StatusDeparted  = "DEPARTED"
	StatusCancelled = "CANCELLED"
	StatusCompleted = "COMPLETED"

	ReservationActive   = "ACTIVE"
	ReservationReleased = "RELEASED"
	ReservationExpired  = "EXPIRED"
)

type Flight struct {
	ID             int64     `json:"id"`
	AirlineCode    string    `json:"airline_code"`
	AirlineName    string    `json:"airline_name"`
	FlightNo       string    `json:"flight_no"`
	Origin         string    `json:"origin"`
	Destination    string    `json:"destination"`
	DepartureTime  time.Time `json:"departure_time"`
	ArrivalTime    time.Time `json:"arrival_time"`
	TotalSeats     int32     `json:"total_seats"`
	AvailableSeats int32     `json:"available_seats"`
	PriceMinor     int64     `json:"price_minor"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
}
func (f Flight) FlightNumber() string { return f.AirlineCode + f.FlightNo }

func (f Flight) DepartureDate() time.Time { return truncateDay(f.DepartureTime) }

type Reservation struct {
	ID        int64
	FlightID  int64
	BookingID string
	SeatCount int32
	Status    string
	CreatedAt time.Time
	UpdatedAt time.Time
}

type SearchQuery struct {
	Origin      string
	Destination string
	Date *time.Time
}

type FlightUpdate struct {
	Status     *string
	PriceMinor *int64
	Currency   *string
}

var (
	ErrFlightNotFound       = errors.New("flight not found")
	ErrFlightNotScheduled   = errors.New("flight is not in SCHEDULED status")
	ErrNotEnoughSeats       = errors.New("not enough available seats")
	ErrReservationNotFound  = errors.New("reservation not found")
	ErrReservationConflict  = errors.New("reservation for this booking already exists with different parameters")
	ErrReservationNotActive = errors.New("reservation for this booking is no longer active")
	ErrReservationExpired   = errors.New("reservation has expired")
)

func truncateDay(t time.Time) time.Time {
	y, m, d := t.UTC().Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}
