-- schema.sql
-- Postgres 13+ recommended

CREATE EXTENSION IF NOT EXISTS pgcrypto;
CREATE EXTENSION IF NOT EXISTS postgis;

-- ----------------------------
-- Enums (as TEXT + CHECK)
-- ----------------------------

-- USERS
-- role: client | runner | admin
-- status: pending | active | suspended | deleted

-- RUNNER
-- kyc_status: unsubmitted | pending | approved | rejected

-- ERRANDS
-- errand_status:
-- draft | posted | bidding | assigned | in_progress | delivered | completed | cancelled | disputed | expired

-- OFFER
-- offer_status: pending | accepted | rejected | withdrawn | expired

-- EVENTS
-- event_type matches errand_status + additional events like "location_ping", "note_added", etc. (keep flexible)

-- WALLET LEDGER
-- ledger_type:
-- escrow_hold | escrow_release | errand_payout | platform_fee | refund | tip | withdrawal_request | withdrawal_paid | adjustment

-- TRANSACTIONS
-- tx_status: initiated | pending | success | failed | reversed

-- DISPUTES
-- dispute_status: open | under_review | resolved | rejected
-- dispute_resolution: refund_client | pay_runner | split | no_action

-- NOTIFICATIONS
-- notification_status: unread | read

-- ----------------------------
-- Core Tables
-- ----------------------------

CREATE TABLE IF NOT EXISTS service_regions (
                                               id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    code       TEXT UNIQUE, -- e.g. "KE-047"
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE TABLE IF NOT EXISTS users (
                                     id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    phone        TEXT UNIQUE NOT NULL,
    -- email is UNIQUE but nullable (multiple NULLs allowed by Postgres UNIQUE).
    -- App layer enforces email presence for password-based auth.
    -- Future OTP-only users may omit email.
    email        TEXT UNIQUE,
    name         TEXT NOT NULL,
    role         TEXT NOT NULL CHECK (role IN ('client', 'runner', 'admin')),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'active', 'suspended', 'deleted')),
    password     TEXT, -- optional if you do OTP-only; keep for flexibility
    last_lat     DOUBLE PRECISION,
    last_lng     DOUBLE PRECISION,
    last_geo     GEOGRAPHY(Point, 4326),
    last_seen_at TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_users_role     ON users (role);
CREATE INDEX IF NOT EXISTS idx_users_status   ON users (status);
CREATE INDEX IF NOT EXISTS idx_users_last_geo ON users USING GIST (last_geo);

CREATE TABLE IF NOT EXISTS user_sessions (
                                             id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    refresh_token TEXT        NOT NULL,
    expires_at    TIMESTAMPTZ NOT NULL,
    revoked_at    TIMESTAMPTZ,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_sessions_user ON user_sessions (user_id);

-- ----------------------------
-- Client Profile
-- ----------------------------

CREATE TABLE IF NOT EXISTS client_profiles (
                                               user_id            UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    bio                TEXT,
    profile_picture VARCHAR(256), 
    preferred_currency VARCHAR(10) NOT NULL DEFAULT 'KES',
    -- Nullable FK; intentional — not every client sets a default region.
    default_region_id  UUID REFERENCES service_regions (id),
    total_errands      INT         NOT NULL DEFAULT 0,
    active_errands     INT         NOT NULL DEFAULT 0,
    dispute_count      INT         NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
    );

-- ----------------------------
-- Runner Profile & Coverage
-- ----------------------------

CREATE TABLE IF NOT EXISTS runner_profiles (
                                               user_id         UUID PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    bio             TEXT,
    kyc_status      TEXT        NOT NULL DEFAULT 'unsubmitted'
    CHECK (kyc_status IN ('unsubmitted', 'pending', 'approved', 'rejected')),
    vehicle_type    TEXT, -- foot/bike/car/etc (keep freeform)
    vehicle_plate   TEXT,
    is_available    BOOLEAN     NOT NULL DEFAULT FALSE,
    rating_avg      NUMERIC(3, 2) NOT NULL DEFAULT 0,
    rating_count    INT         NOT NULL DEFAULT 0,
    completed_count INT         NOT NULL DEFAULT 0,
    cancelled_count INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE TABLE IF NOT EXISTS runner_kyc_documents (
                                                    id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    doc_type    TEXT        NOT NULL, -- national_id, selfie, license, etc.
    doc_url     TEXT        NOT NULL,
    status      TEXT        NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'approved', 'rejected')),
    notes       TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    reviewed_at TIMESTAMPTZ,
    reviewed_by UUID REFERENCES users (id)
    );

