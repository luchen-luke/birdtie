BEGIN;
-- Independent retained image declarations. Old six-field Place rights do not
-- authorize photos, and Organization/Agent roles do not grant city editorial.
CREATE TABLE place_photo_candidates (
 id uuid PRIMARY KEY,place_id uuid NOT NULL REFERENCES places(id),city_id text NOT NULL REFERENCES cities(id),
 submitted_by uuid NOT NULL REFERENCES accounts(id),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740991),
 status text NOT NULL CHECK(status IN('PENDING','APPROVED','REJECTED','WITHDRAWN','EXPIRED')),
 thumbnail bytea,sha256 text NOT NULL CHECK(sha256 ~ '^[0-9a-f]{64}$'),rights jsonb NOT NULL,
 rights_sha256 text NOT NULL CHECK(rights_sha256 ~ '^[0-9a-f]{64}$'),retention_until timestamptz NOT NULL CHECK(isfinite(retention_until)),
 reviewed_by uuid REFERENCES accounts(id),updated_by uuid REFERENCES accounts(id),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(thumbnail IS NULL OR octet_length(thumbnail) BETWEEN 1 AND 262144),
 CHECK(thumbnail IS NULL OR encode(sha256(thumbnail),'hex')=sha256),
 CHECK(jsonb_typeof(rights)='object' AND rights->>'basis' IN('OWNED','LICENSED') AND rights->>'canRetain'='true' AND rights->>'canPublish'='true'),
 CHECK(reviewed_by IS NULL OR reviewed_by<>submitted_by),
 CHECK(status IN('PENDING','APPROVED') OR thumbnail IS NULL)
);
CREATE INDEX place_photo_editorial ON place_photo_candidates(place_id,created_at DESC,id);
CREATE TABLE place_photo_publications (
 place_id uuid PRIMARY KEY REFERENCES places(id),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 candidate_id uuid REFERENCES place_photo_candidates(id),updated_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE place_photo_operations (
 actor_account_id uuid NOT NULL REFERENCES accounts(id),operation_id uuid NOT NULL,place_id uuid NOT NULL REFERENCES places(id),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740991),action text NOT NULL,
 candidate_id uuid,candidate_version bigint NOT NULL,applied_version bigint NOT NULL,status text NOT NULL,
 input_sha256 text NOT NULL CHECK(input_sha256 ~ '^[0-9a-f]{64}$'),created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(actor_account_id,operation_id),
 CHECK((status='STOPPED' AND action='STOP' AND candidate_id IS NULL AND candidate_version=0 AND applied_version=0) OR
 (status='SUCCEEDED' AND action IN('SUBMIT','APPROVE','REJECT','WITHDRAW') AND candidate_id IS NOT NULL AND candidate_version BETWEEN 1 AND 9007199254740991 AND applied_version BETWEEN 0 AND 9007199254740991 AND(action<>'APPROVE' OR applied_version=expected_version+1)))
);
CREATE FUNCTION birdtie_place_photo_editor(i uuid,v uuid,reviewer boolean DEFAULT false) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM places p JOIN cities c ON c.id=p.city_id JOIN city_editor_memberships m ON m.city_id=c.id
 JOIN accounts a ON a.id=m.account_id WHERE p.id=i AND a.id=v AND a.account_type='person' AND a.status='active' AND m.state='active'
 AND m.created_at<=clock_timestamp() AND(NOT reviewer OR m.role='reviewer') AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()))
