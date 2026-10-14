BEGIN;
CREATE TABLE personal_collection_append_operations(
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('journey','theme_map')),
 target_id uuid NOT NULL,
	city_id text NOT NULL REFERENCES cities(id),
 expected_revision bigint NOT NULL CHECK(expected_revision BETWEEN 1 AND 8999999999999999),
	status text NOT NULL CHECK(status IN('SUCCEEDED','STOPPED')),
 applied_revision bigint NOT NULL CHECK((status='SUCCEEDED' AND applied_revision=expected_revision+1) OR(status='STOPPED' AND applied_revision=0)),
 input_sha256 bytea NOT NULL CHECK(octet_length(input_sha256)=32),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK(isfinite(created_at)),
 PRIMARY KEY(owner_account_id,operation_id)
);
CREATE FUNCTION birdtie_guard_collection_append_operation() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE actual_owner uuid;actual_revision bigint;actual_visibility text;actual_status text;actual_city text;
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'collection append receipt is immutable' USING ERRCODE='23514';END IF;
 IF NEW.kind='journey' THEN
  SELECT owner_account_id,revision,visibility,status,city_id INTO actual_owner,actual_revision,actual_visibility,actual_status,actual_city FROM journeys WHERE id=NEW.target_id FOR KEY SHARE;
 ELSE
  SELECT owner_account_id,revision,visibility,status,city_id INTO actual_owner,actual_revision,actual_visibility,actual_status,actual_city FROM theme_maps WHERE id=NEW.target_id FOR KEY SHARE;
 END IF;
 IF actual_owner IS DISTINCT FROM NEW.owner_account_id OR actual_city IS DISTINCT FROM NEW.city_id OR(NEW.status='SUCCEEDED' AND(actual_revision IS DISTINCT FROM NEW.applied_revision OR actual_visibility IS DISTINCT FROM 'private' OR actual_status IS DISTINCT FROM 'draft')) THEN
  RAISE EXCEPTION 'append receipt requires actual owned draft revision' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER personal_collection_append_operation_guard BEFORE INSERT OR UPDATE OR DELETE ON personal_collection_append_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_collection_append_operation();
COMMIT;
