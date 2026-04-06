-- Migration: 007_functions_and_triggers.up.sql
-- Keeps denormalised counters in sync automatically so application code
-- never has to remember to update them.

-- ── updated_at trigger ───────────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION set_updated_at()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$;

-- Apply to every table that has an updated_at column.
DO $$
DECLARE
    tbl TEXT;
BEGIN
    FOREACH tbl IN ARRAY ARRAY[
        'users', 'runner_profiles', 'client_profiles', 'errands',
        'errand_offers', 'disputes'
    ]
    LOOP
        EXECUTE format(
            'CREATE TRIGGER trg_%s_updated_at
             BEFORE UPDATE ON %I
             FOR EACH ROW EXECUTE FUNCTION set_updated_at()',
            tbl, tbl
        );
    END LOOP;
END;
$$;

-- ── client_profiles counters ─────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION sync_client_errand_counters()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    -- Recalculate on any status change.
    UPDATE client_profiles SET
        total_errands  = (
            SELECT COUNT(*) FROM errands
            WHERE client_id = NEW.client_id
              AND status NOT IN ('draft','cancelled','expired')
        ),
        active_errands = (
            SELECT COUNT(*) FROM errands
            WHERE client_id = NEW.client_id
              AND status IN ('posted','bidding','assigned','in_progress','delivered')
        ),
        updated_at = NOW()
    WHERE user_id = NEW.client_id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_errand_status_client_counters
AFTER INSERT OR UPDATE OF status ON errands
FOR EACH ROW EXECUTE FUNCTION sync_client_errand_counters();

-- ── runner completed/cancelled counters ─────────────────────────────────────

CREATE OR REPLACE FUNCTION sync_runner_errand_counters()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.assigned_runner_id IS NULL THEN
        RETURN NEW;
    END IF;

    UPDATE runner_profiles SET
        completed_count = (
            SELECT COUNT(*) FROM errands
            WHERE assigned_runner_id = NEW.assigned_runner_id
              AND status = 'completed'
        ),
        cancelled_count = (
            SELECT COUNT(*) FROM errands
            WHERE assigned_runner_id = NEW.assigned_runner_id
              AND status = 'cancelled'
        ),
        updated_at = NOW()
    WHERE user_id = NEW.assigned_runner_id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_errand_status_runner_counters
AFTER UPDATE OF status ON errands
FOR EACH ROW
WHEN (OLD.status IS DISTINCT FROM NEW.status)
EXECUTE FUNCTION sync_runner_errand_counters();

-- ── client dispute counter ───────────────────────────────────────────────────

CREATE OR REPLACE FUNCTION sync_client_dispute_counter()
RETURNS TRIGGER LANGUAGE plpgsql AS $$
DECLARE
    v_client_id UUID;
BEGIN
    SELECT client_id INTO v_client_id FROM errands WHERE id = NEW.errand_id;

    UPDATE client_profiles SET
        dispute_count = (
            SELECT COUNT(*) FROM disputes d
            JOIN errands e ON e.id = d.errand_id
            WHERE e.client_id = v_client_id
              AND d.status NOT IN ('resolved','rejected')
        ),
        updated_at = NOW()
    WHERE user_id = v_client_id;
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_dispute_client_counter
AFTER INSERT ON disputes
FOR EACH ROW EXECUTE FUNCTION sync_client_dispute_counter();