$$;
CREATE FUNCTION birdtie_place_photo_visible(i uuid,at timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT EXISTS(SELECT 1 FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=i AND p.publication_status='published'
 AND c.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>at) AND(c.expires_at IS NULL OR c.expires_at>at)
 AND(NOT EXISTS(SELECT 1 FROM place_sources s WHERE s.place_id=p.id AND s.permission_id IS NOT NULL) OR
 (EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions r ON r.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND r.source_url=p.source_ref AND origin.permission_id=r.id AND origin.status IN('published','linked_duplicate') AND r.state='CONFIRMED' AND r.revoked_at IS NULL
 AND r.version=f.permission_version AND r.reviewed_by IS NOT NULL AND r.reviewed_by<>r.declared_by AND r.reviewed_at<=at AND r.retention_until>at AND f.valid_from<=at AND f.valid_to>at
 AND f.refresh_due>at AND f.conflict_state='NONE' AND f.observed_at<=at AND f.verified_at<=at AND f.field_key='name' AND f.value=to_jsonb(p.name))
 AND(coalesce(p.address_label,'')='' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions r ON r.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND r.source_url=p.source_ref AND origin.permission_id=r.id AND origin.status IN('published','linked_duplicate') AND r.state='CONFIRMED' AND r.revoked_at IS NULL AND r.version=f.permission_version
 AND r.reviewed_by IS NOT NULL AND r.reviewed_by<>r.declared_by AND r.reviewed_at<=at AND r.retention_until>at AND f.valid_from<=at AND f.valid_to>at AND f.refresh_due>at AND f.conflict_state='NONE'
 AND f.observed_at<=at AND f.verified_at<=at AND f.field_key='address' AND f.value=to_jsonb(p.address_label)))
 AND(p.location_precision<>'point' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions r ON r.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND r.source_url=p.source_ref AND origin.permission_id=r.id AND origin.status IN('published','linked_duplicate') AND r.state='CONFIRMED' AND r.revoked_at IS NULL AND r.version=f.permission_version
 AND r.reviewed_by IS NOT NULL AND r.reviewed_by<>r.declared_by AND r.reviewed_at<=at AND r.retention_until>at AND f.valid_from<=at AND f.valid_to>at AND f.refresh_due>at AND f.conflict_state='NONE'
 AND f.observed_at<=at AND f.verified_at<=at AND f.field_key='coordinates' AND f.value=jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude))))))
$$;
-- Policy fingerprint deliberately excludes changing observation clocks and
-- candidate/publication rows written by this operation. Those have own CAS.
CREATE FUNCTION birdtie_place_photo_authority(i uuid) RETURNS text LANGUAGE sql STABLE AS $$
 WITH p AS(SELECT *,xmin::text token FROM places WHERE id=i),city AS(SELECT c.*,c.xmin::text token FROM cities c WHERE c.id IN(SELECT city_id FROM p)),
 tokens AS(
 SELECT 'place' t,id::text id,token FROM p UNION ALL SELECT 'city',id,token FROM city
 UNION ALL SELECT 'editor',m.account_id::text,m.xmin::text FROM city_editor_memberships m WHERE m.city_id IN(SELECT id FROM city)
 UNION ALL SELECT 'account',a.id::text,a.xmin::text FROM accounts a WHERE a.id IN(SELECT m.account_id FROM city_editor_memberships m WHERE m.city_id IN(SELECT id FROM city))
 UNION ALL SELECT 'source',s.candidate_id::text,s.xmin::text FROM place_sources s WHERE s.place_id=i
 UNION ALL SELECT 'field',f.id::text,f.xmin::text FROM place_field_evidence f WHERE f.place_id=i
 UNION ALL SELECT 'rights',r.id::text,r.xmin::text FROM place_source_permissions r WHERE r.id IN(SELECT permission_id FROM place_sources WHERE place_id=i)
 UNION ALL SELECT 'origin',c.id::text,c.xmin::text FROM place_candidates c WHERE c.id IN(SELECT candidate_id FROM place_sources WHERE place_id=i))
 SELECT encode(sha256(convert_to(coalesce(jsonb_agg(jsonb_build_array(t,id,token)ORDER BY t,id),'[]'::jsonb)::text,'UTF8')),'hex') FROM tokens
$$;
CREATE FUNCTION birdtie_place_photo_proof(i uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT birdtie_place_photo_authority(i)||':'||coalesce((SELECT xmin::text FROM place_photo_publications WHERE place_id=i),'none')||':'||
 coalesce((SELECT c.xmin::text FROM place_photo_candidates c JOIN place_photo_publications p ON p.candidate_id=c.id WHERE p.place_id=i),'none')
$$;
CREATE FUNCTION birdtie_place_photo_metadata(i uuid,at timestamptz DEFAULT clock_timestamp()) RETURNS jsonb LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_object('entityType','place','entityId',i,'version',pub.version,'thumbnailPath','/v1/places/'||i::text||'/photos/'||pub.version::text||'/content',
 'width',320,'height',180,'byteCount',octet_length(photo.thumbnail),'sha256',photo.sha256,'attribution',photo.rights->>'attribution',
 'expiresAt',least(at+interval '60 seconds',photo.retention_until,p.expires_at,city.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,r.retention_until)) FROM place_field_evidence f JOIN place_source_permissions r ON r.id=f.permission_id WHERE f.place_id=i AND r.source_url=p.source_ref
 AND f.field_key IN('name','address','coordinates') AND r.state='CONFIRMED' AND r.revoked_at IS NULL AND r.version=f.permission_version AND f.valid_from<=at AND f.valid_to>at AND f.refresh_due>at AND r.retention_until>at AND f.conflict_state='NONE')))
 FROM place_photo_publications pub JOIN place_photo_candidates photo ON photo.id=pub.candidate_id AND photo.place_id=pub.place_id
 JOIN places p ON p.id=pub.place_id JOIN cities city ON city.id=p.city_id WHERE pub.place_id=i AND photo.status='APPROVED' AND photo.thumbnail IS NOT NULL AND photo.retention_until>at
 AND photo.city_id=p.city_id AND birdtie_place_photo_visible(i,at) AND birdtie_place_photo_editor(i,photo.submitted_by,false) AND birdtie_place_photo_editor(i,photo.reviewed_by,true)
 AND photo.reviewed_by<>photo.submitted_by
