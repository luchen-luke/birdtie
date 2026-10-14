-- An independent, explicitly selected public derivative. Never imports 100.
CREATE TABLE public_moment_photos (
 moment_id uuid PRIMARY KEY REFERENCES moments(id),photo_id uuid NOT NULL UNIQUE,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 8999999999999999),
 approved_moment_revision bigint NOT NULL CHECK(approved_moment_revision BETWEEN 1 AND 8999999999999999),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 thumbnail bytea,sha256 char(64),rights jsonb NOT NULL,
 retention_until timestamptz NOT NULL,source_binding text NOT NULL,
 attribution text NOT NULL,status text NOT NULL CHECK(status IN('READY','DELETED')),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((status='READY' AND thumbnail IS NOT NULL AND octet_length(thumbnail) BETWEEN 1 AND 262144 AND sha256 ~ '^[0-9a-f]{64}$') OR(status='DELETED' AND thumbnail IS NULL AND sha256 IS NULL))
);
CREATE INDEX public_moment_photos_retention ON public_moment_photos(retention_until) WHERE thumbnail IS NOT NULL;
CREATE TABLE public_moment_photo_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,
 moment_id uuid NOT NULL,status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 action text,photo_id uuid,expected_moment_revision bigint,expected_photo_version bigint,
 applied_photo_version bigint NOT NULL,payload_sha256 bytea,committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND applied_photo_version=0 AND action IS NULL AND photo_id IS NULL AND expected_moment_revision IS NULL AND expected_photo_version IS NULL AND payload_sha256 IS NULL) OR(status='SUCCEEDED' AND action IN('SAVE','DELETE') AND photo_id IS NOT NULL AND expected_moment_revision BETWEEN 1 AND 8999999999999999 AND expected_photo_version BETWEEN 0 AND 8999999999999999 AND ((action='SAVE' AND expected_moment_revision>=2 AND expected_photo_version<8999999999999999) OR(action='DELETE' AND expected_photo_version>0)) AND applied_photo_version=least(expected_photo_version+1,8999999999999999) AND octet_length(payload_sha256)=32))
);

-- Canonical retained-field gates are inserted verbatim from the existing Go
-- reader by this source's preparation step; no applied 114/100 byte is changed.
CREATE FUNCTION birdtie_public_moment_photo_source(moment uuid,read_at timestamptz)
RETURNS TABLE(binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(src.candidate_id,src.xmin::text,src.permission_id) ORDER BY src.candidate_id),'[]'::jsonb) FROM place_sources src WHERE src.place_id=p.id AND src.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text) ORDER BY f.id),'[]'::jsonb) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates')))::text)::text,
 least(c.expires_at,least(p.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,rights.retention_until)) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL AND rights.version=f.permission_version
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND rights.retention_until>cl.at AND f.conflict_state='NONE')))
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id JOIN cities c ON c.id=m.city_id
 CROSS JOIN(SELECT read_at at) cl
 WHERE m.id=moment AND m.revision>=2 AND m.status='published' AND m.visibility='public' AND m.location_precision='place'
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
 AND a.account_type='person' AND a.status='active' AND p.publication_status='published' AND c.publication_status='published'
 AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND(c.expires_at IS NULL OR c.expires_at>cl.at) AND (
 NOT EXISTS(SELECT 1 FROM place_sources managed_source WHERE managed_source.place_id=p.id AND managed_source.permission_id IS NOT NULL)
 OR (
 EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='name' AND f.value=to_jsonb(p.name))
 AND (coalesce(p.address_label,'')='' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='address' AND f.value=to_jsonb(p.address_label)))
 AND (p.location_precision<>'point' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='coordinates' AND f.value=jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude)))
 ))
$$;
CREATE FUNCTION birdtie_public_moment_photo_current(moment uuid,viewer uuid,read_at timestamptz)
RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM public_moment_photos image JOIN moments m ON m.id=image.moment_id
 CROSS JOIN LATERAL birdtie_public_moment_photo_source(m.id,read_at) source
 WHERE image.moment_id=moment AND image.status='READY' AND image.thumbnail IS NOT NULL
 AND image.owner_account_id=m.author_account_id AND image.approved_moment_revision=m.revision
 AND image.retention_until>read_at AND image.source_binding=source.binding
 AND image.rights->>'canRetain'='true' AND image.rights->>'canPublish'='true'
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND((b.blocker_account_id=viewer AND b.blocked_account_id=m.author_account_id) OR(b.blocker_account_id=m.author_account_id AND b.blocked_account_id=viewer))))
$$;
CREATE FUNCTION birdtie_public_moment_photo_metadata(moment uuid,viewer uuid,read_at timestamptz)
RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('momentId',image.moment_id,'momentRevision',image.approved_moment_revision,'photoId',image.photo_id,'version',image.version,
 'thumbnailPath','/v1/moments/'||image.moment_id||'/public-photo/'||image.approved_moment_revision||'/'||image.photo_id||'/'||image.version||'/content',
 'width',320,'height',180,'byteCount',octet_length(image.thumbnail),'sha256',image.sha256,
 'expiresAt',least(read_at+interval '60 seconds',image.retention_until,source.deadline),'attribution',image.attribution)
 FROM public_moment_photos image CROSS JOIN LATERAL birdtie_public_moment_photo_source(image.moment_id,read_at) source
 WHERE image.moment_id=moment AND birdtie_public_moment_photo_current(moment,viewer,read_at)
