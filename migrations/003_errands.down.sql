ALTER TABLE IF EXISTS errands DROP CONSTRAINT IF EXISTS fk_errands_accepted_offer;
DROP TABLE IF EXISTS errand_events;
DROP TABLE IF EXISTS errand_offers;
DROP TABLE IF EXISTS errand_stops;
DROP TABLE IF EXISTS errands;
DROP TYPE  IF EXISTS stop_type;
DROP TYPE  IF EXISTS offer_status;
DROP TYPE  IF EXISTS errand_status;
