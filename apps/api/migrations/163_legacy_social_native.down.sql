-- Preserve immutable source facts and private history; downgrade requires an explicit reviewed export.
DO $$ BEGIN RAISE EXCEPTION '163 social history cannot be destructively downgraded'; END $$;
