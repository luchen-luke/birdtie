BEGIN;
DO $$BEGIN RAISE EXCEPTION '160 requires an explicit data-preserving rollback plan: stable location tombstones and media rebindings must not be discarded';END$$;
ROLLBACK;