$$;
CREATE FUNCTION birdtie_place_photo_candidate_guard() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'photo history is retained without pixels' USING ERRCODE='23514';END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.status<>'PENDING' OR NEW.version<>1 OR NEW.updated_by<>NEW.submitted_by OR NOT birdtie_place_photo_editor(NEW.place_id,NEW.submitted_by,false)
   OR NOT birdtie_place_photo_visible(NEW.place_id,clock_timestamp()) OR NEW.retention_until<=clock_timestamp() OR NEW.thumbnail IS NULL
   OR NEW.city_id IS DISTINCT FROM(SELECT city_id FROM places WHERE id=NEW.place_id) THEN RAISE EXCEPTION 'current place contributor retained photo required' USING ERRCODE='23514';END IF;
 ELSE
  IF (NEW.id,NEW.place_id,NEW.city_id,NEW.submitted_by,NEW.expected_version,NEW.sha256,NEW.rights_sha256,NEW.rights,NEW.retention_until,NEW.created_at) IS DISTINCT FROM
     (OLD.id,OLD.place_id,OLD.city_id,OLD.submitted_by,OLD.expected_version,OLD.sha256,OLD.rights_sha256,OLD.rights,OLD.retention_until,OLD.created_at)
   OR NEW.version<>least(OLD.version+1,9007199254740991) OR(NEW.thumbnail IS NOT NULL AND NEW.thumbnail IS DISTINCT FROM OLD.thumbnail) THEN RAISE EXCEPTION 'photo declaration immutable; versioned retirement only' USING ERRCODE='23514';END IF;
  IF NEW.status='EXPIRED' THEN
   IF NEW.retention_until>clock_timestamp() OR NEW.thumbnail IS NOT NULL THEN RAISE EXCEPTION 'expiry cannot retain pixels' USING ERRCODE='23514';END IF;
  ELSIF NEW.status='WITHDRAWN' THEN
   IF NEW.thumbnail IS NOT NULL OR NOT EXISTS(SELECT 1 FROM accounts a WHERE a.id=NEW.updated_by AND a.account_type='person' AND a.status='active') OR
    (NEW.updated_by<>NEW.submitted_by AND NOT birdtie_place_photo_editor(NEW.place_id,NEW.updated_by,true)) THEN RAISE EXCEPTION 'own or current reviewer withdrawal required' USING ERRCODE='23514';END IF;
  ELSIF NEW.status IN('APPROVED','REJECTED') THEN
   IF OLD.status<>'PENDING' OR NEW.reviewed_by=NEW.submitted_by OR NEW.reviewed_by IS DISTINCT FROM NEW.updated_by OR NOT birdtie_place_photo_editor(NEW.place_id,NEW.reviewed_by,true)
    OR(NEW.status='APPROVED' AND(NEW.thumbnail IS NULL OR NEW.retention_until<=clock_timestamp() OR NOT birdtie_place_photo_editor(NEW.place_id,NEW.submitted_by,false) OR NOT birdtie_place_photo_visible(NEW.place_id,clock_timestamp()))) THEN RAISE EXCEPTION 'different current reviewer required' USING ERRCODE='23514';END IF;
  ELSE RAISE EXCEPTION 'terminal photo cannot revive' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NEW;
 END $$;
