CREATE TYPE discount_type AS ENUM ('PERCENTAGE', 'FIXED_AMOUNT');

CREATE TABLE promo_codes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    code VARCHAR(20) NOT NULL UNIQUE,
    discount_type discount_type NOT NULL,
    discount_value DECIMAL(12,2) NOT NULL CHECK (discount_value >= 0),
    min_order_amount DECIMAL(12,2) NOT NULL CHECK (min_order_amount >= 0),
    max_uses INTEGER NOT NULL CHECK (max_uses > 0),
    current_uses INTEGER NOT NULL DEFAULT 0 CHECK (current_uses >= 0),
    valid_from TIMESTAMPTZ NOT NULL,
    valid_until TIMESTAMPTZ NOT NULL,
    active BOOLEAN NOT NULL DEFAULT true,
    CHECK (valid_until >= valid_from),
    CHECK (current_uses <= max_uses),
    CHECK (discount_type <> 'PERCENTAGE' OR discount_value <= 100)
);
