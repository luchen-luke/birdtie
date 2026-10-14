-- Only the explicitly preserved Hangzhou source, in a separate native database.
-- Does not import credentials, Sessions, public flags, Place/media permission,
-- or an old Civu schema. The original 29-table snapshot remains private outside DB.
CREATE FUNCTION birdtie_legacy_fixed_keys(v jsonb,ks text[]) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE WHEN jsonb_typeof(v)='object' THEN
  (SELECT count(*) FROM jsonb_object_keys(v))=cardinality(ks)
  AND NOT EXISTS(SELECT 1 FROM jsonb_object_keys(v) k WHERE NOT(k=ANY(ks)))
 ELSE false END
$$;
CREATE FUNCTION birdtie_legacy_nonnegative_counts(v jsonb) RETURNS boolean
LANGUAGE sql IMMUTABLE STRICT AS $$
 SELECT CASE WHEN jsonb_typeof(v)='object' THEN NOT EXISTS(
  SELECT 1 FROM jsonb_each(v) e WHERE jsonb_typeof(e.value)<>'number'
   OR e.value::text !~ '^[0-9]+$' OR length(e.value::text)>6
   OR (CASE WHEN e.value::text ~ '^[0-9]{1,6}$' THEN e.value::text::integer ELSE 100001 END)>100000
 ) ELSE false END
$$;
CREATE FUNCTION birdtie_legacy_relationships_safe(v jsonb) RETURNS boolean
LANGUAGE plpgsql IMMUTABLE STRICT AS $$
DECLARE r jsonb;
BEGIN
 IF birdtie_legacy_fixed_keys(v,ARRAY['places','media','mediaLocations','thread','threadLegs']) IS DISTINCT FROM true
  OR jsonb_typeof(v->'places') IS DISTINCT FROM 'array' OR jsonb_typeof(v->'media') IS DISTINCT FROM 'array'
  OR jsonb_typeof(v->'mediaLocations') IS DISTINCT FROM 'array' OR jsonb_typeof(v->'threadLegs') IS DISTINCT FROM 'array' THEN RETURN false;END IF;
 FOR r IN SELECT value FROM jsonb_array_elements(v->'places') LOOP
  IF birdtie_legacy_fixed_keys(r,ARRAY['sourcePlaceId','rawName','position']) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 FOR r IN SELECT value FROM jsonb_array_elements(v->'media') LOOP
  IF birdtie_legacy_fixed_keys(r,ARRAY['sourceMediaId','position','state']) IS DISTINCT FROM true OR (r->>'state'='UNAVAILABLE') IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 FOR r IN SELECT value FROM jsonb_array_elements(v->'mediaLocations') LOOP
  IF birdtie_legacy_fixed_keys(r,ARRAY['sourceMediaId','latitude','longitude','mapEnabled','sourcePlaceIds','capturedDate']) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 IF v->'thread'<>'null'::jsonb AND birdtie_legacy_fixed_keys(v->'thread',ARRAY['sourceThreadId','position','entryCount','routeRevision','createdAt']) IS DISTINCT FROM true THEN RETURN false;END IF;
 FOR r IN SELECT value FROM jsonb_array_elements(v->'threadLegs') LOOP
  IF birdtie_legacy_fixed_keys(r,ARRAY['sourceFromNoteId','sourceToNoteId','fromPosition','toPosition','mode','sourceFromAnchorPlaceId','sourceToAnchorPlaceId','createdAt','updatedAt']) IS DISTINCT FROM true THEN RETURN false;END IF;
 END LOOP;
 RETURN true;
END;
$$;

