BEGIN;
LOCK TABLE moments,public_moment_media_sets,public_moment_media_attachments,public_moment_media_video_payloads IN SHARE ROW EXCLUSIVE MODE;
LOCK TABLE cities,places,place_sources,place_source_permissions,place_field_evidence,place_candidates IN SHARE MODE;
-- Preserve every original selector before introducing the sole relation source.
CREATE TABLE moment_location_compatibility_backfill AS SELECT id moment_id,city_id,place_id,location_precision,revision,clock_timestamp() recorded_at FROM moments;
ALTER TABLE moment_location_compatibility_backfill ADD PRIMARY KEY(moment_id);
CREATE TRIGGER moment_location_backfill_immutable BEFORE UPDATE OR DELETE ON moment_location_compatibility_backfill FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TABLE moment_location_links (
 id uuid PRIMARY KEY, moment_id uuid NOT NULL REFERENCES moments(id),
 kind text NOT NULL CHECK(kind IN('CITY','PLACE')),city_id text REFERENCES cities(id),place_id uuid REFERENCES places(id),
 precision text NOT NULL CHECK(precision IN('none','city','place')),position integer NOT NULL CHECK(position BETWEEN 0 AND 49),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision BETWEEN 1 AND 9007199254740991),
 status text NOT NULL DEFAULT 'ACTIVE' CHECK(status IN('ACTIVE','REMOVED')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((kind='CITY' AND city_id IS NOT NULL AND place_id IS NULL AND precision IN('none','city')) OR(kind='PLACE' AND city_id IS NULL AND place_id IS NOT NULL)),
 UNIQUE(moment_id,position,status) DEFERRABLE INITIALLY DEFERRED
);
CREATE TABLE moment_location_epochs(moment_id uuid PRIMARY KEY REFERENCES moments(id),version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991));
CREATE FUNCTION birdtie_guard_moment_location_epoch()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'persistent Moment location epoch required';END IF;
 IF TG_OP='UPDATE' AND(NEW.moment_id<>OLD.moment_id OR NEW.version<>OLD.version+1)THEN RAISE EXCEPTION 'monotone Moment location epoch required';END IF;RETURN NEW;END$$;
CREATE TRIGGER moment_location_epoch_guard BEFORE UPDATE OR DELETE ON moment_location_epochs FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_location_epoch();
CREATE FUNCTION birdtie_tick_moment_location_epoch()RETURNS trigger LANGUAGE plpgsql AS $$BEGIN
 INSERT INTO moment_location_epochs(moment_id,version)VALUES(NEW.moment_id,1)ON CONFLICT(moment_id)DO UPDATE SET version=moment_location_epochs.version+1;RETURN NULL;END$$;
CREATE TRIGGER moment_location_epoch AFTER INSERT OR UPDATE ON moment_location_links FOR EACH ROW EXECUTE FUNCTION birdtie_tick_moment_location_epoch();
-- Removed relations do not consume an active ordering slot. The partial target
-- unique index rejects duplicate targets at every granularity.
ALTER TABLE moment_location_links DROP CONSTRAINT moment_location_links_moment_id_position_status_key;
CREATE UNIQUE INDEX moment_location_active_city ON moment_location_links(moment_id,city_id) WHERE status='ACTIVE' AND kind='CITY';
CREATE UNIQUE INDEX moment_location_active_place ON moment_location_links(moment_id,place_id) WHERE status='ACTIVE' AND kind='PLACE';
ALTER TABLE moment_location_links ADD COLUMN active_position integer GENERATED ALWAYS AS(CASE WHEN status='ACTIVE' THEN position END) STORED;
ALTER TABLE moment_location_links ADD CONSTRAINT moment_location_active_order UNIQUE(moment_id,active_position) DEFERRABLE INITIALLY DEFERRED;
CREATE FUNCTION birdtie_guard_moment_location() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'Moment location tombstone required';END IF;
 IF TG_OP='UPDATE' AND(OLD.status='REMOVED' OR NEW.id<>OLD.id OR NEW.moment_id<>OLD.moment_id OR NEW.kind<>OLD.kind OR NEW.city_id IS DISTINCT FROM OLD.city_id OR NEW.place_id IS DISTINCT FROM OLD.place_id OR NEW.revision<>OLD.revision+1)THEN RAISE EXCEPTION 'stable Moment location identity required';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER moment_location_identity BEFORE INSERT OR UPDATE OR DELETE ON moment_location_links FOR EACH ROW EXECUTE FUNCTION birdtie_guard_moment_location();
