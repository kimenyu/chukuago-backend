DROP TRIGGER IF EXISTS trg_dispute_client_counter         ON disputes;
DROP TRIGGER IF EXISTS trg_errand_status_runner_counters  ON errands;
DROP TRIGGER IF EXISTS trg_errand_status_client_counters  ON errands;
DROP TRIGGER IF EXISTS trg_disputes_updated_at            ON disputes;
DROP TRIGGER IF EXISTS trg_errand_offers_updated_at       ON errand_offers;
DROP TRIGGER IF EXISTS trg_errands_updated_at             ON errands;
DROP TRIGGER IF EXISTS trg_client_profiles_updated_at     ON client_profiles;
DROP TRIGGER IF EXISTS trg_runner_profiles_updated_at     ON runner_profiles;
DROP TRIGGER IF EXISTS trg_users_updated_at               ON users;

DROP FUNCTION IF EXISTS sync_client_dispute_counter();
DROP FUNCTION IF EXISTS sync_runner_errand_counters();
DROP FUNCTION IF EXISTS sync_client_errand_counters();
DROP FUNCTION IF EXISTS set_updated_at();
