BEGIN;
-- Untrusted client telemetry, not an effect/permission or provider receipt ledger.
-- No person, session, URL, coordinates, query or approval payload is retained.
CREATE TABLE booking_external_events (
 event_id uuid PRIMARY KEY,
 place_id uuid NOT NULL REFERENCES places(id),
 event_type text NOT NULL CHECK(event_type='EXTERNAL_BOOKING_CLICK'),
 outcome text NOT NULL CHECK(outcome='CLIENT_REPORTED_EXTERNAL_OPEN'),
 recorded_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX booking_external_events_place_time ON booking_external_events(place_id,recorded_at);
COMMIT;
