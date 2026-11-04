package booking

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	flightv1 "flightbooking/gen/flight/v1"
)

type Handler struct {
	svc *Service
	log *slog.Logger
}

func NewHandler(svc *Service, log *slog.Logger) http.Handler {
	h := &Handler{svc: svc, log: log}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /flights", h.searchFlights)
	mux.HandleFunc("GET /flights/{id}", h.getFlight)
	mux.HandleFunc("POST /bookings", h.createBooking)
	mux.HandleFunc("GET /bookings", h.listBookings)
	mux.HandleFunc("GET /bookings/{id}", h.getBooking)
	mux.HandleFunc("POST /bookings/{id}/cancel", h.cancelBooking)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	return h.logRequests(mux)
}

func (h *Handler) searchFlights(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	flights, err := h.svc.SearchFlights(r.Context(), q.Get("origin"), q.Get("destination"), q.Get("date"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]flightDTO, 0, len(flights))
	for _, f := range flights {
		out = append(out, toFlightDTO(f))
	}
	writeJSON(w, http.StatusOK, map[string]any{"flights": out})
}

func (h *Handler) getFlight(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		h.writeError(w, &ValidationError{"flight id must be a positive integer"})
		return
	}
	f, err := h.svc.GetFlight(r.Context(), id)
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toFlightDTO(f))
}

func (h *Handler) createBooking(w http.ResponseWriter, r *http.Request) {
	var in CreateBookingInput
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		h.writeError(w, &ValidationError{"invalid JSON body: " + err.Error()})
		return
	}
	b, err := h.svc.CreateBooking(r.Context(), in)
	if err != nil {
		h.writeError(w, err)
		return
	}
	w.Header().Set("Location", "/bookings/"+b.ID)
	writeJSON(w, http.StatusCreated, toBookingDTO(b))
}

func (h *Handler) getBooking(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.GetBooking(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toBookingDTO(b))
}

func (h *Handler) listBookings(w http.ResponseWriter, r *http.Request) {
	bookings, err := h.svc.ListBookings(r.Context(), r.URL.Query().Get("user_id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	out := make([]bookingDTO, 0, len(bookings))
	for _, b := range bookings {
		out = append(out, toBookingDTO(b))
	}
	writeJSON(w, http.StatusOK, map[string]any{"bookings": out})
}

func (h *Handler) cancelBooking(w http.ResponseWriter, r *http.Request) {
	b, err := h.svc.CancelBooking(r.Context(), r.PathValue("id"))
	if err != nil {
		h.writeError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toBookingDTO(b))
}

func (h *Handler) writeError(w http.ResponseWriter, err error) {
	var verr *ValidationError
	switch {
	case errors.As(err, &verr):
		writeErrorJSON(w, http.StatusBadRequest, "INVALID_ARGUMENT", verr.Msg)
		return
	case errors.Is(err, ErrBookingNotFound):
		writeErrorJSON(w, http.StatusNotFound, "BOOKING_NOT_FOUND", err.Error())
		return
	case errors.Is(err, ErrNotCancellable):
		writeErrorJSON(w, http.StatusConflict, "BOOKING_NOT_CANCELLABLE", err.Error())
		return
	}

	st, ok := status.FromError(err)
	if !ok {
		h.log.Error("internal error", "err", err)
		writeErrorJSON(w, http.StatusInternalServerError, "INTERNAL", "internal error")
		return
	}
	var httpStatus int
	switch st.Code() {
	case codes.InvalidArgument:
		httpStatus = http.StatusBadRequest
	case codes.NotFound:
		httpStatus = http.StatusNotFound
	case codes.ResourceExhausted, codes.FailedPrecondition, codes.AlreadyExists:
		httpStatus = http.StatusConflict
	case codes.Unavailable:
		httpStatus = http.StatusServiceUnavailable
		w.Header().Set("Retry-After", "10")
	case codes.DeadlineExceeded:
		httpStatus = http.StatusGatewayTimeout
	default:
		h.log.Error("flight service call failed", "code", st.Code().String(), "message", st.Message())
		writeErrorJSON(w, http.StatusBadGateway, "UPSTREAM_ERROR", "flight service error")
		return
	}
	writeErrorJSON(w, httpStatus, st.Code().String(), st.Message())
}

func (h *Handler) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rec, r)
		h.log.Info("http request", "method", r.Method, "path", r.URL.RequestURI(),
			"status", rec.status, "duration", time.Since(start).Round(time.Microsecond))
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErrorJSON(w http.ResponseWriter, code int, errCode, msg string) {
	writeJSON(w, code, map[string]any{"error": map[string]string{"code": errCode, "message": msg}})
}

type moneyDTO struct {
	Amount   string `json:"amount"`
	Currency string `json:"currency"`
}

func toMoneyDTO(minor int64, currency string) moneyDTO {
	return moneyDTO{Amount: fmt.Sprintf("%d.%02d", minor/100, minor%100), Currency: currency}
}

type flightDTO struct {
	ID             int64     `json:"id"`
	FlightNumber   string    `json:"flight_number"`
	AirlineCode    string    `json:"airline_code"`
	AirlineName    string    `json:"airline_name"`
	Origin         string    `json:"origin"`
	Destination    string    `json:"destination"`
	DepartureTime  time.Time `json:"departure_time"`
	ArrivalTime    time.Time `json:"arrival_time"`
	TotalSeats     int32     `json:"total_seats"`
	AvailableSeats int32     `json:"available_seats"`
	Price          moneyDTO  `json:"price"`
	Status         string    `json:"status"`
}

var flightStatusNames = map[flightv1.FlightStatus]string{
	flightv1.FlightStatus_FLIGHT_STATUS_SCHEDULED: "SCHEDULED",
	flightv1.FlightStatus_FLIGHT_STATUS_DEPARTED:  "DEPARTED",
	flightv1.FlightStatus_FLIGHT_STATUS_CANCELLED: "CANCELLED",
	flightv1.FlightStatus_FLIGHT_STATUS_COMPLETED: "COMPLETED",
}

func toFlightDTO(f *flightv1.Flight) flightDTO {
	return flightDTO{
		ID:             f.GetId(),
		FlightNumber:   f.GetFlightNumber(),
		AirlineCode:    f.GetAirlineCode(),
		AirlineName:    f.GetAirlineName(),
		Origin:         f.GetOrigin(),
		Destination:    f.GetDestination(),
		DepartureTime:  f.GetDepartureTime().AsTime(),
		ArrivalTime:    f.GetArrivalTime().AsTime(),
		TotalSeats:     f.GetTotalSeats(),
		AvailableSeats: f.GetAvailableSeats(),
		Price:          toMoneyDTO(f.GetPrice().GetAmountMinor(), f.GetPrice().GetCurrencyCode()),
		Status:         flightStatusNames[f.GetStatus()],
	}
}

type bookingDTO struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	FlightID       int64     `json:"flight_id"`
	PassengerName  string    `json:"passenger_name"`
	PassengerEmail string    `json:"passenger_email"`
	SeatCount      int32     `json:"seat_count"`
	TotalPrice     moneyDTO  `json:"total_price"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func toBookingDTO(b Booking) bookingDTO {
	return bookingDTO{
		ID:             b.ID,
		UserID:         b.UserID,
		FlightID:       b.FlightID,
		PassengerName:  b.PassengerName,
		PassengerEmail: b.PassengerEmail,
		SeatCount:      b.SeatCount,
		TotalPrice:     toMoneyDTO(b.TotalPriceMinor, b.Currency),
		Status:         b.Status,
		CreatedAt:      b.CreatedAt,
		UpdatedAt:      b.UpdatedAt,
	}
}
