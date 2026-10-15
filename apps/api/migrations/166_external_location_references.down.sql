BEGIN;
DO $$BEGIN RAISE EXCEPTION 'Preserve stable external references; use a forward repair';END$$;
COMMIT;
