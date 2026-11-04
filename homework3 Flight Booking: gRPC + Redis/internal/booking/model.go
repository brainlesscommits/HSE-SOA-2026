package booking

import (
	"errors"
	"time"
)

const (
	StatusConfirmed = "CONFIRMED"
	StatusCancelled = "CANCELLED"
)

type Booking struct {
	ID              string
	UserID          string
	FlightID        int64
	PassengerName   string
	PassengerEmail  string
	SeatCount       int32
	TotalPriceMinor int64
	Currency        string
	Status          string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type CreateBookingInput struct {
	UserID         string `json:"user_id"`
	FlightID       int64  `json:"flight_id"`
	PassengerName  string `json:"passenger_name"`
	PassengerEmail string `json:"passenger_email"`
	SeatCount      int32  `json:"seat_count"`
}

var (
	ErrBookingNotFound = errors.New("booking not found")
	ErrNotCancellable  = errors.New("only CONFIRMED booking can be cancelled")
)

type ValidationError struct{ Msg string }

func (e *ValidationError) Error() string { return e.Msg }
