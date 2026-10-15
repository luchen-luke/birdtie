-- New user-provided static image derivatives; independent of 100/125/145.
CREATE FUNCTION birdtie_public_moment_media_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'public-moment-media-v1'::text $$;
CREATE TABLE moment_media_materials (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),owner_account_id uuid NOT NULL REFERENCES accounts(id),local_scope uuid NOT NULL,
 version bigint NOT NULL DEFAULT 1 CHECK(version=1),display bytea NOT NULL CHECK(octet_length(display) BETWEEN 1 AND 1048576),
 thumbnail bytea NOT NULL CHECK(octet_length(thumbnail) BETWEEN 1 AND 262144),metadata jsonb NOT NULL,
 prepared_at timestamptz NOT NULL DEFAULT clock_timestamp(),retain_until timestamptz NOT NULL,
 CHECK(retain_until>prepared_at AND retain_until<=prepared_at+interval '10 minutes')
);
CREATE INDEX moment_media_material_owner ON moment_media_materials(owner_account_id,retain_until);
CREATE TABLE moment_media_prepare_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,receipt jsonb NOT NULL,
 PRIMARY KEY(actor_account_id,operation_id),CHECK(receipt->>'status' IN('SUCCEEDED','STOPPED'))
);
CREATE TABLE public_moment_media_sets (
 moment_id uuid PRIMARY KEY REFERENCES moments(id),owner_account_id uuid NOT NULL REFERENCES accounts(id),
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),approved_moment_revision bigint NOT NULL,
 status text NOT NULL CHECK(status IN('READY','DELETED')),source_binding char(64) NOT NULL,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE public_moment_media_attachments (
 moment_id uuid NOT NULL REFERENCES public_moment_media_sets(moment_id),set_version bigint NOT NULL,
 position integer NOT NULL CHECK(position BETWEEN 0 AND 8),media_id uuid NOT NULL,media_version bigint NOT NULL CHECK(media_version>0),
 metadata jsonb NOT NULL,rights jsonb NOT NULL,retention_until timestamptz NOT NULL,
 display bytea,thumbnail bytea,
 PRIMARY KEY(moment_id,set_version,position),UNIQUE(moment_id,set_version,media_id),
 CHECK((display IS NULL AND thumbnail IS NULL) OR(octet_length(display) BETWEEN 1 AND 1048576 AND octet_length(thumbnail) BETWEEN 1 AND 262144))
);
CREATE INDEX public_moment_media_retention ON public_moment_media_attachments(retention_until) WHERE display IS NOT NULL;
CREATE TABLE public_moment_media_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,moment_id uuid NOT NULL REFERENCES moments(id),receipt jsonb NOT NULL,
 PRIMARY KEY(actor_account_id,operation_id),CHECK(receipt->>'status' IN('SUCCEEDED','STOPPED'))
);
CREATE FUNCTION birdtie_guard_moment_media_receipt() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'original media operation receipts are immutable';END IF;
 IF NEW.receipt->>'operationId'<>NEW.operation_id::text THEN RAISE EXCEPTION 'original media operation identity required';END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current person required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER moment_media_prepare_receipts_immutable BEFORE INSERT OR UPDATE OR DELETE ON moment_media_prepare_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_media_receipt();
CREATE TRIGGER public_moment_media_receipts_immutable BEFORE INSERT OR UPDATE OR DELETE ON public_moment_media_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_media_receipt();
CREATE FUNCTION birdtie_retire_moment_media_sets() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.author_account_id IS DISTINCT FROM OLD.author_account_id OR NEW.revision IS DISTINCT FROM OLD.revision OR NEW.status<>'published' OR NEW.visibility<>'public' OR NEW.place_id IS DISTINCT FROM OLD.place_id OR NEW.city_id IS DISTINCT FROM OLD.city_id OR NEW.location_precision IS DISTINCT FROM OLD.location_precision THEN
 UPDATE public_moment_media_sets SET status='DELETED',version=least(version+1,8999999999999999),updated_at=clock_timestamp() WHERE moment_id=OLD.id AND status='READY';
 UPDATE public_moment_media_attachments SET display=NULL,thumbnail=NULL WHERE moment_id=OLD.id AND display IS NOT NULL;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER public_moment_media_retire AFTER UPDATE OF author_account_id,revision,status,visibility,place_id,city_id,location_precision ON moments FOR EACH ROW EXECUTE FUNCTION birdtie_retire_moment_media_sets();
-- A later explicitly approved legacy single photo also supersedes a set. Both
-- writers retain their original immutable operation receipts.
CREATE FUNCTION birdtie_legacy_photo_retires_media_set() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.status='READY' THEN
 UPDATE public_moment_media_sets SET status='DELETED',version=least(version+1,8999999999999999),updated_at=clock_timestamp() WHERE moment_id=NEW.moment_id AND status='READY';
 UPDATE public_moment_media_attachments SET display=NULL,thumbnail=NULL WHERE moment_id=NEW.moment_id AND display IS NOT NULL;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER legacy_photo_retires_media_set AFTER INSERT OR UPDATE OF status,thumbnail,version ON public_moment_photos FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_photo_retires_media_set();
CREATE FUNCTION birdtie_purge_moment_media_materials(batch integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;BEGIN
 IF batch NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'bounded media purge required';END IF;
 WITH picked AS MATERIALIZED(SELECT id FROM moment_media_materials WHERE retain_until<=clock_timestamp() ORDER BY id LIMIT batch FOR UPDATE SKIP LOCKED),removed AS(DELETE FROM moment_media_materials WHERE id IN(SELECT id FROM picked) RETURNING id) SELECT count(*) INTO n FROM removed;RETURN n;
END $$;
COMMENT ON TABLE moment_media_materials IS 'Owner-only normalized static JPEG. Structural validation only; antivirus NOT_RUN, privacy UNKNOWN. No raw upload or EXIF. Not a Moment/publication authorization.';