CREATE FUNCTION birdtie_check_moment_locations() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*) FROM moment_location_links WHERE moment_id=NEW.moment_id AND status='ACTIVE')>50 THEN RAISE EXCEPTION 'Moment locations exceed50';END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER moment_location_bound AFTER INSERT OR UPDATE ON moment_location_links DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_check_moment_locations();
INSERT INTO moment_location_links(id,moment_id,kind,city_id,place_id,precision,position)
SELECT md5('moment-locations-v1:'||id::text)::uuid,id,CASE WHEN place_id IS NOT NULL THEN 'PLACE' ELSE 'CITY' END,
 CASE WHEN place_id IS NULL THEN city_id END,place_id,location_precision,0 FROM moments WHERE city_id IS NOT NULL OR place_id IS NOT NULL;
CREATE FUNCTION birdtie_moment_location_place_source(pid uuid,read_at timestamptz)
RETURNS TABLE(city_id text,city_name text,place_name text,region text,country_code text,coordinate_system text,location_precision text,latitude double precision,longitude double precision,binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT p.city_id,c.name,p.name,c.region,c.country_code::text,p.coordinate_system,p.location_precision,p.latitude::double precision,p.longitude::double precision,
 jsonb_build_array(p.xmin::text,c.xmin::text,(SELECT coalesce(jsonb_agg(jsonb_build_array(s.candidate_id,s.permission_id,s.xmin::text)ORDER BY s.candidate_id),'[]')FROM place_sources s WHERE s.place_id=p.id AND s.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text)ORDER BY f.id),'[]')FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates')))::text,
 least(c.expires_at,least(p.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,rights.retention_until)) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL AND rights.version=f.permission_version
 AND f.valid_from<=read_at AND f.valid_to>read_at AND f.refresh_due>read_at AND rights.retention_until>read_at AND f.conflict_state='NONE')))
 FROM places p JOIN cities c ON c.id=p.city_id WHERE p.id=pid AND p.publication_status='published' AND c.publication_status='published'
 AND(p.expires_at IS NULL OR p.expires_at>read_at)AND(c.expires_at IS NULL OR c.expires_at>read_at)AND (
 NOT EXISTS(SELECT 1 FROM place_sources managed_source WHERE managed_source.place_id=p.id AND managed_source.permission_id IS NOT NULL)
 OR (
 EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=read_at AND rights.retention_until>read_at
 AND f.valid_from<=read_at AND f.valid_to>read_at AND f.refresh_due>read_at AND f.conflict_state='NONE' AND f.observed_at<=read_at AND f.verified_at<=read_at
 AND f.field_key='name' AND f.value=to_jsonb(p.name))
 AND (coalesce(p.address_label,'')='' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=read_at AND rights.retention_until>read_at
 AND f.valid_from<=read_at AND f.valid_to>read_at AND f.refresh_due>read_at AND f.conflict_state='NONE' AND f.observed_at<=read_at AND f.verified_at<=read_at
 AND f.field_key='address' AND f.value=to_jsonb(p.address_label)))
 AND (p.location_precision<>'point' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=read_at AND rights.retention_until>read_at
 AND f.valid_from<=read_at AND f.valid_to>read_at AND f.refresh_due>read_at AND f.conflict_state='NONE' AND f.observed_at<=read_at AND f.verified_at<=read_at
 AND f.field_key='coordinates' AND f.value=jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude)))
 ))
$$;
CREATE FUNCTION birdtie_moment_locations_public(mid uuid,read_at timestamptz)
RETURNS TABLE(id uuid,"position" integer,kind text,"precision" text,city_id text,city_name text,place_id uuid,place_name text,region text,country_code text,coordinate_system text,location_precision text,latitude double precision,longitude double precision,binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT l.id,l.position,'CITY',l.precision,c.id,c.name,NULL::uuid,''::text,c.region,c.country_code::text,NULL::text,NULL::text,NULL::double precision,NULL::double precision,
 jsonb_build_array(l.xmin::text,l.revision,c.xmin::text)::text,c.expires_at
 FROM moment_location_links l JOIN cities c ON c.id=l.city_id WHERE l.moment_id=mid AND l.status='ACTIVE' AND l.kind='CITY' AND l.precision='city'
 AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>read_at)
 UNION ALL SELECT l.id,l.position,CASE WHEN l.precision='city' THEN 'CITY' ELSE 'PLACE' END,l.precision,p.city_id,p.city_name,
 CASE WHEN l.precision='place' THEN l.place_id END,CASE WHEN l.precision='place' THEN p.place_name ELSE '' END,p.region,p.country_code,
 CASE WHEN l.precision='place' THEN p.coordinate_system END,CASE WHEN l.precision='place' THEN p.location_precision END,
 CASE WHEN l.precision='place' THEN p.latitude END,CASE WHEN l.precision='place' THEN p.longitude END,jsonb_build_array(l.xmin::text,l.revision,p.binding)::text,p.deadline
 FROM moment_location_links l CROSS JOIN LATERAL birdtie_moment_location_place_source(l.place_id,read_at)p WHERE l.moment_id=mid AND l.status='ACTIVE' AND l.kind='PLACE' AND l.precision IN('city','place')
