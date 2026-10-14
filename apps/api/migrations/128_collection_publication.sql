BEGIN;
-- Extend the original theme-map entity; Journey already has these fields in006.
ALTER TABLE theme_maps DROP CONSTRAINT theme_maps_visibility_check;
ALTER TABLE theme_maps ADD CONSTRAINT theme_maps_visibility_check CHECK(visibility IN('private','public'));
ALTER TABLE theme_maps DROP CONSTRAINT theme_maps_status_check;
ALTER TABLE theme_maps ADD CONSTRAINT theme_maps_status_check CHECK(status IN('draft','published','withdrawn'));
ALTER TABLE theme_maps ADD COLUMN owner_confirmed_at timestamptz;
ALTER TABLE theme_maps ADD COLUMN published_at timestamptz;
ALTER TABLE theme_maps ADD CONSTRAINT theme_map_public_confirmation CHECK(status<>'published' OR(visibility='public' AND owner_confirmed_at IS NOT NULL AND published_at IS NOT NULL AND owner_confirmed_at<=published_at));
CREATE INDEX theme_maps_public_city_time ON theme_maps(city_id,published_at DESC,id) WHERE status='published' AND visibility='public';
CREATE TABLE personal_collection_publication_operations (
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 kind text NOT NULL CHECK(kind IN('journey','theme_map')),
 target_id uuid NOT NULL,
 journey_id uuid REFERENCES journeys(id),
 theme_map_id uuid REFERENCES theme_maps(id),
 action text NOT NULL CHECK(action IN('PUBLISH','UNPUBLISH')),
 expected_revision bigint NOT NULL CHECK(expected_revision>0 AND expected_revision<9223372036854775807),
 result_revision bigint NOT NULL CHECK(result_revision=expected_revision+1),
 input_sha256 bytea NOT NULL CHECK(octet_length(input_sha256)=32),
 committed_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 PRIMARY KEY(owner_account_id,operation_id),
 CHECK((kind='journey' AND journey_id IS NOT NULL AND journey_id=target_id AND theme_map_id IS NULL) OR(kind='theme_map' AND theme_map_id IS NOT NULL AND theme_map_id=target_id AND journey_id IS NULL))
);
CREATE FUNCTION birdtie_collection_publication_operation_immutable() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 RAISE EXCEPTION 'collection publication receipt is immutable' USING ERRCODE='23514';
END $$;
CREATE TRIGGER collection_publication_operation_immutable BEFORE UPDATE OR DELETE ON personal_collection_publication_operations FOR EACH ROW EXECUTE FUNCTION birdtie_collection_publication_operation_immutable();
COMMIT;