CREATE INDEX IF NOT EXISTS idx_runner_docs_user ON runner_kyc_documents (user_id);

-- Runner can serve multiple regions (countrywide + filtered availability)
CREATE TABLE IF NOT EXISTS runner_service_areas (
                                                    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    region_id  UUID REFERENCES service_regions (id),
    label      TEXT, -- e.g. "Nairobi CBD", "Thika Road"
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_runner_areas_user   ON runner_service_areas (user_id);
CREATE INDEX IF NOT EXISTS idx_runner_areas_region ON runner_service_areas (region_id);

-- ----------------------------
-- Errands (multi-stop + scheduling + bids + instant accept)
-- ----------------------------

CREATE TABLE IF NOT EXISTS errands (
                                       id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    client_id          UUID        NOT NULL REFERENCES users (id),
    title              TEXT        NOT NULL,
    description        TEXT,
    category           TEXT        NOT NULL, -- shopping, delivery, pickup, queueing, bills, etc.
    currency           TEXT        NOT NULL DEFAULT 'KES',

    allow_bids         BOOLEAN     NOT NULL DEFAULT TRUE,
    instant_accept     BOOLEAN     NOT NULL DEFAULT TRUE,
    bidding_ends_at    TIMESTAMPTZ,          -- if allow_bids
    budget_min         NUMERIC(12, 2),
    budget_max         NUMERIC(12, 2),
    fixed_price        NUMERIC(12, 2),       -- optional if client sets a fixed price

    scheduled_at       TIMESTAMPTZ,          -- if scheduled
    expires_at         TIMESTAMPTZ,          -- when posting expires

    status             TEXT        NOT NULL DEFAULT 'posted'
    CHECK (status IN (
           'draft', 'posted', 'bidding', 'assigned', 'in_progress',
           'delivered', 'completed', 'cancelled', 'disputed', 'expired'
                     )),

    assigned_runner_id UUID REFERENCES users (id),

    -- accepted_offer_id references errand_offers but that table references errands,
    -- creating a circular FK. We use a DEFERRABLE constraint to resolve this.
    -- Set the FK after inserting both rows within the same transaction.
    accepted_offer_id  UUID,

    region_id          UUID REFERENCES service_regions (id),

    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_errands_client ON errands (client_id);
CREATE INDEX IF NOT EXISTS idx_errands_status ON errands (status);
CREATE INDEX IF NOT EXISTS idx_errands_runner ON errands (assigned_runner_id);
CREATE INDEX IF NOT EXISTS idx_errands_region ON errands (region_id);

-- Multi-stop stops (pickup / dropoff / stop)
CREATE TABLE IF NOT EXISTS errand_stops (
                                            id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id     UUID        NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    seq           INT         NOT NULL, -- 1..N in order
    stop_type     TEXT        NOT NULL CHECK (stop_type IN ('pickup', 'dropoff', 'stop')),

    address_label TEXT,
    address_text  TEXT,
    contact_name  TEXT,
    contact_phone TEXT,

    lat           DOUBLE PRECISION,
    lng           DOUBLE PRECISION,
    geo           GEOGRAPHY(Point, 4326),

    instructions  TEXT,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE UNIQUE INDEX IF NOT EXISTS uq_stop_seq  ON errand_stops (errand_id, seq);
CREATE INDEX IF NOT EXISTS idx_stops_geo       ON errand_stops USING GIST (geo);

-- Offers (bids) — also used for instant-accept pricing
CREATE TABLE IF NOT EXISTS errand_offers (
                                             id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id   UUID          NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    runner_id   UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    amount      NUMERIC(12, 2) NOT NULL,
    currency    TEXT          NOT NULL DEFAULT 'KES',
    eta_minutes INT,
    message     TEXT,
    status      TEXT          NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'accepted', 'rejected', 'withdrawn', 'expired')),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_offers_errand ON errand_offers (errand_id);
CREATE INDEX IF NOT EXISTS idx_offers_runner ON errand_offers (runner_id);
CREATE INDEX IF NOT EXISTS idx_offers_status ON errand_offers (status);

-- Wire up the deferred circular FK now that errand_offers exists
ALTER TABLE errands
    ADD CONSTRAINT fk_errands_accepted_offer
        FOREIGN KEY (accepted_offer_id)
            REFERENCES errand_offers (id)
            DEFERRABLE INITIALLY DEFERRED;

-- Timeline / audit of status changes & important events
CREATE TABLE IF NOT EXISTS errand_events (
                                             id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id     UUID        NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    actor_user_id UUID REFERENCES users (id),
    event_type    TEXT        NOT NULL, -- flexible: accepted, picked_up, delivered, etc.
    notes         TEXT,
    metadata      JSONB       NOT NULL DEFAULT '{}'::jsonb,
    occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_events_errand ON errand_events (errand_id);
CREATE INDEX IF NOT EXISTS idx_events_time   ON errand_events (occurred_at);

-- ----------------------------
-- Wallets
-- ----------------------------

-- Wallets are auto-created for every user on registration (both client & runner).
-- Runners need a wallet to receive payouts; clients need one for escrow top-ups.
CREATE TABLE IF NOT EXISTS wallets (
                                       id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID          UNIQUE NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    currency   TEXT          NOT NULL DEFAULT 'KES',
    balance    NUMERIC(14, 2) NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_wallets_user ON wallets (user_id);

-- Ledger is the source of truth; balance is kept denormalised for fast reads.
CREATE TABLE IF NOT EXISTS wallet_ledger (
                                             id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    wallet_id    UUID          NOT NULL REFERENCES wallets (id) ON DELETE CASCADE,
    user_id      UUID          NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    ledger_type  TEXT          NOT NULL
    CHECK (ledger_type IN (
           'escrow_hold', 'escrow_release', 'errand_payout', 'platform_fee',
           'refund', 'tip', 'withdrawal_request', 'withdrawal_paid', 'adjustment'
                          )),
    amount       NUMERIC(14, 2) NOT NULL, -- +credit, -debit
    currency     TEXT          NOT NULL DEFAULT 'KES',
    reference_id UUID,                   -- errand_id / dispute_id / withdrawal_id
    metadata     JSONB         NOT NULL DEFAULT '{}'::jsonb,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_ledger_wallet ON wallet_ledger (wallet_id);
CREATE INDEX IF NOT EXISTS idx_ledger_user   ON wallet_ledger (user_id);
CREATE INDEX IF NOT EXISTS idx_ledger_ref    ON wallet_ledger (reference_id);

-- Escrow per errand
CREATE TABLE IF NOT EXISTS escrow_accounts (
                                               id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id   UUID          UNIQUE NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    client_id   UUID          NOT NULL REFERENCES users (id),
    amount_held NUMERIC(14, 2) NOT NULL DEFAULT 0,
    currency    TEXT          NOT NULL DEFAULT 'KES',
    status      TEXT          NOT NULL DEFAULT 'held'
    CHECK (status IN ('held', 'released', 'refunded', 'partial')),
    created_at  TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_escrow_client ON escrow_accounts (client_id);

-- External payment transactions (M-Pesa, etc.) + idempotency key
CREATE TABLE IF NOT EXISTS payment_transactions (
                                                    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id         UUID          NOT NULL REFERENCES users (id),
    errand_id       UUID REFERENCES errands (id),
    provider        TEXT          NOT NULL, -- mpesa / card / bank
    provider_ref    TEXT,                   -- e.g. M-Pesa receipt number
    idempotency_key TEXT          UNIQUE NOT NULL,
    amount          NUMERIC(14, 2) NOT NULL,
    currency        TEXT          NOT NULL DEFAULT 'KES',
    status          TEXT          NOT NULL DEFAULT 'initiated'
    CHECK (status IN ('initiated', 'pending', 'success', 'failed', 'reversed')),
    raw_payload     JSONB         NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_payments_user   ON payment_transactions (user_id);
CREATE INDEX IF NOT EXISTS idx_payments_errand ON payment_transactions (errand_id);

-- Withdrawals
CREATE TABLE IF NOT EXISTS withdrawals (
                                           id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID          NOT NULL REFERENCES users (id),
    amount       NUMERIC(14, 2) NOT NULL,
    currency     TEXT          NOT NULL DEFAULT 'KES',
    destination  TEXT          NOT NULL, -- masked mpesa number / bank account
    status       TEXT          NOT NULL DEFAULT 'pending'
    CHECK (status IN ('pending', 'processing', 'paid', 'failed', 'reversed')),
    provider_ref TEXT,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT now(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_withdrawals_user   ON withdrawals (user_id);
CREATE INDEX IF NOT EXISTS idx_withdrawals_status ON withdrawals (status);

-- ----------------------------
-- Proof of Delivery
-- ----------------------------

CREATE TABLE IF NOT EXISTS delivery_proofs (
                                               id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id  UUID        NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    stop_id    UUID REFERENCES errand_stops (id) ON DELETE SET NULL,
    runner_id  UUID REFERENCES users (id),
    proof_type TEXT        NOT NULL CHECK (proof_type IN ('photo', 'signature', 'otp', 'note')),
    proof_url  TEXT,      -- for photo / signature
    otp_hash   TEXT,      -- store bcrypt hash, never the raw OTP
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_proofs_errand ON delivery_proofs (errand_id);

-- ----------------------------
-- Chat
-- ----------------------------

CREATE TABLE IF NOT EXISTS conversations (
                                             id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id  UUID UNIQUE NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE TABLE IF NOT EXISTS messages (
                                        id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID        NOT NULL REFERENCES conversations (id) ON DELETE CASCADE,
    sender_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    message_type    TEXT        NOT NULL DEFAULT 'text'
    CHECK (message_type IN ('text', 'image', 'file', 'system')),
    content         TEXT,
    attachment_url  TEXT,
    metadata        JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at         TIMESTAMPTZ
    );

CREATE INDEX IF NOT EXISTS idx_messages_conv   ON messages (conversation_id);
CREATE INDEX IF NOT EXISTS idx_messages_sender ON messages (sender_id);

-- ----------------------------
-- Reviews
-- ----------------------------

-- UNIQUE on (errand_id, reviewer_id) allows both parties to leave a review.
-- Original UNIQUE on errand_id alone would have blocked the second reviewer.
CREATE TABLE IF NOT EXISTS reviews (
                                       id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id   UUID        NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    reviewer_id UUID        NOT NULL REFERENCES users (id),
    reviewee_id UUID        NOT NULL REFERENCES users (id),
    rating      INT         NOT NULL CHECK (rating BETWEEN 1 AND 5),
    comment     TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (errand_id, reviewer_id)
    );

CREATE INDEX IF NOT EXISTS idx_reviews_reviewee ON reviews (reviewee_id);

-- ----------------------------
-- Disputes
-- ----------------------------

CREATE TABLE IF NOT EXISTS disputes (
                                        id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id         UUID UNIQUE NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    opened_by_user_id UUID        NOT NULL REFERENCES users (id),
    reason            TEXT        NOT NULL,
    status            TEXT        NOT NULL DEFAULT 'open'
    CHECK (status IN ('open', 'under_review', 'resolved', 'rejected')),
    resolution        TEXT
    CHECK (resolution IN ('refund_client', 'pay_runner', 'split', 'no_action')),
    resolution_notes  TEXT,
    amount_client     NUMERIC(14, 2),
    amount_runner     NUMERIC(14, 2),
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at       TIMESTAMPTZ,
    resolved_by       UUID REFERENCES users (id)
    );

CREATE TABLE IF NOT EXISTS dispute_evidence (
                                                id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    dispute_id UUID        NOT NULL REFERENCES disputes (id) ON DELETE CASCADE,
    user_id    UUID REFERENCES users (id),
    ev_type    TEXT        NOT NULL CHECK (ev_type IN ('photo', 'file', 'message', 'note')),
    url        TEXT,
    note       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_evidence_dispute ON dispute_evidence (dispute_id);

-- ----------------------------
-- Notifications
-- ----------------------------

CREATE TABLE IF NOT EXISTS notifications (
                                             id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    title      TEXT        NOT NULL,
    body       TEXT        NOT NULL,
    data       JSONB       NOT NULL DEFAULT '{}'::jsonb,
    status     TEXT        NOT NULL DEFAULT 'unread' CHECK (status IN ('unread', 'read')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at    TIMESTAMPTZ
    );

CREATE INDEX IF NOT EXISTS idx_notifications_user   ON notifications (user_id);
CREATE INDEX IF NOT EXISTS idx_notifications_status ON notifications (status);

-- ----------------------------
-- Admin Audit Logs
-- ----------------------------

CREATE TABLE IF NOT EXISTS audit_logs (
                                          id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    actor_id    UUID REFERENCES users (id),
    action      TEXT        NOT NULL,
    entity_type TEXT        NOT NULL,
    entity_id   UUID,
    metadata    JSONB       NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
    );

CREATE INDEX IF NOT EXISTS idx_audit_actor  ON audit_logs (actor_id);
CREATE INDEX IF NOT EXISTS idx_audit_entity ON audit_logs (entity_type, entity_id);