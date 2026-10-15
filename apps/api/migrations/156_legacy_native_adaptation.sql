BEGIN;
-- Content may exist independently of any geographic context. Missing titles
-- stay empty; a current public write still needs real text or approved media.
ALTER TABLE moments ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE moments DROP CONSTRAINT moments_title_check;
ALTER TABLE moments ADD CONSTRAINT moments_title_check CHECK(length(title)<=160);
ALTER TABLE moments ADD CONSTRAINT moment_optional_geography CHECK(
 (place_id IS NULL AND (location_precision='none' OR(city_id IS NOT NULL AND location_precision='city')))
 OR(place_id IS NOT NULL AND(location_precision='place' OR(visibility='private' AND location_precision='none'))));
ALTER TABLE moments ADD CONSTRAINT moment_optional_place_fk FOREIGN KEY(place_id) REFERENCES places(id);

CREATE OR REPLACE FUNCTION birdtie_public_moment_photo_source(moment uuid,read_at timestamptz)
RETURNS TABLE(binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(src.candidate_id,src.xmin::text,src.permission_id) ORDER BY src.candidate_id),'[]'::jsonb) FROM place_sources src WHERE src.place_id=p.id AND src.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text) ORDER BY f.id),'[]'::jsonb) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates')))::text)::text,
 least(c.expires_at,least(p.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,rights.retention_until)) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL AND rights.version=f.permission_version
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND rights.retention_until>cl.at AND f.conflict_state='NONE')))
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN places p ON p.id=m.place_id AND(m.city_id IS NULL OR p.city_id=m.city_id) JOIN cities c ON c.id=p.city_id
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

-- The raw parent fence matches momentMediaParentTx, without invoking the
-- public read binding (which itself checks content).
CREATE FUNCTION birdtie_moment_media_parent_raw(mid uuid) RETURNS text LANGUAGE sql STABLE AS $$
 SELECT (jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(src.candidate_id,src.xmin::text,src.permission_id) ORDER BY src.candidate_id),'[]'::jsonb) FROM place_sources src WHERE src.place_id=p.id AND src.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text) ORDER BY f.id),'[]'::jsonb) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates')))::text)
 || CASE WHEN m.city_id IS NULL AND p.id IS NOT NULL THEN jsonb_build_array('place-city-v1',pc.xmin::text) ELSE '[]'::jsonb END)::text
 FROM moments m JOIN accounts a ON a.id=m.author_account_id LEFT JOIN cities c ON c.id=m.city_id
 LEFT JOIN places p ON p.id=m.place_id AND(m.city_id IS NULL OR p.city_id=m.city_id) LEFT JOIN cities pc ON pc.id=p.city_id WHERE m.id=mid
$$;
CREATE FUNCTION birdtie_moment_media_rights_current(rights jsonb,read_at timestamptz) RETURNS boolean LANGUAGE plpgsql STABLE AS $$
BEGIN
 RETURN rights->'canRetain'='true'::jsonb AND rights->'canPublish'='true'::jsonb
  AND rights->>'basis' IN('OWNED','LICENSED')
  AND (rights->>'retentionUntil')::timestamptz>read_at
  AND (rights->>'basis'='OWNED' OR length(btrim(rights->>'licenseReference'))>0);
EXCEPTION WHEN data_exception THEN RETURN false;
END $$;
CREATE FUNCTION birdtie_moment_has_publishable_content(mid uuid,read_at timestamptz)
RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM moments m WHERE m.id=mid AND (
 length(regexp_replace(m.body,'[[:space:]]','','g'))>0
 OR EXISTS(SELECT 1 FROM public_moment_media_sets ms
 JOIN public_moment_media_attachments a ON a.moment_id=ms.moment_id AND a.set_version=ms.version
 WHERE ms.moment_id=m.id AND ms.owner_account_id=m.author_account_id
 AND ms.status='READY' AND ms.approved_moment_revision=m.revision
 AND ms.source_binding=encode(sha256(convert_to(to_json(birdtie_moment_media_parent_raw(m.id))::text,'UTF8')),'hex')
 AND birdtie_moment_media_rights_current(a.rights,read_at)
 AND a.metadata->>'displaySha256'=encode(sha256(a.display),'hex')
 AND a.metadata->>'thumbnailSha256'=encode(sha256(a.thumbnail),'hex')
 AND a.metadata->>'mediaId'=a.media_id::text AND a.metadata->>'mediaVersion'=a.media_version::text
 AND a.metadata->>'validationStatus'='VALIDATED'
 AND a.display IS NOT NULL AND a.thumbnail IS NOT NULL AND a.retention_until>read_at
 AND (COALESCE(NULLIF(a.metadata->>'kind',''),'IMAGE')='IMAGE' OR EXISTS(
 SELECT 1 FROM public_moment_media_video_payloads v WHERE v.moment_id=a.moment_id
 AND v.set_version=a.set_version AND v.position=a.position AND v.content IS NOT NULL
 AND a.metadata->>'kind'='VIDEO' AND a.metadata->'video'->>'videoSha256'=encode(sha256(v.content),'hex'))))
 OR EXISTS(SELECT 1 FROM public_moment_photos p WHERE p.moment_id=m.id
 AND p.owner_account_id=m.author_account_id AND p.approved_moment_revision=m.revision
 AND p.status='READY' AND p.thumbnail IS NOT NULL AND p.retention_until>read_at
 AND birdtie_public_moment_photo_current(m.id,m.author_account_id,read_at))))