CREATE TRIGGER place_photo_candidate_guard BEFORE INSERT OR UPDATE OR DELETE ON place_photo_candidates FOR EACH ROW EXECUTE FUNCTION birdtie_place_photo_candidate_guard();
CREATE FUNCTION birdtie_place_photo_publication_guard() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'publication version cannot disappear' USING ERRCODE='23514';END IF;
 IF TG_OP='UPDATE' AND(NEW.place_id<>OLD.place_id OR NEW.version<>least(OLD.version+1,9007199254740991)) THEN RAISE EXCEPTION 'photo publication CAS required' USING ERRCODE='23514';END IF;
 IF NEW.candidate_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM place_photo_candidates c WHERE c.id=NEW.candidate_id AND c.place_id=NEW.place_id AND c.status='APPROVED' AND c.thumbnail IS NOT NULL AND c.retention_until>clock_timestamp()
  AND birdtie_place_photo_editor(c.place_id,c.submitted_by,false) AND birdtie_place_photo_editor(c.place_id,c.reviewed_by,true)) THEN RAISE EXCEPTION 'publication needs reviewed current photo' USING ERRCODE='23514';END IF;
 RETURN NEW;
 END $$;
CREATE TRIGGER place_photo_publication_guard BEFORE INSERT OR UPDATE OR DELETE ON place_photo_publications FOR EACH ROW EXECUTE FUNCTION birdtie_place_photo_publication_guard();
CREATE FUNCTION birdtie_place_photo_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
 BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'photo original operation immutable' USING ERRCODE='23514';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM place_photo_candidates c WHERE c.id=NEW.candidate_id AND c.place_id=NEW.place_id AND c.version=NEW.candidate_version) THEN RAISE EXCEPTION 'photo receipt must match actual candidate effect' USING ERRCODE='23514';END IF;
 IF NEW.action='APPROVE' AND NOT EXISTS(SELECT 1 FROM place_photo_publications p WHERE p.place_id=NEW.place_id AND p.candidate_id=NEW.candidate_id AND p.version=NEW.applied_version) THEN RAISE EXCEPTION 'photo approval receipt needs publication' USING ERRCODE='23514';END IF;
 RETURN NEW;
 END $$;
CREATE TRIGGER place_photo_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON place_photo_operations FOR EACH ROW EXECUTE FUNCTION birdtie_place_photo_operation_guard();
-- Explicit maintenance only; no scheduler/model/Agent activation. Bytes are
-- physically NULL while minimal metadata and original receipts remain.
CREATE FUNCTION birdtie_purge_human_media_derivatives(batch integer) RETURNS TABLE(place_photos integer,entity_covers integer) LANGUAGE plpgsql AS $$
 DECLARE ids uuid[];cover_keys text[];n integer;m integer;
 BEGIN
 IF batch NOT BETWEEN 1 AND 100 THEN RAISE EXCEPTION 'bounded batch required' USING ERRCODE='23514';END IF;
 SELECT array_agg(id) INTO ids FROM(SELECT id FROM place_photo_candidates WHERE thumbnail IS NOT NULL AND retention_until<=clock_timestamp() ORDER BY retention_until,id FOR UPDATE SKIP LOCKED LIMIT batch) q;
 UPDATE place_photo_candidates SET thumbnail=NULL,status='EXPIRED',updated_by=NULL,version=least(version+1,9007199254740991),updated_at=clock_timestamp() WHERE id=ANY(coalesce(ids,'{}'::uuid[]));GET DIAGNOSTICS n=ROW_COUNT;
 UPDATE place_photo_publications SET candidate_id=NULL,version=least(version+1,9007199254740991),updated_at=clock_timestamp() WHERE candidate_id=ANY(coalesce(ids,'{}'::uuid[]));
 SELECT array_agg(entity_type||':'||entity_id::text) INTO cover_keys FROM(SELECT entity_type,entity_id FROM entity_covers WHERE thumbnail IS NOT NULL AND retention_until<=clock_timestamp() ORDER BY retention_until,entity_type,entity_id FOR UPDATE SKIP LOCKED LIMIT batch) q;
 UPDATE entity_covers SET thumbnail=NULL,sha256=NULL,rights=NULL,retention_until=NULL,version=least(version+1,9007199254740991),updated_at=clock_timestamp() WHERE entity_type||':'||entity_id::text=ANY(coalesce(cover_keys,'{}'::text[]));GET DIAGNOSTICS m=ROW_COUNT;
 RETURN QUERY SELECT n,m;
 END $$;
COMMIT;