CREATE TABLE legacy_import_runs (
 run_id uuid PRIMARY KEY,
 source_system text NOT NULL CHECK(source_system='hangzhou-civu-20261014'),
 source_protocol text NOT NULL CHECK(source_protocol='birdtie-civu-hangzhou-source-v1'),
 backup_cipher_sha256 text NOT NULL CHECK(backup_cipher_sha256='5dbc323ae7f16df233dea5a412d30c6f563632150e1c639443c4a3997ec5838f'),
 schema_sha256 text NOT NULL CHECK(schema_sha256='b4989ea80d771d454e61b93fb6b65b8692bde9e9a68c4a686cad57068801cd9c'),
 snapshot_sha256 text NOT NULL CHECK(snapshot_sha256 ~ '^[0-9a-f]{64}$'),
 source_file_sha256 text NOT NULL CHECK(source_file_sha256 ~ '^[0-9a-f]{64}$'),
 manifest_file_sha256 text NOT NULL CHECK(manifest_file_sha256 ~ '^[0-9a-f]{64}$'),
 source_header jsonb NOT NULL,
 source_counts jsonb NOT NULL,
 deferred_counts jsonb NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK(birdtie_legacy_fixed_keys(source_header,ARRAY['instanceTag','container','database','backupCipherSha256','schemaSha256','snapshotSha256'])),
 CHECK((source_header->>'instanceTag'=source_system AND source_header->>'container'='birdtie-civu-restore-20261014' AND source_header->>'database'='civu_restore'
  AND source_header->>'backupCipherSha256'=backup_cipher_sha256 AND source_header->>'schemaSha256'=schema_sha256 AND source_header->>'snapshotSha256'=snapshot_sha256) IS TRUE),
 CHECK(birdtie_legacy_fixed_keys(source_counts,ARRAY['users','auth_identities','cities','regions','geo_areas','places','place_area_relations','place_provider_refs','user_saved_places','place_save_sources','journeys','journey_stops','journey_arrangements','journey_notes','journey_members','journey_media','journey_publications','journey_publication_places','theme_maps','theme_map_entries','theme_map_entry_sources','notes','note_places','note_media','note_media_locations','moment_threads','moment_thread_legs','media_assets','place_images']) AND birdtie_legacy_nonnegative_counts(source_counts)),
 CHECK(birdtie_legacy_fixed_keys(deferred_counts,ARRAY['places','user_saved_places','journeys','theme_maps','media_assets','auth_identities']) AND birdtie_legacy_nonnegative_counts(deferred_counts))
);

