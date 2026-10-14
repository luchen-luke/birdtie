BEGIN;
-- Private read-only source records. They are neither current Journeys/Places
-- nor saved_items, memberships, publication consent, or identity claims.
CREATE TABLE legacy_personal_archive_runs (
 run_id uuid PRIMARY KEY REFERENCES legacy_import_runs(run_id),
 projection_protocol text NOT NULL CHECK(projection_protocol='birdtie-civu-personal-archive-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256 ~ '^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'
  AND counts ?& ARRAY['journeys','journeyTombstones','themeMaps','themeMapTombstones','savedPlaces']
  AND counts-ARRAY['journeys','journeyTombstones','themeMaps','themeMapTombstones','savedPlaces']='{}'::jsonb),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_personal_archives (
 id uuid PRIMARY KEY,
 run_id uuid NOT NULL REFERENCES legacy_personal_archive_runs(run_id),
 source_system text NOT NULL CHECK(source_system='hangzhou-civu-20261014'),
 kind text NOT NULL CHECK(kind IN('journey','theme_map','saved_place')),
 source_key text NOT NULL,
 source_owner_id uuid NOT NULL,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 source_revision bigint CHECK(source_revision BETWEEN 1 AND 8999999999999999),
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 deleted boolean NOT NULL,
 source_deleted_at timestamptz,
 title text NOT NULL CHECK(length(title)<=1000),
 source_created_at timestamptz,
 source_updated_at timestamptz,
 payload jsonb,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(source_system,kind,source_key),
 CHECK((kind IN('journey','theme_map') AND source_key ~ '^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$') OR
   (kind='saved_place' AND source_key=source_owner_id::text||':'||coalesce(payload->>'sourcePlaceId',''))),
 CHECK((deleted AND kind<>'saved_place' AND title='' AND payload IS NULL AND source_deleted_at IS NOT NULL) OR
   (NOT deleted AND source_deleted_at IS NULL AND jsonb_typeof(payload)='object')),
 CHECK(kind<>'saved_place' OR source_revision IS NULL)
);
CREATE INDEX legacy_personal_archive_owner_page ON legacy_personal_archives(owner_account_id,kind,id);
CREATE FUNCTION birdtie_legacy_personal_archive_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_entity_mappings m JOIN accounts a ON a.id=m.target_id
  WHERE m.source_system=NEW.source_system AND m.entity_kind='user' AND m.source_id=NEW.source_owner_id
   AND m.run_id=NEW.run_id AND m.target_id=NEW.owner_account_id AND m.owner_account_id=a.id AND a.account_type='person')
 THEN RAISE EXCEPTION 'original legacy owner mapping required'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_personal_archive_binding AFTER INSERT ON legacy_personal_archives
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_personal_archive_binding();
CREATE TRIGGER legacy_personal_archive_immutable BEFORE UPDATE OR DELETE ON legacy_personal_archives
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_personal_archive_run_immutable BEFORE UPDATE OR DELETE ON legacy_personal_archive_runs
 FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_legacy_personal_archive_complete() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual jsonb;
BEGIN
 SELECT jsonb_build_object('journeys',count(*) FILTER(WHERE kind='journey'),
  'journeyTombstones',count(*) FILTER(WHERE kind='journey' AND deleted),
  'themeMaps',count(*) FILTER(WHERE kind='theme_map'),
  'themeMapTombstones',count(*) FILTER(WHERE kind='theme_map' AND deleted),
  'savedPlaces',count(*) FILTER(WHERE kind='saved_place')) INTO actual
 FROM legacy_personal_archives WHERE run_id=NEW.run_id;
 IF actual<>NEW.counts THEN RAISE EXCEPTION 'complete legacy personal archive required'; END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_personal_archive_complete AFTER INSERT ON legacy_personal_archive_runs
 DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_personal_archive_complete();
COMMIT;