$$;
CREATE FUNCTION birdtie_public_moment_photo_proof(moment uuid,viewer uuid)
RETURNS text LANGUAGE sql STABLE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(
 (SELECT jsonb_build_array(image.photo_id,image.version,image.xmin::text,image.source_binding) FROM public_moment_photos image WHERE image.moment_id=moment),
 (SELECT binding FROM birdtie_public_moment_photo_source(moment,clock_timestamp())),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb)
 FROM person_block_control_epochs ep JOIN moments m ON m.id=moment WHERE viewer IS NOT NULL AND((ep.owner_account_id=viewer AND ep.target_account_id=m.author_account_id) OR(ep.owner_account_id=m.author_account_id AND ep.target_account_id=viewer))))::text,'UTF8')),'hex')
$$;
CREATE FUNCTION birdtie_guard_public_moment_photo_operation() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'public Moment photo receipts are immutable';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':'||NEW.operation_id::text,145));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'active receipt actor required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM public_moment_photos image JOIN moments m ON m.id=image.moment_id WHERE image.moment_id=NEW.moment_id AND image.photo_id=NEW.photo_id AND image.owner_account_id=NEW.actor_account_id AND m.author_account_id=NEW.actor_account_id AND image.version=NEW.applied_photo_version AND m.revision=NEW.expected_moment_revision AND((NEW.action='DELETE' AND image.status='DELETED') OR(NEW.action='SAVE' AND birdtie_public_moment_photo_current(m.id,NEW.actor_account_id,clock_timestamp())))) THEN RAISE EXCEPTION 'matching concrete public photo effect required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER public_moment_photo_operations_immutable BEFORE INSERT OR UPDATE OR DELETE ON public_moment_photo_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_public_moment_photo_operation();
CREATE FUNCTION birdtie_retire_public_moment_photos() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.author_account_id IS DISTINCT FROM OLD.author_account_id OR NEW.revision IS DISTINCT FROM OLD.revision OR NEW.status<>'published' OR NEW.visibility<>'public' OR NEW.place_id IS DISTINCT FROM OLD.place_id OR NEW.city_id IS DISTINCT FROM OLD.city_id OR NEW.location_precision<>'place' THEN
 UPDATE public_moment_photos SET thumbnail=NULL,sha256=NULL,status='DELETED',version=least(version+1,8999999999999999),updated_at=clock_timestamp() WHERE moment_id=OLD.id AND thumbnail IS NOT NULL;
 END IF;RETURN NEW;
END $$;
CREATE TRIGGER public_moment_photos_retire AFTER UPDATE OF author_account_id,revision,status,visibility,place_id,city_id,location_precision ON moments FOR EACH ROW EXECUTE FUNCTION birdtie_retire_public_moment_photos();
CREATE FUNCTION birdtie_purge_public_moment_photo_derivatives(batch integer) RETURNS integer LANGUAGE plpgsql AS $$
DECLARE n integer;
BEGIN
 IF batch NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'bounded public derivative purge required';END IF;
 WITH targets AS MATERIALIZED(SELECT image.moment_id FROM public_moment_photos image WHERE image.thumbnail IS NOT NULL AND(image.retention_until<=clock_timestamp() OR NOT birdtie_public_moment_photo_current(image.moment_id,NULL,clock_timestamp())) ORDER BY image.moment_id LIMIT batch FOR UPDATE SKIP LOCKED),
 cleared AS(UPDATE public_moment_photos image SET thumbnail=NULL,sha256=NULL,status='DELETED',version=least(version+1,8999999999999999),updated_at=clock_timestamp() FROM targets t WHERE t.moment_id=image.moment_id RETURNING image.moment_id)
 SELECT count(*) INTO n FROM cleared;RETURN n;
END $$;
COMMENT ON TABLE public_moment_photos IS 'Explicit public normalized JPEG only. No private attachment/EXIF/model grant. Logical column clearing is not physical-page or backup erasure.';
