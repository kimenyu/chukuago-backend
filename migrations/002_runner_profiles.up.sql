-- Migration: 002_runner_profiles.up.sql

CREATE TYPE kyc_status AS ENUM ('unsubmitted', 'pending', 'approved', 'rejected');

CREATE TABLE runner_profiles (
    user_id         UUID        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    bio             TEXT,
    kyc_status      kyc_status  NOT NULL DEFAULT 'unsubmitted',
    vehicle_type    TEXT        CHECK (vehicle_type IN ('motorbike','bicycle','car','van','foot')),
    vehicle_plate   TEXT,
    is_available    BOOLEAN     NOT NULL DEFAULT FALSE,
    rating_avg      NUMERIC(3,2) NOT NULL DEFAULT 0,
    rating_count    INT         NOT NULL DEFAULT 0,
    completed_count INT         NOT NULL DEFAULT 0,
    cancelled_count INT         NOT NULL DEFAULT 0,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- KYC documents — one submission can have multiple doc rows (front, back, selfie).
CREATE TABLE runner_kyc_documents (
    id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    doc_type     TEXT        NOT NULL CHECK (doc_type IN ('national_id_front','national_id_back','selfie')),
    doc_url      TEXT        NOT NULL,
    status       TEXT        NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','approved','rejected')),
    notes        TEXT,
    reviewed_at  TIMESTAMPTZ,
    reviewed_by  UUID        REFERENCES users (id),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_kyc_user_id ON runner_kyc_documents (user_id);
CREATE INDEX idx_kyc_status  ON runner_kyc_documents (status);

-- Service regions: Kenyan counties / custom zones used to scope the runner feed.
CREATE TABLE service_regions (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL UNIQUE,
    code       TEXT UNIQUE,           -- e.g. 'NBI' for Nairobi
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Seed the 47 counties for launch.
INSERT INTO service_regions (id, name, code) VALUES
    (gen_random_uuid(), 'Nairobi',      'NBI'),
    (gen_random_uuid(), 'Mombasa',      'MSA'),
    (gen_random_uuid(), 'Kisumu',       'KSM'),
    (gen_random_uuid(), 'Nakuru',       'NKR'),
    (gen_random_uuid(), 'Eldoret',      'ELD'),
    (gen_random_uuid(), 'Thika',        'THK'),
    (gen_random_uuid(), 'Malindi',      'MLN'),
    (gen_random_uuid(), 'Kitale',       'KTL'),
    (gen_random_uuid(), 'Garissa',      'GRS'),
    (gen_random_uuid(), 'Kakamega',     'KKM');

CREATE TABLE runner_service_areas (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    region_id  UUID REFERENCES service_regions (id) ON DELETE SET NULL,
    label      TEXT,                  -- free-text alternative to a region ID
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT area_has_region_or_label CHECK (region_id IS NOT NULL OR label IS NOT NULL)
);

CREATE INDEX idx_service_areas_user_id   ON runner_service_areas (user_id);
CREATE INDEX idx_service_areas_region_id ON runner_service_areas (region_id);

-- Client profiles
CREATE TABLE client_profiles (
    user_id            UUID        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    bio                TEXT,
    profile_pic        TEXT,
    preferred_currency CHAR(3)     NOT NULL DEFAULT 'KES',
    default_region_id  UUID        REFERENCES service_regions (id),
    total_errands      INT         NOT NULL DEFAULT 0,
    active_errands     INT         NOT NULL DEFAULT 0,
    dispute_count      INT         NOT NULL DEFAULT 0,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
