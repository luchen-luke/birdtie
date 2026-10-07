BEGIN;
CREATE TABLE IF NOT EXISTS activity_analytics_events (
    id bigserial PRIMARY KEY,
    activity_id uuid NOT NULL REFERENCES activities(id) ON DELETE CASCADE,
    event_type text NOT NULL CHECK (event_type IN ('impression','detail','rsvp','save')),
    entry_source text NOT NULL CHECK (entry_source IN ('now','agent','map','organization','inbox','direct','unknown')),
    event_key text UNIQUE,
    occurred_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS activity_analytics_by_activity
    ON activity_analytics_events (activity_id, occurred_at DESC);
COMMIT;
