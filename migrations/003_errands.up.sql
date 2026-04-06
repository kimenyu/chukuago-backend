-- Migration: 003_errands.up.sql

CREATE TYPE errand_status AS ENUM (
    'draft', 'posted', 'bidding', 'assigned',
    'in_progress', 'delivered', 'completed',
    'cancelled', 'disputed', 'expired'
);

CREATE TYPE offer_status AS ENUM (
    'pending', 'accepted', 'rejected', 'withdrawn', 'expired'
);

CREATE TYPE stop_type AS ENUM ('pickup', 'dropoff', 'stop');

CREATE TABLE errands (
    id                 UUID          PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id          UUID          NOT NULL REFERENCES users (id),
    title              TEXT          NOT NULL,
    description        TEXT,
    category           TEXT          NOT NULL,
    currency           CHAR(3)       NOT NULL DEFAULT 'KES',

    -- Pricing model: either fixed price or bid range
    allow_bids         BOOLEAN       NOT NULL DEFAULT TRUE,
    instant_accept     BOOLEAN       NOT NULL DEFAULT FALSE,
    bidding_ends_at    TIMESTAMPTZ,
    budget_min         NUMERIC(12,2),
    budget_max         NUMERIC(12,2),
    fixed_price        NUMERIC(12,2),

    scheduled_at       TIMESTAMPTZ,
    expires_at         TIMESTAMPTZ,

    status             errand_status NOT NULL DEFAULT 'posted',
    assigned_runner_id UUID          REFERENCES users (id),
    accepted_offer_id  UUID,         -- FK set after offers table exists (see below)
    region_id          UUID          REFERENCES service_regions (id),

    created_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ   NOT NULL DEFAULT NOW(),

    CONSTRAINT price_model CHECK (
        fixed_price IS NOT NULL OR (budget_min IS NOT NULL OR budget_max IS NOT NULL)
    )
);

CREATE INDEX idx_errands_client_id  ON errands (client_id);
CREATE INDEX idx_errands_status     ON errands (status);
CREATE INDEX idx_errands_region_id  ON errands (region_id);
CREATE INDEX idx_errands_runner_id  ON errands (assigned_runner_id);
CREATE INDEX idx_errands_created    ON errands (created_at DESC);

CREATE TABLE errand_stops (
    id            UUID      PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id     UUID      NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    seq           SMALLINT  NOT NULL,                -- ordering: 1, 2, 3 …
    stop_type     stop_type NOT NULL,
    address_label TEXT,
    address_text  TEXT,
    contact_name  TEXT,
    contact_phone TEXT,
    lat           DOUBLE PRECISION,
    lng           DOUBLE PRECISION,
    instructions  TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (errand_id, seq)
);

CREATE INDEX idx_stops_errand_id ON errand_stops (errand_id);

-- PostGIS spatial index on stop coordinates for runner feed radius queries.
CREATE INDEX idx_stops_geo ON errand_stops
    USING GIST (ST_MakePoint(lng, lat));

CREATE TABLE errand_offers (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id   UUID         NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    runner_id   UUID         NOT NULL REFERENCES users (id),
    amount      NUMERIC(12,2) NOT NULL CHECK (amount > 0),
    currency    CHAR(3)      NOT NULL DEFAULT 'KES',
    eta_minutes SMALLINT     CHECK (eta_minutes > 0),
    message     TEXT,
    status      offer_status NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),

    -- One active bid per runner per errand.
    UNIQUE (errand_id, runner_id)
);

CREATE INDEX idx_offers_errand_id ON errand_offers (errand_id);
CREATE INDEX idx_offers_runner_id ON errand_offers (runner_id);
CREATE INDEX idx_offers_status    ON errand_offers (status);

-- Back-fill the FK now that errand_offers exists.
ALTER TABLE errands
    ADD CONSTRAINT fk_errands_accepted_offer
    FOREIGN KEY (accepted_offer_id) REFERENCES errand_offers (id)
    DEFERRABLE INITIALLY DEFERRED;

-- Immutable audit trail — never updated, only inserted.
CREATE TABLE errand_events (
    id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id     UUID        NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    actor_user_id UUID        REFERENCES users (id),
    event_type    TEXT        NOT NULL,
    notes         TEXT,
    metadata      JSONB       NOT NULL DEFAULT '{}',
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_events_errand_id  ON errand_events (errand_id);
CREATE INDEX idx_events_occurred   ON errand_events (occurred_at DESC);