$$;
CREATE FUNCTION birdtie_check_moment_publishable_content() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE mid uuid;
BEGIN
 IF TG_TABLE_NAME='moments' THEN mid:=NEW.id;
 ELSIF TG_OP='DELETE' THEN mid:=OLD.moment_id; ELSE mid:=NEW.moment_id; END IF;
 IF EXISTS(SELECT 1 FROM moments m WHERE m.id=mid AND m.status='published' AND m.visibility='public')
 AND NOT birdtie_moment_has_publishable_content(mid,clock_timestamp()) THEN
  RAISE EXCEPTION 'public Moment requires text or current approved media';
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER moment_publishable_content AFTER INSERT OR UPDATE ON moments
DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_publishable_content();
CREATE FUNCTION birdtie_optional_moment_content_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$ SELECT 'moment-optional-content-v1'::text $$;
CREATE OR REPLACE FUNCTION birdtie_guard_human_moment_public_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Moment operation';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT:'||NEW.operation_id::text,144));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.moment_id AND m.author_account_id=NEW.actor_account_id AND m.revision=NEW.applied_revision AND((NEW.action IN('CREATE','UPDATE') AND m.status='draft' AND m.visibility='private') OR(NEW.action='WITHDRAW' AND m.status='withdrawn' AND m.visibility='private') OR(NEW.action IN('PUBLISH','UPDATE_PUBLIC') AND m.status='published' AND m.visibility='public' AND ((m.place_id IS NOT NULL AND m.location_precision='place') OR(m.place_id IS NULL AND m.location_precision IN('none','city'))) AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at))) THEN RAISE EXCEPTION 'canonical owned Moment effect required';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_moment_public_binding(mid uuid,viewer uuid,read_at timestamptz) RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(m.xmin::text,a.xmin::text,c.xmin::text,source.binding,
 (SELECT account.xmin::text FROM accounts account WHERE account.id=viewer),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb) FROM person_block_control_epochs ep WHERE viewer IS NOT NULL AND((ep.owner_account_id=viewer AND ep.target_account_id=a.id) OR(ep.owner_account_id=a.id AND ep.target_account_id=viewer))))::text,'UTF8')),'hex')
 FROM moments m JOIN accounts a ON a.id=m.author_account_id LEFT JOIN cities c ON c.id=m.city_id
 LEFT JOIN LATERAL birdtie_public_moment_photo_source(m.id,read_at) source ON true
 WHERE m.id=mid AND m.status='published' AND m.visibility='public' AND m.revision>=2
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=read_at
 AND a.account_type='person' AND a.status='active' AND(m.city_id IS NULL OR(c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>read_at)))
 AND((m.place_id IS NULL AND m.location_precision IN('none','city')) OR(m.place_id IS NOT NULL AND m.location_precision='place' AND source.binding IS NOT NULL))
 AND birdtie_moment_has_publishable_content(m.id,read_at)
 AND(viewer IS NULL OR EXISTS(SELECT 1 FROM accounts account WHERE account.id=viewer AND account.account_type='person' AND account.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND((b.blocker_account_id=viewer AND b.blocked_account_id=a.id) OR(b.blocker_account_id=a.id AND b.blocked_account_id=viewer)))
$$;
-- A new native conversion phase; the original 146/149 preservation is immutable.
-- City names are public geographic context. Places remain drafts and personal
-- objects remain private drafts; neither source-public flags nor media consent
-- are converted into current publication authority.
CREATE TABLE legacy_native_adaptation_runs (
 run_id uuid PRIMARY KEY REFERENCES legacy_import_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-civu-native-adaptation-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256 ~ '^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 deferred jsonb NOT NULL CHECK(jsonb_typeof(deferred)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_native_adaptation_entities (
 run_id uuid NOT NULL REFERENCES legacy_native_adaptation_runs(run_id),
 kind text NOT NULL CHECK(kind IN('city','place','moment','journey','theme_map','saved_place')),
 source_key text NOT NULL CHECK(length(source_key) BETWEEN 1 AND 100),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 target_id text NOT NULL CHECK(length(target_id) BETWEEN 1 AND 80),
 owner_account_id uuid REFERENCES accounts(id),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(kind,source_key),
 UNIQUE(kind,target_id),
 CHECK((kind IN('city','place'))=(owner_account_id IS NULL))
);
CREATE INDEX legacy_native_adaptation_owner ON legacy_native_adaptation_entities(owner_account_id,kind);
CREATE TABLE legacy_native_catalog_aliases (
 run_id uuid NOT NULL REFERENCES legacy_native_adaptation_runs(run_id),
 kind text NOT NULL CHECK(kind IN('city','merged_place')),
 source_id uuid NOT NULL,
 target_city_id text REFERENCES cities(id),
 canonical_source_place_id uuid,
 PRIMARY KEY(kind,source_id),
 CHECK((kind='city' AND target_city_id IS NOT NULL AND canonical_source_place_id IS NULL)
  OR(kind='merged_place' AND target_city_id IS NULL AND canonical_source_place_id IS NOT NULL AND canonical_source_place_id<>source_id))
);
CREATE TABLE legacy_native_adaptation_residuals (
 run_id uuid NOT NULL REFERENCES legacy_native_adaptation_runs(run_id),
 kind text NOT NULL CHECK(kind IN('note','journey','theme_map','saved_place')),
 source_key text NOT NULL,
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 target_id text,
 reasons text[] NOT NULL CHECK(cardinality(reasons)>0),
 PRIMARY KEY(run_id,kind,source_key)
);
CREATE FUNCTION birdtie_legacy_native_target_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE valid boolean;
BEGIN
 IF NEW.owner_account_id IS NOT NULL AND NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id
  AND account_type='person' AND status IN('suspended','active')) THEN
  RAISE EXCEPTION 'legacy native owner no longer available';
 END IF;
 CASE NEW.kind
 WHEN 'city' THEN SELECT EXISTS(SELECT 1 FROM cities WHERE id=NEW.target_id AND publication_status='published'
  AND (content_status='building' OR(id='aberdeen-gb' AND NEW.source_key='34611cd6-62bf-41fb-808f-7010b7b7fef6'
   AND name='Aberdeen' AND country_code='GB' AND time_zone='Europe/London'))) INTO valid;
 WHEN 'place' THEN SELECT EXISTS(SELECT 1 FROM places WHERE id=NEW.target_id::uuid AND publication_status='draft') INTO valid;
 WHEN 'moment' THEN
  SELECT EXISTS(SELECT 1 FROM moments m JOIN legacy_note_archives n ON n.source_note_id::text=NEW.source_key
   WHERE m.id=NEW.target_id::uuid AND m.author_account_id=NEW.owner_account_id AND m.visibility='private' AND m.status='draft'
    AND n.run_id=NEW.run_id AND n.owner_account_id=m.author_account_id AND NOT n.deleted AND n.source_sha256=NEW.source_sha256) INTO valid;
 WHEN 'journey' THEN
  SELECT EXISTS(SELECT 1 FROM journeys j JOIN legacy_personal_archives a ON a.kind='journey' AND a.source_key=NEW.source_key
   WHERE j.id=NEW.target_id::uuid AND j.owner_account_id=NEW.owner_account_id AND j.visibility='private' AND j.status='draft'
    AND a.run_id=NEW.run_id AND a.owner_account_id=j.owner_account_id AND NOT a.deleted AND a.source_sha256=NEW.source_sha256) INTO valid;
 WHEN 'theme_map' THEN
  SELECT EXISTS(SELECT 1 FROM theme_maps j JOIN legacy_personal_archives a ON a.kind='theme_map' AND a.source_key=NEW.source_key
   WHERE j.id=NEW.target_id::uuid AND j.owner_account_id=NEW.owner_account_id AND j.visibility='private' AND j.status='draft'
    AND a.run_id=NEW.run_id AND a.owner_account_id=j.owner_account_id AND NOT a.deleted AND a.source_sha256=NEW.source_sha256) INTO valid;
 WHEN 'saved_place' THEN
  SELECT EXISTS(SELECT 1 FROM saved_items s JOIN legacy_personal_archives a ON a.kind='saved_place' AND a.source_key=NEW.source_key
   WHERE s.id=NEW.target_id::uuid AND s.owner_account_id=NEW.owner_account_id AND s.place_id IS NOT NULL
    AND a.run_id=NEW.run_id AND a.owner_account_id=s.owner_account_id AND NOT a.deleted AND a.source_sha256=NEW.source_sha256) INTO valid;
 END CASE;
 IF valid IS DISTINCT FROM true THEN RAISE EXCEPTION 'legacy native conversion requires original owner and private draft target'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_native_target_binding AFTER INSERT ON legacy_native_adaptation_entities
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_native_target_binding();
CREATE TRIGGER legacy_native_run_immutable BEFORE UPDATE OR DELETE ON legacy_native_adaptation_runs
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_native_entity_immutable BEFORE UPDATE OR DELETE ON legacy_native_adaptation_entities
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_native_residual_immutable BEFORE UPDATE OR DELETE ON legacy_native_adaptation_residuals
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_native_alias_immutable BEFORE UPDATE OR DELETE ON legacy_native_catalog_aliases
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
COMMIT;
