BEGIN;
DO $$ BEGIN IF EXISTS(SELECT 1 FROM moment_comments WHERE parent_comment_id IS NOT NULL) THEN RAISE EXCEPTION 'reply facts exist; rollback would erase canonical relationships'; END IF; END $$;
DROP TRIGGER moment_comment_reply_guard ON moment_comments;
DROP FUNCTION birdtie_guard_comment_reply();
ALTER TABLE moment_comments DROP CONSTRAINT moment_comments_parent_pair, DROP CONSTRAINT moment_comments_parent_same_moment, DROP CONSTRAINT moment_comments_same_moment_identity, DROP COLUMN parent_comment_id, DROP COLUMN parent_comment_version;
COMMIT;
