-- +goose Up
CREATE TYPE flight_status AS ENUM ('SCHEDULED', 'DEPARTED', 'CANCELLED', 'COMPLETED');
CREATE TYPE reservation_status AS ENUM ('ACTIVE', 'RELEASED', 'EXPIRED');

CREATE TABLE airlines (
    code char(2) PRIMARY KEY CONSTRAINT airlines_code_chk CHECK (code ~ '^[A-Z0-9]{2}$'),
    name text    NOT NULL    CONSTRAINT airlines_name_chk CHECK (btrim(name) <> '')
);

CREATE TABLE airports (
    code char(3) PRIMARY KEY CONSTRAINT airports_code_chk CHECK (code ~ '^[A-Z]{3}$'),
    name text    NOT NULL    CONSTRAINT airports_name_chk CHECK (btrim(name) <> ''),
    city text    NOT NULL    CONSTRAINT airports_city_chk CHECK (btrim(city) <> '')
);

CREATE TABLE flights (
    id              bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    airline_code    char(2)       NOT NULL REFERENCES airlines (code),
    flight_no       varchar(4)    NOT NULL CONSTRAINT flights_flight_no_chk CHECK (flight_no ~ '^[0-9]{1,4}$'),
    origin          char(3)       NOT NULL REFERENCES airports (code),
    destination     char(3)       NOT NULL REFERENCES airports (code),
    departure_time  timestamptz   NOT NULL,
    arrival_time    timestamptz   NOT NULL,
    total_seats     integer       NOT NULL CONSTRAINT flights_total_seats_chk CHECK (total_seats > 0),
    available_seats integer       NOT NULL CONSTRAINT flights_available_seats_chk CHECK (available_seats >= 0),
    price           numeric(12,2) NOT NULL CONSTRAINT flights_price_chk CHECK (price > 0),
    currency        char(3)       NOT NULL DEFAULT 'RUB' CONSTRAINT flights_currency_chk CHECK (currency ~ '^[A-Z]{3}$'),
    status          flight_status NOT NULL DEFAULT 'SCHEDULED',
    created_at      timestamptz   NOT NULL DEFAULT now(),
    updated_at      timestamptz   NOT NULL DEFAULT now(),
    CONSTRAINT flights_seats_le_total_chk CHECK (available_seats <= total_seats),
    CONSTRAINT flights_route_chk CHECK (origin <> destination),
    CONSTRAINT flights_times_chk CHECK (arrival_time > departure_time)
);
CREATE UNIQUE INDEX flights_number_date_uq
    ON flights (airline_code, flight_no, ((departure_time AT TIME ZONE 'UTC')::date));

CREATE INDEX flights_search_idx
    ON flights (origin, destination, departure_time)
    WHERE status = 'SCHEDULED';

CREATE TABLE seat_reservations (
    id         bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    flight_id  bigint             NOT NULL REFERENCES flights (id),
    booking_id uuid               NOT NULL CONSTRAINT seat_reservations_booking_uq UNIQUE,
    seat_count integer            NOT NULL CONSTRAINT seat_reservations_seat_count_chk CHECK (seat_count > 0),
    status     reservation_status NOT NULL DEFAULT 'ACTIVE',
    created_at timestamptz        NOT NULL DEFAULT now(),
    updated_at timestamptz        NOT NULL DEFAULT now()
);

CREATE INDEX seat_reservations_flight_idx ON seat_reservations (flight_id);

-- +goose Down
DROP TABLE seat_reservations;
DROP TABLE flights;
DROP TABLE airports;
DROP TABLE airlines;
DROP TYPE reservation_status;
DROP TYPE flight_status;
