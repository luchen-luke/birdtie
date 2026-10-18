BEGIN;
ALTER TABLE moment_comments ADD COLUMN parent_comment_id uuid, ADD COLUMN parent_comment_version bigint;
ALTER TABLE moment_comments ADD CONSTRAINT moment_comments_same_moment_identity UNIQUE(moment_id,id);
ALTER TABLE moment_comments ADD CONSTRAINT moment_comments_parent_same_moment FOREIGN KEY(moment_id,parent_comment_id) REFERENCES moment_comments(moment_id,id);
ALTER TABLE moment_comments ADD CONSTRAINT moment_comments_parent_pair CHECK((parent_comment_id IS NULL AND parent_comment_version IS NULL) OR(parent_comment_id IS NOT NULL AND parent_comment_version IS NOT NULL AND parent_comment_id<>id AND parent_comment_version BETWEEN 1 AND 8999999999999999));
CREATE FUNCTION birdtie_guard_comment_reply() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF TG_OP='UPDATE' AND (NEW.moment_id IS DISTINCT FROM OLD.moment_id OR NEW.parent_comment_id IS DISTINCT FROM OLD.parent_comment_id OR NEW.parent_comment_version IS DISTINCT FROM OLD.parent_comment_version) THEN RAISE EXCEPTION 'comment reply identity immutable' USING ERRCODE='23514'; END IF;
 IF TG_OP='INSERT' AND NEW.parent_comment_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM moment_comments p WHERE p.id=NEW.parent_comment_id AND p.moment_id=NEW.moment_id AND NOT p.deleted AND p.version=NEW.parent_comment_version) THEN RAISE EXCEPTION 'current same moment parent required' USING ERRCODE='23514'; END IF;
 RETURN NEW; END $$;
CREATE TRIGGER moment_comment_reply_guard BEFORE INSERT OR UPDATE ON moment_comments FOR EACH ROW EXECUTE FUNCTION birdtie_guard_comment_reply();
COMMIT;
