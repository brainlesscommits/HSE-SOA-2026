-- +goose Up
INSERT INTO airlines (code, name) VALUES
    ('SU', 'Aeroflot'),
    ('S7', 'S7 Airlines'),
    ('DP', 'Pobeda'),
    ('U6', 'Ural Airlines');

INSERT INTO airports (code, name, city) VALUES
    ('SVO', 'Sheremetyevo', 'Moscow'),
    ('VKO', 'Vnukovo', 'Moscow'),
    ('DME', 'Domodedovo', 'Moscow'),
    ('LED', 'Pulkovo', 'Saint Petersburg'),
    ('AER', 'Sochi', 'Sochi'),
    ('KZN', 'Kazan', 'Kazan'),
    ('SVX', 'Koltsovo', 'Yekaterinburg');

INSERT INTO flights (airline_code, flight_no, origin, destination, departure_time, arrival_time,
                     total_seats, available_seats, price, status) VALUES
    ('SU', '1234', 'SVO', 'LED', '2026-12-01 08:00+00', '2026-12-01 09:30+00', 150, 150,  5500.00, 'SCHEDULED'),
    ('SU', '1236', 'SVO', 'LED', '2026-12-01 14:00+00', '2026-12-01 15:30+00', 150, 150,  6200.00, 'SCHEDULED'),
    ('S7', '21', 'SVO', 'LED', '2026-12-01 18:00+00', '2026-12-01 19:25+00', 120, 120,  4900.00, 'CANCELLED'),
    ('SU', '1234', 'SVO', 'LED', '2026-12-02 08:00+00', '2026-12-02 09:30+00', 150, 150,  5300.00, 'SCHEDULED'),
    ('DP', '6001', 'VKO', 'AER', '2026-12-05 06:10+00', '2026-12-05 08:40+00', 189, 189,  3999.00, 'SCHEDULED'),
    ('U6', '263', 'DME', 'SVX', '2026-12-03 10:00+00', '2026-12-03 12:20+00', 160, 160,  7800.50, 'SCHEDULED'),
    ('S7', '1001', 'LED', 'KZN', '2026-12-04 07:00+00', '2026-12-04 09:00+00',   2,   2, 12000.00, 'SCHEDULED');

-- +goose Down
DELETE FROM flights;
DELETE FROM airports;
DELETE FROM airlines;
