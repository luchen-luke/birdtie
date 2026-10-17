BEGIN;
-- Restores catalog visibility only; all source and business rows were retained.
DROP TABLE IF EXISTS destination_catalog_exclusions;
COMMIT;
