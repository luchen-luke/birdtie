BEGIN;
-- Opt-in belongs to one exact author publication. Legacy publications have no
-- grant row and remain unavailable for copying; no private text is replicated.
CREATE TABLE personal_collection_copy_grants(
 kind text NOT NULL CHECK(kind IN('journey','theme_map')),
 target_id uuid NOT NULL,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 publication_revision bigint NOT NULL CHECK(publication_revision>=2),
 grant_version bigint NOT NULL CHECK(grant_version BETWEEN 1 AND 8999999999999999),
 allow_private_place_copy boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(kind,target_id),
 CHECK(NOT allow_private_place_copy OR publication_revision<=8999999999999999)
);
CREATE FUNCTION birdtie_collection_copy_grant_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE valid boolean;
BEGIN
 IF TG_OP='DELETE' THEN RAISE EXCEPTION 'copy grant history cannot be deleted' USING ERRCODE='23514'; END IF;
 IF TG_OP='UPDATE' AND (NEW.kind<>OLD.kind OR NEW.target_id<>OLD.target_id OR NEW.owner_account_id<>OLD.owner_account_id OR NOT(NEW.grant_version=OLD.grant_version+1 OR(OLD.grant_version=8999999999999999 AND NEW.grant_version=OLD.grant_version AND NOT NEW.allow_private_place_copy AND NEW.publication_revision>OLD.publication_revision))) THEN
  RAISE EXCEPTION 'copy grant version changed' USING ERRCODE='23514';
 END IF;
 IF NEW.kind='journey' THEN
  SELECT owner_account_id=NEW.owner_account_id AND revision=NEW.publication_revision AND (NOT NEW.allow_private_place_copy OR(status='published' AND visibility='public')) INTO valid FROM journeys WHERE id=NEW.target_id;
 ELSE
  SELECT owner_account_id=NEW.owner_account_id AND revision=NEW.publication_revision AND (NOT NEW.allow_private_place_copy OR(status='published' AND visibility='public')) INTO valid FROM theme_maps WHERE id=NEW.target_id;
 END IF;
 IF valid IS DISTINCT FROM true OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.owner_account_id AND account_type='person' AND status='active') THEN
  RAISE EXCEPTION 'copy author publication absent' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER collection_copy_grant_guard BEFORE INSERT OR UPDATE OR DELETE ON personal_collection_copy_grants FOR EACH ROW EXECUTE FUNCTION birdtie_collection_copy_grant_guard();
CREATE TABLE personal_collection_copy_operations(
 receiver_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 destination_kind text CHECK(destination_kind IN('journey','theme_map')),
 destination_id uuid,
 source_kind text CHECK(source_kind IN('journey','theme_map')),
 source_id uuid,
 publication_revision bigint,
 grant_version bigint,
 ordered_place_ids_sha256 bytea,
 input_sha256 bytea,
 result_revision bigint NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(receiver_account_id,operation_id),
 UNIQUE(destination_kind,destination_id),
 CHECK((status='STOPPED' AND result_revision=0 AND destination_kind IS NULL AND destination_id IS NULL AND source_kind IS NULL AND source_id IS NULL AND publication_revision IS NULL AND grant_version IS NULL AND ordered_place_ids_sha256 IS NULL AND input_sha256 IS NULL)
 OR(status='SUCCEEDED' AND result_revision=1 AND destination_kind IS NOT NULL AND destination_id IS NOT NULL AND destination_id<>operation_id AND source_kind IS NOT NULL AND source_kind=destination_kind AND source_id IS NOT NULL AND source_id<>destination_id AND publication_revision IS NOT NULL AND grant_version IS NOT NULL AND ordered_place_ids_sha256 IS NOT NULL AND input_sha256 IS NOT NULL AND publication_revision BETWEEN 2 AND 8999999999999999 AND grant_version BETWEEN 1 AND 8999999999999999 AND octet_length(ordered_place_ids_sha256)=32 AND octet_length(input_sha256)=32))
);
CREATE FUNCTION birdtie_collection_copy_operation_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE valid boolean;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'copy receipt immutable' USING ERRCODE='23514'; END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.receiver_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'copy actor unavailable' USING ERRCODE='23514'; END IF;
 IF NEW.status='STOPPED' THEN RETURN NEW; END IF;
 IF NOT EXISTS(SELECT 1 FROM personal_collection_copy_grants WHERE kind=NEW.source_kind AND target_id=NEW.source_id AND publication_revision=NEW.publication_revision AND grant_version=NEW.grant_version AND allow_private_place_copy) THEN RAISE EXCEPTION 'copy grant unavailable' USING ERRCODE='23514'; END IF;
 IF NEW.destination_kind='journey' THEN
  SELECT owner_account_id=NEW.receiver_account_id AND revision=1 AND visibility='private' AND status='draft' AND summary='' INTO valid FROM journeys WHERE id=NEW.destination_id;
  IF EXISTS(SELECT 1 FROM journey_stops WHERE journey_id=NEW.destination_id AND (note<>'' OR location_visibility<>'hidden')) THEN valid=false; END IF;
 ELSE
  SELECT owner_account_id=NEW.receiver_account_id AND revision=1 AND visibility='private' AND status='draft' AND summary='' INTO valid FROM theme_maps WHERE id=NEW.destination_id;
  IF EXISTS(SELECT 1 FROM theme_map_places WHERE theme_map_id=NEW.destination_id AND (note<>'' OR location_visibility<>'hidden')) THEN valid=false; END IF;
 END IF;
 IF valid IS DISTINCT FROM true THEN RAISE EXCEPTION 'copy private destination absent' USING ERRCODE='23514'; END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER collection_copy_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON personal_collection_copy_operations FOR EACH ROW EXECUTE FUNCTION birdtie_collection_copy_operation_guard();
CREATE TABLE personal_collection_copy_origins(
 destination_kind text NOT NULL CHECK(destination_kind IN('journey','theme_map')),
 destination_id uuid NOT NULL,
 receiver_account_id uuid NOT NULL REFERENCES accounts(id),
 source_kind text NOT NULL CHECK(source_kind=destination_kind),
 source_id uuid NOT NULL,
 publication_revision bigint NOT NULL,
 grant_version bigint NOT NULL,
 origin_authority text NOT NULL CHECK(origin_authority ~ '^[0-9a-f]{64}$'),
 PRIMARY KEY(destination_kind,destination_id)
);
CREATE TRIGGER collection_copy_origin_immutable BEFORE UPDATE OR DELETE ON personal_collection_copy_origins FOR EACH ROW EXECUTE FUNCTION birdtie_collection_publication_operation_immutable();
CREATE TABLE personal_collection_copy_borrowed_places(
 destination_kind text NOT NULL,
 destination_id uuid NOT NULL,
 place_id uuid NOT NULL REFERENCES places(id),
 PRIMARY KEY(destination_kind,destination_id,place_id),
 FOREIGN KEY(destination_kind,destination_id) REFERENCES personal_collection_copy_origins(destination_kind,destination_id)
);
COMMIT;
