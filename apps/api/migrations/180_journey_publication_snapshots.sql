BEGIN;
-- Rich schedules retain the migration-158 private/draft boundary. This is an
-- allowlisted public version of the same Journey, not another approval ledger.
CREATE TABLE journey_publication_snapshots (
 journey_id uuid NOT NULL REFERENCES journeys(id),
 publication_revision bigint NOT NULL CHECK(publication_revision>1),
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 source_revision bigint NOT NULL CHECK(source_revision>0 AND publication_revision=source_revision+1),
 operation_id uuid NOT NULL,
 published_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 160),
 summary text NOT NULL CHECK(length(summary)<=5000),
 stops jsonb NOT NULL CHECK(jsonb_typeof(stops)='array' AND jsonb_array_length(stops) BETWEEN 1 AND 50),
 PRIMARY KEY(journey_id,publication_revision),
 UNIQUE(journey_id,publication_revision,owner_account_id),
 FOREIGN KEY(owner_account_id,operation_id) REFERENCES personal_collection_publication_operations(owner_account_id,operation_id) DEFERRABLE INITIALLY DEFERRED
);
CREATE TRIGGER journey_publication_snapshot_immutable BEFORE UPDATE OR DELETE ON journey_publication_snapshots FOR EACH ROW EXECUTE FUNCTION birdtie_collection_publication_operation_immutable();
CREATE TABLE journey_publication_current (
 journey_id uuid PRIMARY KEY REFERENCES journeys(id),
 publication_revision bigint NOT NULL,
 owner_account_id uuid NOT NULL REFERENCES accounts(id),
 FOREIGN KEY(journey_id,publication_revision,owner_account_id) REFERENCES journey_publication_snapshots(journey_id,publication_revision,owner_account_id)
);
CREATE INDEX journey_publication_discovery ON journey_publication_snapshots(published_at DESC,journey_id DESC);
CREATE FUNCTION birdtie_journey_publication_snapshot_guard() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE j journeys%ROWTYPE;op personal_collection_publication_operations%ROWTYPE;s jsonb;lastpos integer:=0;ids text[]:=ARRAY[]::text[];
BEGIN
 SELECT * INTO j FROM journeys WHERE id=NEW.journey_id;
 SELECT * INTO op FROM personal_collection_publication_operations WHERE owner_account_id=NEW.owner_account_id AND operation_id=NEW.operation_id;
 IF j.owner_account_id IS DISTINCT FROM NEW.owner_account_id OR j.revision IS DISTINCT FROM NEW.publication_revision OR j.detail_schema IS DISTINCT FROM 'birdtie.journey-details.v1' OR j.visibility<>'private' OR j.status<>'draft'
 OR op.action IS DISTINCT FROM 'PUBLISH' OR op.outcome IS DISTINCT FROM 'COMMITTED' OR op.kind IS DISTINCT FROM 'journey' OR op.target_id IS DISTINCT FROM NEW.journey_id OR op.expected_revision IS DISTINCT FROM NEW.source_revision OR op.result_revision IS DISTINCT FROM NEW.publication_revision THEN
  RAISE EXCEPTION 'Journey publication requires original current owner and operation' USING ERRCODE='23514';
 END IF;
 FOR s IN SELECT value FROM jsonb_array_elements(NEW.stops) LOOP
  IF jsonb_typeof(s)<>'object' OR NOT s ?& ARRAY['entryId','position','title','note','locationVisibility','placeId','momentId','momentRevision','commentMomentId'] OR s - ARRAY['entryId','position','title','note','locationVisibility','placeId','momentId','momentRevision','commentMomentId']<>'{}'::jsonb
  OR (s->>'position')::integer<=lastpos OR (s->>'position')::integer>500 OR s->>'entryId'=ANY(ids) OR length(s->>'title')>160 OR length(s->>'note')>2000 OR s->>'locationVisibility' NOT IN('hidden','city','place')
  OR NOT EXISTS(SELECT 1 FROM journey_itinerary_entries e WHERE e.journey_id=NEW.journey_id AND e.entry_id=(s->>'entryId')::uuid AND e.sequence_no=(s->>'position')::integer AND e.entry_type='STOP' AND coalesce(e.place_id::text,'')=s->>'placeId')
  OR (s->>'momentId'='' AND ((s->>'momentRevision')::bigint<>0 OR s->>'commentMomentId'<>'')) OR(s->>'momentId'<>'' AND((s->>'momentRevision')::bigint<1 OR(s->>'commentMomentId'<>'' AND s->>'commentMomentId'<>s->>'momentId'))) THEN
   RAISE EXCEPTION 'Journey publication contains unapproved schedule material' USING ERRCODE='23514';
  END IF;
  lastpos:=(s->>'position')::integer;ids:=array_append(ids,s->>'entryId');
 END LOOP;
 RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journey_publication_snapshot_guard AFTER INSERT ON journey_publication_snapshots DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_journey_publication_snapshot_guard();
CREATE FUNCTION birdtie_journey_publication_current_guard() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN
 IF NOT EXISTS(SELECT 1 FROM journeys j JOIN journey_publication_snapshots s ON s.journey_id=j.id AND s.owner_account_id=j.owner_account_id WHERE j.id=NEW.journey_id AND j.owner_account_id=NEW.owner_account_id AND j.revision=NEW.publication_revision AND s.publication_revision=NEW.publication_revision AND j.visibility='private' AND j.status='draft' AND j.detail_schema='birdtie.journey-details.v1')THEN RAISE EXCEPTION 'current Journey publication requires the newly committed version' USING ERRCODE='23514';END IF;RETURN NULL;
END $$;
CREATE CONSTRAINT TRIGGER journey_publication_current_guard AFTER INSERT OR UPDATE ON journey_publication_current DEFERRABLE INITIALLY DEFERRED FOR EACH ROW EXECUTE FUNCTION birdtie_journey_publication_current_guard();

-- Date classification is evaluated by the server in a single known schedule
-- timezone. Conflicting/missing cities or incomplete dates remain UNKNOWN.
CREATE FUNCTION birdtie_journey_lifecycle(jid uuid,at_time timestamptz) RETURNS jsonb LANGUAGE sql STABLE AS $$
 WITH j AS(SELECT * FROM journeys WHERE id=jid),zones AS(
 SELECT DISTINCT c.time_zone FROM j JOIN cities c ON c.id=j.city_id
 UNION SELECT DISTINCT c.time_zone FROM j JOIN journey_itinerary_entries e ON e.journey_id=j.id JOIN cities c ON c.id=e.city_id WHERE j.city_id IS NULL AND e.entry_type='STOP'),
 z AS(SELECT CASE WHEN count(*)=1 THEN min(time_zone) ELSE '' END tz FROM zones),v AS(SELECT j.*,z.tz,CASE WHEN z.tz<>'' THEN(at_time AT TIME ZONE z.tz)::date END today FROM j CROSS JOIN z)
 SELECT jsonb_build_object('state',CASE WHEN journey_start_date IS NULL OR journey_end_date IS NULL OR today IS NULL THEN 'UNKNOWN' WHEN today<journey_start_date THEN 'NOT_STARTED' WHEN today>journey_end_date THEN 'ENDED' ELSE 'IN_PROGRESS' END,
 'timeZone',tz,'asOfDate',coalesce(today::text,''),'startDate',coalesce(journey_start_date::text,''),'endDate',coalesce(journey_end_date::text,'')) FROM v
$$;
COMMIT;
