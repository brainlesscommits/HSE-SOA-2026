# ER-диаграмма

Две независимые БД: `flights` (Flight Service) и `bookings` (Booking Service).
Связь `bookings.flight_id → flights.id` и `seat_reservations.booking_id → bookings.id`
существует только логически (через gRPC): внешний ключ между разными БД невозможен.

```mermaid
erDiagram
    %% ===== Flight Service DB (flights) =====
    AIRLINES {
        char2 code PK "IATA, CHECK ^[A-Z0-9]{2}$"
        text name "NOT NULL, не пустое"
    }
    AIRPORTS {
        char3 code PK "IATA, CHECK ^[A-Z]{3}$"
        text name "NOT NULL"
        text city "NOT NULL"
    }
    FLIGHTS {
        bigint id PK
        char2 airline_code FK "→ airlines.code"
        varchar4 flight_no "цифры номера рейса, CHECK ^[0-9]{1,4}$"
        char3 origin FK "→ airports.code"
        char3 destination FK "→ airports.code, CHECK <> origin"
        timestamptz departure_time "NOT NULL"
        timestamptz arrival_time "CHECK > departure_time"
        int total_seats "CHECK > 0"
        int available_seats "CHECK 0..total_seats"
        numeric price "numeric(12,2), CHECK > 0"
        char3 currency "ISO 4217"
        flight_status status "SCHEDULED|DEPARTED|CANCELLED|COMPLETED"
        timestamptz created_at
        timestamptz updated_at
    }
    SEAT_RESERVATIONS {
        bigint id PK
        bigint flight_id FK "→ flights.id"
        uuid booking_id UK "1 бронирование = 1 резервация"
        int seat_count "CHECK > 0"
        reservation_status status "ACTIVE|RELEASED|EXPIRED"
        timestamptz created_at
        timestamptz updated_at
    }

    %% ===== Booking Service DB (bookings) =====
    BOOKINGS {
        uuid id PK "генерируется до ReserveSeats"
        varchar64 user_id "NOT NULL"
        bigint flight_id "логическая ссылка на flights.id, CHECK > 0"
        varchar200 passenger_name "NOT NULL"
        varchar254 passenger_email "CHECK формат email"
        int seat_count "CHECK > 0"
        numeric total_price "numeric(12,2), CHECK > 0, снимок цены"
        char3 currency "ISO 4217"
        booking_status status "CONFIRMED|CANCELLED"
        timestamptz created_at
        timestamptz updated_at
    }

    AIRLINES ||--o{ FLIGHTS : "выполняет"
    AIRPORTS ||--o{ FLIGHTS : "вылет из (origin)"
    AIRPORTS ||--o{ FLIGHTS : "прилёт в (destination)"
    FLIGHTS  ||--o{ SEAT_RESERVATIONS : "места"
    BOOKINGS ||..|| SEAT_RESERVATIONS : "booking_id (между сервисами, gRPC)"
    BOOKINGS }o..|| FLIGHTS : "flight_id (между сервисами, gRPC)"
```

## Ограничения целостности

| Таблица | Ограничение |
|---|---|
| flights | `UNIQUE (airline_code, flight_no, (departure_time AT TIME ZONE 'UTC')::date)` — номер рейса + дата вылета уникальны |
| flights | `total_seats > 0`, `price > 0`, `available_seats >= 0`, `available_seats <= total_seats` |
| flights | `origin <> destination`, `arrival_time > departure_time` |
| flights | FK на `airlines`, `airports` (origin и destination) |
| seat_reservations | `UNIQUE (booking_id)`, `seat_count > 0`, FK на `flights` |
| bookings | `seat_count > 0`, `total_price > 0`, `flight_id > 0`, формат email, непустые `user_id` и `passenger_name` |
| все | статусы — PostgreSQL `ENUM`, недопустимое значение не записать |

## Почему это 3NF

* **1NF** — все атрибуты атомарны: номер рейса разделён на `airline_code` + `flight_no`,
  цена — число + код валюты, нет списков в колонках.
* **2NF** — у каждой таблицы суррогатный первичный ключ из одного атрибута; у `flights`
  есть ещё составной потенциальный ключ (авиакомпания, номер, дата вылета), и ни один
  неключевой атрибут не зависит от его части. Если бы хранился полный номер `SU1234`
  вместе с `airline_code`, то `airline_code` зависел бы от части ключа — поэтому
  хранится только числовая часть номера.
* **3NF** — нет транзитивных зависимостей: название авиакомпании вынесено в `airlines`,
  название и город аэропорта — в `airports`; в `flights` остались только коды (FK).
  `bookings` не дублирует данные рейса (маршрут, время) — только `flight_id`.

Осознанные решения, которые не нарушают 3NF, но стоит пояснить:

* `flights.available_seats` формально выводится из `total_seats − Σ seat_count` активных
  резерваций. Это требование задания и счётчик, который блокируется `SELECT … FOR UPDATE`
  при резервировании; согласованность обеспечивает транзакция и `CHECK`.
* `bookings.total_price` — не производное от текущей цены рейса, а **снимок** цены на
  момент бронирования (цена рейса может измениться позже).
* `passenger_name`/`passenger_email` — данные пассажира конкретного бронирования
  (один пользователь бронирует на разных людей), а не атрибуты `user_id`.
