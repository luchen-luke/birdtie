-- Applies the no-coordinate-without-location invariant to databases initialized before
-- the guard was added to 001_foundation.sql.
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM pg_constraint
        WHERE conrelid = 'places'::regclass AND conname = 'places_hidden_coordinate'
    ) THEN
        ALTER TABLE places ADD CONSTRAINT places_hidden_coordinate
            CHECK (location_precision <> 'none' OR latitude IS NULL);
    END IF;
END
$$;
