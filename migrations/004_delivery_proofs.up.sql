-- Migration: 004_delivery_proofs.up.sql
-- Stores OTP-based proof of delivery. Raw OTPs are NEVER stored — only SHA-256 hashes.

CREATE TYPE proof_type AS ENUM ('photo', 'signature', 'otp', 'note');

CREATE TABLE delivery_proofs (
    id         UUID       PRIMARY KEY DEFAULT gen_random_uuid(),
    errand_id  UUID       NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
    stop_id    UUID       REFERENCES errand_stops (id),
    runner_id  UUID       REFERENCES users (id),
    proof_type proof_type NOT NULL,
    proof_url  TEXT,                      -- for photo/signature proofs
    otp_hash   TEXT,                      -- SHA-256 of raw OTP; NULL for non-OTP proofs
    attempts   SMALLINT   NOT NULL DEFAULT 0,
    verified   BOOLEAN    NOT NULL DEFAULT FALSE,
    expires_at TIMESTAMPTZ,               -- NULL for non-OTP proofs
    notes      TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- Only one active (unverified) OTP proof per errand at a time.
    -- Regenerating a code invalidates the previous one via ON CONFLICT DO UPDATE.
    CONSTRAINT uq_active_otp UNIQUE (errand_id, proof_type) DEFERRABLE INITIALLY DEFERRED
);

CREATE INDEX idx_proofs_errand_id ON delivery_proofs (errand_id);
CREATE INDEX idx_proofs_verified  ON delivery_proofs (errand_id, verified) WHERE verified = FALSE;
