-- Refuse to lose the cleanup ledger while any provider original may remain.
DO $$ BEGIN
  IF to_regclass('public.private_video_materials') IS NOT NULL THEN
    IF EXISTS (SELECT 1 FROM private_video_materials WHERE status<>'deleted' OR cleanup_pending) THEN
      RAISE EXCEPTION 'private videos must finish deletion cleanup before rollback';
    END IF;
  END IF;
END $$;
-- Completed deleted media_assets remain as generic audit tombstones.
DROP TABLE IF EXISTS private_video_materials;
