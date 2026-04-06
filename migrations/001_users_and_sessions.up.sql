-- Migration: 001_users_and_sessions.up.sql

-- pgcrypto: used for gen_random_uuid() — available on Neon by default.
CREATE EXTENSION IF NOT EXISTS "pgcrypto";

-- PostGIS: used for the runner feed radius query in migration 003.
--
-- ON NEON: Enable PostGIS from the Neon Console before running migrations:
--   Dashboard → your project → Extensions → search "postgis" → Enable.
-- Neon supports PostGIS on all plans. It takes about 30 seconds to activate.
-- Once enabled, the line below is a no-op on subsequent migration runs.
CREATE EXTENSION IF NOT EXISTS "postgis";

CREATE TYPE user_role   AS ENUM ('client', 'runner', 'admin');
CREATE TYPE user_status AS ENUM ('pending', 'active', 'suspended', 'deleted');

CREATE TABLE users (
                       id           UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
                       phone        TEXT        NOT NULL UNIQUE,
                       email        TEXT        UNIQUE,
                       name         TEXT,
                       role         user_role   NOT NULL DEFAULT 'client',
                       status       user_status NOT NULL DEFAULT 'active',
                       password     TEXT,
                       last_lat     DOUBLE PRECISION,
                       last_lng     DOUBLE PRECISION,
                       last_seen_at TIMESTAMPTZ,
                       created_at   TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                       updated_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_users_phone  ON users (phone);
CREATE INDEX idx_users_role   ON users (role);
CREATE INDEX idx_users_status ON users (status);

CREATE TABLE user_sessions (
                               id            UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
                               user_id       UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
                               refresh_token TEXT        NOT NULL UNIQUE,
                               expires_at    TIMESTAMPTZ NOT NULL,
                               revoked_at    TIMESTAMPTZ,
                               created_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_sessions_user_id ON user_sessions (user_id);
CREATE INDEX idx_sessions_token   ON user_sessions (refresh_token);

CREATE TABLE device_tokens (
                               id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
                               user_id    UUID        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
                               fcm_token  TEXT        NOT NULL,
                               platform   TEXT        NOT NULL CHECK (platform IN ('android', 'ios')),
                               updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                               UNIQUE (user_id, platform)
);
