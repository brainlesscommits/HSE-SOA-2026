-- +goose Up
CREATE TYPE booking_status AS ENUM ('CONFIRMED', 'CANCELLED');

CREATE TABLE bookings (
    id              uuid           PRIMARY KEY,
    user_id         varchar(64)    NOT NULL CONSTRAINT bookings_user_id_chk CHECK (btrim(user_id) <> ''),
    flight_id       bigint         NOT NULL CONSTRAINT bookings_flight_id_chk CHECK (flight_id > 0),
    passenger_name  varchar(200)   NOT NULL CONSTRAINT bookings_passenger_name_chk CHECK (btrim(passenger_name) <> ''),
    passenger_email varchar(254)   NOT NULL CONSTRAINT bookings_passenger_email_chk CHECK (passenger_email ~ '^[^@\s]+@[^@\s]+\.[^@\s]+$'),
    seat_count      integer        NOT NULL CONSTRAINT bookings_seat_count_chk CHECK (seat_count > 0),
    total_price     numeric(12,2)  NOT NULL CONSTRAINT bookings_total_price_chk CHECK (total_price > 0),
    currency        char(3)        NOT NULL CONSTRAINT bookings_currency_chk CHECK (currency ~ '^[A-Z]{3}$'),
    status          booking_status NOT NULL DEFAULT 'CONFIRMED',
    created_at      timestamptz    NOT NULL DEFAULT now(),
    updated_at      timestamptz    NOT NULL DEFAULT now()
);

CREATE INDEX bookings_user_idx ON bookings (user_id, created_at DESC);

-- +goose Down
DROP TABLE bookings;
DROP TYPE booking_status;
