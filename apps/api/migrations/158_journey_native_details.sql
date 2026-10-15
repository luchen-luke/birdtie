BEGIN;
ALTER TABLE journeys ADD COLUMN detail_schema text;
ALTER TABLE journeys ADD COLUMN journey_start_date date;
ALTER TABLE journeys ADD COLUMN journey_end_date date;
ALTER TABLE journeys ADD COLUMN journey_date_label text NOT NULL DEFAULT '' CHECK(length(journey_date_label)<=1000);
ALTER TABLE journeys ADD COLUMN journey_days integer NOT NULL DEFAULT 0 CHECK(journey_days BETWEEN 0 AND 36600);
ALTER TABLE journeys ALTER COLUMN city_id DROP NOT NULL;
ALTER TABLE journeys DROP CONSTRAINT journeys_title_check;
ALTER TABLE journeys ADD CONSTRAINT journey_detail_title CHECK(length(title)<=160 AND(detail_schema IS NOT NULL OR length(title)>=1));
ALTER TABLE journeys ADD CONSTRAINT journey_detail_date_order CHECK(journey_start_date IS NULL OR journey_end_date IS NULL OR journey_end_date>=journey_start_date);
ALTER TABLE journeys ADD CONSTRAINT journey_detail_private CHECK(detail_schema IS NULL OR(detail_schema='birdtie.journey-details.v1' AND visibility='private' AND status IN('draft','withdrawn')));
ALTER TABLE journeys ADD CONSTRAINT journey_detail_optional_city CHECK(city_id IS NOT NULL OR(detail_schema IS NOT NULL AND detail_schema='birdtie.journey-details.v1'));