CREATE TABLE legacy_entity_mappings (
 source_system text NOT NULL CHECK(source_system='hangzhou-civu-20261014'),
 entity_kind text NOT NULL CHECK(entity_kind IN('user','note')),
 source_id uuid NOT NULL,
 target_id uuid NOT NULL,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 run_id uuid NOT NULL REFERENCES legacy_import_runs(run_id),
 source_version bigint NOT NULL CHECK(source_version BETWEEN 1 AND 8999999999999999),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 source_status text NOT NULL,
 source_data_origin text NOT NULL,
 claim_status text NOT NULL DEFAULT 'UNCLAIMED' CHECK(claim_status='UNCLAIMED'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(source_system,entity_kind,source_id),
 UNIQUE(entity_kind,target_id),
 CHECK(entity_kind<>'user' OR target_id=owner_account_id),
 CHECK(entity_kind<>'note' OR(source_status IN('DELETED','ARCHIVED') AND source_data_origin='legacy_note_owner_archive'))
);
CREATE INDEX legacy_mapping_run ON legacy_entity_mappings(run_id,entity_kind);

CREATE TABLE legacy_note_archives (
 id uuid PRIMARY KEY,
 source_system text NOT NULL CHECK(source_system='hangzhou-civu-20261014'),
 source_note_id uuid NOT NULL,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 run_id uuid NOT NULL REFERENCES legacy_import_runs(run_id),
 source_version bigint NOT NULL CHECK(source_version BETWEEN 1 AND 8999999999999999),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 deleted boolean NOT NULL,
 title text NOT NULL,
 body text NOT NULL,
 recorded_start_date text,
 recorded_end_date text,
 source_visibility text NOT NULL,
 source_moderation_status text NOT NULL,
 source_relationships jsonb,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(source_system,source_note_id),
 CHECK((deleted AND title='' AND body='' AND recorded_start_date IS NULL AND recorded_end_date IS NULL AND source_visibility='' AND source_moderation_status='' AND source_relationships IS NULL)
  OR(NOT deleted AND source_relationships IS NOT NULL AND birdtie_legacy_relationships_safe(source_relationships)))
);
CREATE INDEX legacy_note_archive_owner_page ON legacy_note_archives(owner_account_id,created_at DESC,id DESC);

CREATE FUNCTION birdtie_legacy_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 RAISE EXCEPTION 'legacy preservation history is immutable';
END;
$$;
CREATE TRIGGER legacy_run_immutable BEFORE UPDATE OR DELETE ON legacy_import_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_mapping_immutable BEFORE UPDATE OR DELETE ON legacy_entity_mappings FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_note_immutable BEFORE UPDATE OR DELETE ON legacy_note_archives FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();

CREATE FUNCTION birdtie_legacy_import_binding() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE m legacy_entity_mappings; n legacy_note_archives;
BEGIN
 IF TG_TABLE_NAME='legacy_entity_mappings' THEN
  IF NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal'
    JOIN user_profiles p ON p.account_id=a.id WHERE a.id=NEW.owner_account_id AND a.account_type='person'
    AND a.status='suspended' AND ag.status='suspended' AND p.visibility='private') THEN
   RAISE EXCEPTION 'legacy owner must start suspended private and unclaimed';
  END IF;
  IF NEW.entity_kind='note' THEN
   SELECT * INTO n FROM legacy_note_archives WHERE id=NEW.target_id;
   IF NOT FOUND OR n.source_system<>NEW.source_system OR n.source_note_id<>NEW.source_id OR n.owner_account_id<>NEW.owner_account_id OR n.run_id<>NEW.run_id OR n.source_version<>NEW.source_version OR n.source_sha256<>NEW.source_sha256
    OR n.deleted<>(NEW.source_status='DELETED') OR NOT EXISTS(SELECT 1 FROM legacy_entity_mappings u WHERE u.source_system=NEW.source_system AND u.entity_kind='user' AND u.target_id=NEW.owner_account_id) THEN
    RAISE EXCEPTION 'legacy note mapping binding mismatch';
   END IF;
  END IF;
 ELSE
  SELECT * INTO m FROM legacy_entity_mappings WHERE source_system=NEW.source_system AND entity_kind='note' AND source_id=NEW.source_note_id;
  IF NOT FOUND OR m.target_id<>NEW.id OR m.owner_account_id<>NEW.owner_account_id OR m.run_id<>NEW.run_id OR m.source_version<>NEW.source_version OR m.source_sha256<>NEW.source_sha256 OR NEW.deleted<>(m.source_status='DELETED') THEN
   RAISE EXCEPTION 'legacy archive binding mismatch';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER legacy_mapping_binding AFTER INSERT ON legacy_entity_mappings DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_import_binding();
CREATE CONSTRAINT TRIGGER legacy_archive_binding AFTER INSERT ON legacy_note_archives DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_import_binding();

CREATE FUNCTION birdtie_legacy_run_complete() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF (SELECT count(*) FROM legacy_entity_mappings WHERE run_id=NEW.run_id AND entity_kind='user')<>(NEW.source_counts->>'users')::integer
  OR(SELECT count(*) FROM legacy_entity_mappings WHERE run_id=NEW.run_id AND entity_kind='note')<>(NEW.source_counts->>'notes')::integer
  OR(SELECT count(*) FROM legacy_note_archives WHERE run_id=NEW.run_id)<>(NEW.source_counts->>'notes')::integer THEN
  RAISE EXCEPTION 'legacy import must preserve complete users and note tombstones';
 END IF;
 IF EXISTS(SELECT 1 FROM jsonb_each(NEW.deferred_counts) d WHERE d.value IS DISTINCT FROM NEW.source_counts->d.key) THEN
  RAISE EXCEPTION 'legacy unsupported domains must stay deferred';
 END IF;
 RETURN NEW;
END;
$$;
CREATE CONSTRAINT TRIGGER legacy_run_complete AFTER INSERT ON legacy_import_runs DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_run_complete();
