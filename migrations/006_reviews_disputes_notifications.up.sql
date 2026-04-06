-- Migration: 006_reviews_disputes_notifications.up.sql

-- ── Reviews ─────────────────────────────────────────────────────────────────

CREATE TABLE reviews (
                         id          UUID    PRIMARY KEY DEFAULT gen_random_uuid(),
                         errand_id   UUID    NOT NULL REFERENCES errands (id) ON DELETE CASCADE,
                         reviewer_id UUID    NOT NULL REFERENCES users (id),
                         reviewee_id UUID    NOT NULL REFERENCES users (id),
                         rating      SMALLINT NOT NULL CHECK (rating BETWEEN 1 AND 5),
                         comment     TEXT,
                         created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW(),

    -- One review per reviewer per errand.
                         UNIQUE (errand_id, reviewer_id)
);

CREATE INDEX idx_reviews_errand_id   ON reviews (errand_id);
CREATE INDEX idx_reviews_reviewee_id ON reviews (reviewee_id);

-- ── Disputes ─────────────────────────────────────────────────────────────────

CREATE TYPE dispute_status     AS ENUM ('open', 'under_review', 'resolved', 'rejected');
CREATE TYPE dispute_resolution AS ENUM ('refund_client', 'pay_runner', 'split', 'no_action');

CREATE TABLE disputes (
                          id                UUID              PRIMARY KEY DEFAULT gen_random_uuid(),
                          errand_id         UUID              NOT NULL REFERENCES errands (id),
                          opened_by_user_id UUID              NOT NULL REFERENCES users (id),
                          reason            TEXT              NOT NULL,
                          status            dispute_status    NOT NULL DEFAULT 'open',
                          resolution        dispute_resolution,
                          resolution_notes  TEXT,
                          amount_client     NUMERIC(12,2),
                          amount_runner     NUMERIC(12,2),
                          created_at        TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
                          updated_at        TIMESTAMPTZ       NOT NULL DEFAULT NOW(),
                          resolved_at       TIMESTAMPTZ,
                          resolved_by       UUID              REFERENCES users (id)
);

CREATE INDEX idx_disputes_errand_id ON disputes (errand_id);
CREATE INDEX idx_disputes_status    ON disputes (status);

CREATE TABLE dispute_evidence (
                                  id         UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
                                  dispute_id UUID        NOT NULL REFERENCES disputes (id) ON DELETE CASCADE,
                                  user_id    UUID        REFERENCES users (id),
                                  ev_type    TEXT        NOT NULL CHECK (ev_type IN ('photo','file','note')),
                                  url        TEXT,
                                  note       TEXT,
                                  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_evidence_dispute_id ON dispute_evidence (dispute_id);

-- ── Notifications ─────────────────────────────────────────────────────────────

-- ?? Notifications (MVP-friendly, no partitioning) ????????????????????????????

CREATE TYPE notification_status AS ENUM ('unread', 'read');

CREATE TABLE notifications (
                               id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
                               user_id    UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
                               title      TEXT NOT NULL,
                               body       TEXT NOT NULL,
                               data       JSONB NOT NULL DEFAULT '{}',
                               status     notification_status NOT NULL DEFAULT 'unread',
                               created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
                               read_at    TIMESTAMPTZ
);

CREATE INDEX idx_notifs_user_status ON notifications (user_id, status);
CREATE INDEX idx_notifs_created_at  ON notifications (created_at DESC);

-- ── Audit Logs ───────────────────────────────────────────────────────────────

CREATE TABLE audit_logs (
                            id          UUID        PRIMARY KEY DEFAULT gen_random_uuid(),
                            actor_id    UUID        REFERENCES users (id),
                            action      TEXT        NOT NULL,
                            entity_type TEXT        NOT NULL,
                            entity_id   UUID,
                            metadata    JSONB       NOT NULL DEFAULT '{}',
                            created_at  TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_audit_actor_id    ON audit_logs (actor_id);
CREATE INDEX idx_audit_entity      ON audit_logs (entity_type, entity_id);
CREATE INDEX idx_audit_created     ON audit_logs (created_at DESC);