-- This relation is the canonical schedule of the same journeys row. Legacy
-- journey_stops stay empty for detailed Journeys; there is no shadow Journey.
CREATE TABLE journey_itinerary_entries (
 journey_id uuid NOT NULL REFERENCES journeys(id) ON DELETE CASCADE,
 entry_id uuid NOT NULL,
 sequence_no integer NOT NULL CHECK(sequence_no BETWEEN 1 AND 500),
 entry_type text NOT NULL CHECK(entry_type IN('STOP','ARRANGEMENT','NOTE')),
 city_id text REFERENCES cities(id),
 place_id uuid REFERENCES places(id),
 stop_id uuid,from_stop_id uuid,to_stop_id uuid,
 material jsonb NOT NULL CHECK(jsonb_typeof(material)='object' AND NOT material ? 'origin' AND material ?& ARRAY['id','entryType','position','cityId','placeId','stopId','fromStopId','toStopId']),
 PRIMARY KEY(journey_id,entry_id),UNIQUE(journey_id,sequence_no),
 FOREIGN KEY(place_id,city_id) REFERENCES places(id,city_id),
 FOREIGN KEY(journey_id,stop_id) REFERENCES journey_itinerary_entries(journey_id,entry_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(journey_id,from_stop_id) REFERENCES journey_itinerary_entries(journey_id,entry_id) DEFERRABLE INITIALLY DEFERRED,
 FOREIGN KEY(journey_id,to_stop_id) REFERENCES journey_itinerary_entries(journey_id,entry_id) DEFERRABLE INITIALLY DEFERRED,
 CHECK(material->>'id'=entry_id::text AND material->>'entryType'=entry_type AND(material->>'position')::integer=sequence_no),
 CHECK(coalesce(material->>'cityId','')=coalesce(city_id,'') AND coalesce(material->>'placeId','')=coalesce(place_id::text,'')),
 CHECK(coalesce(material->>'stopId','')=coalesce(stop_id::text,'') AND coalesce(material->>'fromStopId','')=coalesce(from_stop_id::text,'') AND coalesce(material->>'toStopId','')=coalesce(to_stop_id::text,'')),
 CHECK(entry_type='ARRANGEMENT' OR(stop_id IS NULL AND from_stop_id IS NULL AND to_stop_id IS NULL))
);
CREATE FUNCTION birdtie_journey_details_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE jid uuid;schema text;
BEGIN
 IF TG_TABLE_NAME='journeys' THEN jid=NEW.id;ELSE jid=coalesce(NEW.journey_id,OLD.journey_id);END IF;
 SELECT detail_schema INTO schema FROM journeys WHERE id=jid;
 IF schema IS NULL THEN
  IF EXISTS(SELECT 1 FROM journey_itinerary_entries WHERE journey_id=jid) THEN RAISE EXCEPTION 'detailed Journey schema required' USING ERRCODE='23514';END IF;
 ELSE
  IF EXISTS(SELECT 1 FROM journey_stops WHERE journey_id=jid) THEN RAISE EXCEPTION 'one canonical Journey schedule required' USING ERRCODE='23514';END IF;
  IF EXISTS(SELECT 1 FROM journey_itinerary_entries e LEFT JOIN journey_itinerary_entries stop ON stop.journey_id=e.journey_id AND stop.entry_id=e.stop_id LEFT JOIN journey_itinerary_entries fs ON fs.journey_id=e.journey_id AND fs.entry_id=e.from_stop_id LEFT JOIN journey_itinerary_entries ts ON ts.journey_id=e.journey_id AND ts.entry_id=e.to_stop_id WHERE e.journey_id=jid AND((e.stop_id IS NOT NULL AND stop.entry_type IS DISTINCT FROM 'STOP') OR(e.from_stop_id IS NOT NULL AND fs.entry_type IS DISTINCT FROM 'STOP') OR(e.to_stop_id IS NOT NULL AND ts.entry_type IS DISTINCT FROM 'STOP'))) THEN RAISE EXCEPTION 'Journey links require current same-Journey stops' USING ERRCODE='23514';END IF;
 END IF;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journey_details_header AFTER INSERT OR UPDATE ON journeys DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_journey_details_guard();
CREATE CONSTRAINT TRIGGER journey_details_entries AFTER INSERT OR UPDATE OR DELETE ON journey_itinerary_entries DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_journey_details_guard();
CREATE CONSTRAINT TRIGGER journey_details_compatibility AFTER INSERT OR UPDATE OR DELETE ON journey_stops DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_journey_details_guard();

CREATE TABLE legacy_journey_adaptation_runs (
 run_id uuid PRIMARY KEY REFERENCES legacy_native_adaptation_runs(run_id),
 protocol text NOT NULL CHECK(protocol='birdtie-legacy-journey-details-v1'),
 projection_sha256 text NOT NULL CHECK(projection_sha256 ~ '^[0-9a-f]{64}$'),
 counts jsonb NOT NULL CHECK(jsonb_typeof(counts)='object'),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE TABLE legacy_journey_adaptations (
 run_id uuid NOT NULL REFERENCES legacy_journey_adaptation_runs(run_id),
 source_key text NOT NULL,
 source_sha256 text NOT NULL CHECK(source_sha256 ~ '^[0-9a-f]{64}$'),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 target_id uuid NOT NULL UNIQUE,
 PRIMARY KEY(run_id,source_key)
);
CREATE TABLE legacy_journey_entry_origins (
 run_id uuid NOT NULL,source_key text NOT NULL,
 journey_id uuid NOT NULL,entry_id uuid NOT NULL,
 original_place_id uuid,
 origin jsonb NOT NULL CHECK(jsonb_typeof(origin)='object'),
 PRIMARY KEY(journey_id,entry_id),
 FOREIGN KEY(run_id,source_key) REFERENCES legacy_journey_adaptations(run_id,source_key)
);
CREATE FUNCTION birdtie_legacy_journey_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NOT EXISTS(SELECT 1 FROM legacy_personal_archives ar JOIN legacy_entity_mappings m ON m.source_system=ar.source_system AND m.entity_kind='user' AND m.source_id=ar.source_owner_id AND m.target_id=ar.owner_account_id AND m.owner_account_id=ar.owner_account_id AND m.run_id=ar.run_id JOIN journeys j ON j.id=NEW.target_id WHERE ar.run_id=NEW.run_id AND ar.kind='journey' AND ar.source_key=NEW.source_key AND ar.source_sha256=NEW.source_sha256 AND NOT ar.deleted AND ar.owner_account_id=NEW.owner_account_id AND j.owner_account_id=ar.owner_account_id AND j.detail_schema='birdtie.journey-details.v1' AND j.status='draft' AND j.visibility='private') THEN RAISE EXCEPTION 'original Journey owner and archive required' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_journey_binding AFTER INSERT ON legacy_journey_adaptations DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_journey_binding();
CREATE FUNCTION birdtie_legacy_journey_origin_binding() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.origin->>'sourceType' NOT IN('stops','arrangements','notes') OR NOT EXISTS(
  SELECT 1 FROM legacy_journey_adaptations m JOIN legacy_personal_archives ar ON ar.run_id=m.run_id AND ar.kind='journey' AND ar.source_key=m.source_key AND ar.source_sha256=m.source_sha256 AND ar.owner_account_id=m.owner_account_id AND NOT ar.deleted
  JOIN journey_itinerary_entries e ON e.journey_id=m.target_id AND e.entry_id=NEW.entry_id
  WHERE m.run_id=NEW.run_id AND m.source_key=NEW.source_key AND m.target_id=NEW.journey_id AND e.place_id IS NOT DISTINCT FROM NEW.original_place_id
  AND EXISTS(SELECT 1 FROM jsonb_array_elements(ar.payload->(NEW.origin->>'sourceType')) src WHERE src->>'sourceId'=NEW.origin->>'sourceId'
   AND coalesce(src->>'sourcePlaceId','')=coalesce(NEW.origin->>'sourcePlaceId','') AND coalesce(src->>'sourceStopId','')=coalesce(NEW.origin->>'sourceStopId',''))
 ) THEN RAISE EXCEPTION 'exact original Journey entry required' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE CONSTRAINT TRIGGER legacy_journey_origin_binding AFTER INSERT ON legacy_journey_entry_origins DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_journey_origin_binding();
CREATE TRIGGER legacy_journey_run_immutable BEFORE UPDATE OR DELETE ON legacy_journey_adaptation_runs FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_journey_mapping_immutable BEFORE UPDATE OR DELETE ON legacy_journey_adaptations FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE TRIGGER legacy_journey_origin_immutable BEFORE UPDATE OR DELETE ON legacy_journey_entry_origins FOR EACH ROW EXECUTE FUNCTION birdtie_legacy_immutable();
CREATE FUNCTION birdtie_journey_details_contract() RETURNS text LANGUAGE sql IMMUTABLE AS $$SELECT 'journey-native-details-v1'::text$$;
COMMIT;