$$;
CREATE FUNCTION birdtie_moment_location_compatibility(mid uuid,read_at timestamptz)
RETURNS TABLE(city_id text,city_name text,place_id uuid,place_name text,"precision" text,binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT p.city_id,p.city_name,p.place_id,p.place_name,p.precision,p.binding,p.deadline FROM birdtie_moment_locations_public(mid,read_at)p
 WHERE(SELECT count(*)FROM moment_location_links l WHERE l.moment_id=mid AND l.status='ACTIVE')=1
$$;
CREATE FUNCTION birdtie_moment_location_attachment_current(a public_moment_media_attachments,read_at timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT (birdtie_moment_media_rights_current(a.rights,read_at) AND a.retention_until>read_at
 AND a.display IS NOT NULL AND a.thumbnail IS NOT NULL AND a.metadata->>'validationStatus'='VALIDATED'
 AND a.metadata->>'antivirusScan'='NOT_RUN' AND a.metadata->>'privacyRisk'='UNKNOWN'
 AND a.metadata->>'displaySha256'=encode(sha256(a.display),'hex') AND a.metadata->>'thumbnailSha256'=encode(sha256(a.thumbnail),'hex')
 AND a.metadata->>'displayBytes'=octet_length(a.display)::text AND a.metadata->>'thumbnailBytes'=octet_length(a.thumbnail)::text
 AND a.metadata->>'mediaId'=a.media_id::text AND a.metadata->>'mediaVersion'=a.media_version::text AND a.metadata->>'position'=a.position::text
 AND(COALESCE(NULLIF(a.metadata->>'kind',''),'IMAGE')='IMAGE' AND a.metadata->>'validationKind'='STATIC_IMAGE_DECODE_REENCODE_V1'
 OR a.metadata->>'kind'='VIDEO' AND a.metadata->>'validationKind'='FFMPEG_TRANSCODE_REPROBE_FULL_DECODE_V2' AND EXISTS(SELECT 1 FROM public_moment_media_video_payloads v WHERE v.moment_id=a.moment_id AND v.set_version=a.set_version AND v.position=a.position AND v.media_id=a.media_id AND v.media_version=a.media_version AND v.content IS NOT NULL AND a.metadata->'video'->>'videoSha256'=encode(sha256(v.content),'hex')))) IS TRUE
$$;
-- Rebind only demonstrably current152 sets. No deleted/expired/incomplete set
-- is restored; rights and immutable operation facts are never rewritten.
CREATE TABLE moment_location_media_rebindings(moment_id uuid PRIMARY KEY,set_version bigint NOT NULL,old_binding text NOT NULL,new_binding text NOT NULL,recorded_at timestamptz NOT NULL DEFAULT clock_timestamp());
INSERT INTO moment_location_media_rebindings(moment_id,set_version,old_binding,new_binding)
SELECT s.moment_id,s.version,s.source_binding,encode(sha256(convert_to(to_json(jsonb_build_array(m.xmin::text,a.xmin::text)::text)::text,'UTF8')),'hex')
FROM public_moment_media_sets s JOIN moments m ON m.id=s.moment_id JOIN accounts a ON a.id=m.author_account_id
WHERE s.status='READY' AND s.owner_account_id=m.author_account_id AND s.approved_moment_revision=m.revision
 AND s.source_binding=encode(sha256(convert_to(to_json(birdtie_moment_media_parent_raw(m.id))::text,'UTF8')),'hex')
 AND birdtie_moment_public_binding(m.id,NULL::uuid,clock_timestamp())IS NOT NULL
 AND(SELECT count(*)FROM public_moment_media_attachments x WHERE x.moment_id=s.moment_id AND x.set_version=s.version)BETWEEN 1 AND 9
 AND NOT EXISTS(SELECT 1 FROM public_moment_media_attachments x WHERE x.moment_id=s.moment_id AND x.set_version=s.version AND x.metadata->>'kind'='VIDEO' AND(SELECT count(*)FROM public_moment_media_attachments sibling WHERE sibling.moment_id=s.moment_id AND sibling.set_version=s.version)<>1)
 AND NOT EXISTS(SELECT 1 FROM public_moment_media_attachments x WHERE x.moment_id=s.moment_id AND x.set_version=s.version AND NOT birdtie_moment_location_attachment_current(x,clock_timestamp()));
UPDATE public_moment_media_sets s SET source_binding=b.new_binding FROM moment_location_media_rebindings b WHERE b.moment_id=s.moment_id AND b.set_version=s.version;
CREATE TRIGGER moment_location_rebinding_immutable BEFORE UPDATE OR DELETE ON moment_location_media_rebindings FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE OR REPLACE FUNCTION birdtie_moment_media_parent_raw(mid uuid)RETURNS text LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_array(m.xmin::text,a.xmin::text)::text FROM moments m JOIN accounts a ON a.id=m.author_account_id WHERE m.id=mid
$$;
CREATE OR REPLACE FUNCTION birdtie_moment_has_publishable_content(mid uuid,read_at timestamptz) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT EXISTS(SELECT 1 FROM moments m WHERE m.id=mid AND(length(regexp_replace(m.body,'[[:space:]]','','g'))>0
 OR EXISTS(SELECT 1 FROM public_moment_media_sets s WHERE s.moment_id=m.id AND s.owner_account_id=m.author_account_id AND s.status='READY' AND s.approved_moment_revision=m.revision
 AND s.source_binding=encode(sha256(convert_to(to_json(birdtie_moment_media_parent_raw(m.id))::text,'UTF8')),'hex')
 AND(SELECT count(*)FROM public_moment_media_attachments a WHERE a.moment_id=s.moment_id AND a.set_version=s.version)BETWEEN 1 AND 9
 AND NOT EXISTS(SELECT 1 FROM public_moment_media_attachments a WHERE a.moment_id=s.moment_id AND a.set_version=s.version AND a.metadata->>'kind'='VIDEO' AND(SELECT count(*)FROM public_moment_media_attachments sibling WHERE sibling.moment_id=s.moment_id AND sibling.set_version=s.version)<>1)
 AND NOT EXISTS(SELECT 1 FROM public_moment_media_attachments a WHERE a.moment_id=s.moment_id AND a.set_version=s.version AND NOT birdtie_moment_location_attachment_current(a,read_at)))
 OR EXISTS(SELECT 1 FROM public_moment_photos p WHERE p.moment_id=m.id AND p.owner_account_id=m.author_account_id AND p.approved_moment_revision=m.revision AND p.status='READY' AND p.thumbnail IS NOT NULL AND p.retention_until>read_at AND birdtie_public_moment_photo_current(m.id,m.author_account_id,read_at))))
$$;
CREATE OR REPLACE FUNCTION birdtie_moment_public_binding(mid uuid,viewer uuid,read_at timestamptz)RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(m.xmin::text,a.xmin::text,
 (SELECT account.xmin::text FROM accounts account WHERE account.id=viewer),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text)ORDER BY ep.owner_account_id,ep.target_account_id),'[]')FROM person_block_control_epochs ep WHERE viewer IS NOT NULL AND((ep.owner_account_id=viewer AND ep.target_account_id=a.id)OR(ep.owner_account_id=a.id AND ep.target_account_id=viewer))))::text,'UTF8')),'hex')
 FROM moments m JOIN accounts a ON a.id=m.author_account_id WHERE m.id=mid AND m.status='published' AND m.visibility='public' AND m.revision>=2
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=read_at
 AND a.account_type='person' AND a.status='active' AND birdtie_moment_has_publishable_content(m.id,read_at)
 AND(viewer IS NULL OR EXISTS(SELECT 1 FROM accounts account WHERE account.id=viewer AND account.account_type='person' AND account.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND((b.blocker_account_id=viewer AND b.blocked_account_id=a.id)OR(b.blocker_account_id=a.id AND b.blocked_account_id=viewer)))
$$;
CREATE FUNCTION birdtie_moment_location_contract()RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'moment-locations-v1'::text$$;
COMMIT;
