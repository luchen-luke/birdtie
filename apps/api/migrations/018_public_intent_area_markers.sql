-- Opt-in broad display zone for a public Intent. No personal coordinates are stored.
ALTER TABLE intents ADD COLUMN IF NOT EXISTS public_map_zone text;
DO $$ BEGIN
    ALTER TABLE intents ADD CONSTRAINT intents_public_map_zone_valid
        CHECK (public_map_zone IS NULL OR public_map_zone IN
            ('city_centre', 'north', 'south', 'east', 'west'));
EXCEPTION WHEN duplicate_object THEN NULL; END $$;
