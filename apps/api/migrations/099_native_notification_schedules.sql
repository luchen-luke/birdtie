BEGIN;
-- Explicit human schedules only. No AgentRun, outbox event, model permission,
-- inference task, push or real tool is activated by this schema.
CREATE TABLE native_notification_schedules (
 owner_id uuid PRIMARY KEY REFERENCES accounts(id) ON DELETE CASCADE,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 session_id uuid REFERENCES sessions(id) ON DELETE SET NULL,
 version bigint NOT NULL CHECK(version>0),
 settings jsonb NOT NULL CHECK(jsonb_typeof(settings)='object'),
 valid_from timestamptz NOT NULL,
 expires_at timestamptz NOT NULL,
 updated_at timestamptz NOT NULL,
 CHECK(isfinite(valid_from) AND isfinite(expires_at) AND expires_at>valid_from AND expires_at<=valid_from+interval '720 hours')
);
CREATE FUNCTION birdtie_guard_notification_schedule() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE n timestamptz:=clock_timestamp(); q jsonb; c text; seen text[]:='{}';
BEGIN
 IF TG_OP='UPDATE' AND NEW.session_id IS NULL AND OLD.session_id IS NOT NULL
 AND ROW(NEW.owner_id,NEW.agent_id,NEW.version,NEW.settings,NEW.valid_from,NEW.expires_at,NEW.updated_at)
 IS NOT DISTINCT FROM ROW(OLD.owner_id,OLD.agent_id,OLD.version,OLD.settings,OLD.valid_from,OLD.expires_at,OLD.updated_at)
 THEN RETURN NEW;END IF; -- native Session deletion stops the plan, preserves budgets
 IF TG_OP='UPDATE' AND (NEW.owner_id<>OLD.owner_id OR NEW.agent_id<>OLD.agent_id OR NEW.version<>OLD.version+1)
 OR TG_OP='INSERT' AND NEW.version<>1 THEN RAISE EXCEPTION 'schedule version changed' USING ERRCODE='23514';END IF;
 IF NOT EXISTS(SELECT 1 FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.account_id=a.id AND ss.id=NEW.session_id
 WHERE a.id=NEW.owner_id AND a.account_type='person' AND a.status='active' AND ag.id=NEW.agent_id
 AND ag.agent_type='personal' AND ag.status='active' AND ss.revoked_at IS NULL AND ss.expires_at>n AND ss.idle_expires_at>n)
 THEN RAISE EXCEPTION 'schedule identity unavailable' USING ERRCODE='23514';END IF;
 IF (SELECT count(*) FROM jsonb_object_keys(NEW.settings))<>8 OR NOT NEW.settings ?& ARRAY['enabled','timeZone','localMinute','gapPolicy','foldPolicy','quiet','maxContactsPerDay','categories']
 OR jsonb_typeof(NEW.settings->'enabled')<>'boolean' OR jsonb_typeof(NEW.settings->'timeZone')<>'string'
 OR NOT EXISTS(SELECT 1 FROM pg_timezone_names WHERE name=NEW.settings->>'timeZone')
 OR NEW.settings->>'timeZone'='Local' OR NEW.settings->>'gapPolicy'<>'SKIP' OR NEW.settings->>'foldPolicy'<>'EARLIER_ONCE'
 OR jsonb_typeof(NEW.settings->'localMinute')<>'number' OR (NEW.settings->>'localMinute')!~'^(0|[1-9][0-9]*)$'
 OR (NEW.settings->>'localMinute')::int NOT BETWEEN 0 AND 1439
 OR jsonb_typeof(NEW.settings->'maxContactsPerDay')<>'number' OR (NEW.settings->>'maxContactsPerDay')!~'^(0|[1-9][0-9]*)$'
 OR (NEW.settings->>'maxContactsPerDay')::int NOT BETWEEN 0 AND 20
 OR jsonb_typeof(NEW.settings->'categories')<>'array' OR jsonb_array_length(NEW.settings->'categories') NOT BETWEEN 1 AND 8
 THEN RAISE EXCEPTION 'schedule settings invalid' USING ERRCODE='23514';END IF;
 FOR c IN SELECT jsonb_array_elements_text(NEW.settings->'categories') LOOP
 IF c NOT IN ('MESSAGE','ACTIVITY','COMMUNITY','ORGANIZATION','BUSINESS','SYSTEM','AGENT','SOCIAL') OR c=ANY(seen)
 THEN RAISE EXCEPTION 'schedule categories invalid' USING ERRCODE='23514';END IF;seen:=array_append(seen,c);END LOOP;
 q:=NEW.settings->'quiet';
 IF q<>'null'::jsonb AND (jsonb_typeof(q)<>'object' OR (SELECT count(*) FROM jsonb_object_keys(q))<>2 OR NOT q ?& ARRAY['startMinute','endMinute']
 OR jsonb_typeof(q->'startMinute')<>'number' OR jsonb_typeof(q->'endMinute')<>'number'
 OR (q->>'startMinute')!~'^(0|[1-9][0-9]*)$' OR (q->>'endMinute')!~'^(0|[1-9][0-9]*)$'
 OR (q->>'startMinute')::int NOT BETWEEN 0 AND 1439 OR (q->>'endMinute')::int NOT BETWEEN 0 AND 1439
 OR (q->>'startMinute')::int=(q->>'endMinute')::int) THEN RAISE EXCEPTION 'quiet window invalid' USING ERRCODE='23514';END IF;
 NEW.valid_from:=n;NEW.updated_at:=n;
 IF NEW.expires_at<=n OR NEW.expires_at>n+interval '720 hours' THEN RAISE EXCEPTION 'schedule expired' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_notification_schedule_guard BEFORE INSERT OR UPDATE ON native_notification_schedules FOR EACH ROW EXECUTE FUNCTION birdtie_guard_notification_schedule();

CREATE TABLE native_notification_schedule_slots (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_id uuid NOT NULL REFERENCES native_notification_schedules(owner_id) ON DELETE CASCADE,
 agent_id uuid NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
 session_id uuid NOT NULL,
 schedule_version bigint NOT NULL CHECK(schedule_version>0),
 schedule_token text NOT NULL CHECK(schedule_token~'^[0-9]+$'),
 local_date date NOT NULL,
 due_at timestamptz NOT NULL CHECK(isfinite(due_at)),
 state text NOT NULL DEFAULT 'CLAIMED' CHECK(state IN ('CLAIMED','COMPLETED','EMPTY','BUDGET_FULL')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_id,local_date)
);
CREATE TABLE native_notification_schedule_deliveries (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 slot_id uuid REFERENCES native_notification_schedule_slots(id) ON DELETE SET NULL,
 -- Deletion of a domain source/Inbox must not restore rolling touch allowance.
 decision_id uuid UNIQUE REFERENCES native_notification_decisions(id) ON DELETE SET NULL,
 inbox_item_id uuid UNIQUE REFERENCES inbox_items(id) ON DELETE SET NULL,
 delivered boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp()
);
CREATE INDEX native_notification_schedule_budget ON native_notification_schedule_deliveries(owner_id,created_at) WHERE delivered;

CREATE FUNCTION birdtie_notification_schedule_quiet(settings jsonb,n timestamptz) RETURNS boolean LANGUAGE sql STABLE AS $$
 SELECT CASE WHEN settings->'quiet'='null'::jsonb THEN false
 WHEN (settings->'quiet'->>'startMinute')::int < (settings->'quiet'->>'endMinute')::int THEN
 (EXTRACT(hour FROM n AT TIME ZONE (settings->>'timeZone'))::int*60+EXTRACT(minute FROM n AT TIME ZONE (settings->>'timeZone'))::int)
 >=(settings->'quiet'->>'startMinute')::int AND
 (EXTRACT(hour FROM n AT TIME ZONE (settings->>'timeZone'))::int*60+EXTRACT(minute FROM n AT TIME ZONE (settings->>'timeZone'))::int)
 <(settings->'quiet'->>'endMinute')::int
 ELSE (EXTRACT(hour FROM n AT TIME ZONE (settings->>'timeZone'))::int*60+EXTRACT(minute FROM n AT TIME ZONE (settings->>'timeZone'))::int)
 >=(settings->'quiet'->>'startMinute')::int OR
 (EXTRACT(hour FROM n AT TIME ZONE (settings->>'timeZone'))::int*60+EXTRACT(minute FROM n AT TIME ZONE (settings->>'timeZone'))::int)
 <(settings->'quiet'->>'endMinute')::int END
$$;
CREATE FUNCTION birdtie_notification_schedule_current(slot native_notification_schedule_slots,dev boolean) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT EXISTS(SELECT 1 FROM native_notification_schedules p CROSS JOIN stamp
 JOIN accounts a ON a.id=p.owner_id AND a.account_type='person' AND a.status='active'
 JOIN agents ag ON ag.id=p.agent_id AND ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON'
 JOIN sessions ss ON ss.id=p.session_id AND ss.account_id=a.id
 WHERE p.owner_id=slot.owner_id AND p.agent_id=slot.agent_id AND p.session_id=slot.session_id
 AND p.version=slot.schedule_version AND p.xmin::text=slot.schedule_token
 AND (p.settings->>'enabled')::boolean AND (p.settings->>'maxContactsPerDay')::int>0
 AND p.valid_from<=slot.due_at AND slot.due_at<=stamp.n AND p.expires_at>stamp.n
 AND (stamp.n AT TIME ZONE (p.settings->>'timeZone'))::date=slot.local_date
 AND (slot.due_at AT TIME ZONE (p.settings->>'timeZone'))::date=slot.local_date
 AND EXTRACT(hour FROM slot.due_at AT TIME ZONE (p.settings->>'timeZone'))::int*60
 +EXTRACT(minute FROM slot.due_at AT TIME ZONE (p.settings->>'timeZone'))::int=(p.settings->>'localMinute')::int
 AND NOT birdtie_notification_schedule_quiet(p.settings,stamp.n) AND NOT birdtie_notification_schedule_quiet(p.settings,slot.due_at)
 AND ss.revoked_at IS NULL AND ss.expires_at>stamp.n AND ss.idle_expires_at>stamp.n
 AND (dev OR ss.authentication_method<>'dev_phone'))
$$;
CREATE FUNCTION birdtie_notification_schedule_wall_time(settings jsonb,local_day date) RETURNS timestamptz LANGUAGE sql STABLE AS $$
 WITH wall AS (SELECT local_day::timestamp+make_interval(mins=>(settings->>'localMinute')::int) AS n),
 candidates AS(SELECT (wall.n AT TIME ZONE 'UTC')-((sample AT TIME ZONE (settings->>'timeZone'))-(sample AT TIME ZONE 'UTC')) AS candidate,wall.n
 FROM wall CROSS JOIN LATERAL generate_series((wall.n AT TIME ZONE 'UTC')-interval '36 hours',(wall.n AT TIME ZONE 'UTC')+interval '36 hours',interval '1 hour') sample)
 SELECT min(candidate) FROM candidates WHERE candidate AT TIME ZONE (settings->>'timeZone')=n
$$;
CREATE FUNCTION birdtie_notification_digest_current(d native_notification_decisions) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT d.disposition='DIGEST' AND EXISTS(SELECT 1 FROM birdtie_native_notification_source(d.kind,d.source_id,d.recipient_id) s
 CROSS JOIN birdtie_native_notification_preference(d.recipient_id,d.category) p
 WHERE s.actor_id=d.actor_id AND s.event_version=d.event_version AND s.source_version=d.source_version
 AND p.version=d.policy_version AND p.disposition=d.disposition AND p.priority=d.priority AND p.reason=d.reason)
$$;
CREATE FUNCTION birdtie_notification_schedule_delivery_allowed(sid uuid,d native_notification_decisions,dev boolean) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 WITH stamp AS MATERIALIZED(SELECT clock_timestamp() n)
 SELECT birdtie_notification_digest_current(d) AND EXISTS(
 SELECT 1 FROM native_notification_schedule_slots slot JOIN native_notification_schedules p ON p.owner_id=slot.owner_id CROSS JOIN stamp
 WHERE slot.id=sid AND slot.owner_id=d.recipient_id AND slot.state='CLAIMED' AND birdtie_notification_schedule_current(slot,dev)
 AND (p.settings->'categories') ? d.category
 AND NOT EXISTS(SELECT 1 FROM native_notification_schedule_deliveries done WHERE done.decision_id=d.id)
 AND (SELECT count(*) FROM native_notification_schedule_deliveries old WHERE old.owner_id=p.owner_id AND old.delivered AND old.created_at>stamp.n-interval '24 hours')
 <(p.settings->>'maxContactsPerDay')::int)
$$;
CREATE FUNCTION birdtie_guard_notification_schedule_slot() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE p native_notification_schedules; first_at timestamptz;
BEGIN
 IF TG_OP='UPDATE' THEN
 IF NEW.owner_id<>OLD.owner_id OR NEW.agent_id<>OLD.agent_id OR NEW.session_id<>OLD.session_id OR NEW.schedule_version<>OLD.schedule_version
 OR NEW.schedule_token<>OLD.schedule_token OR NEW.local_date<>OLD.local_date OR NEW.due_at<>OLD.due_at OR NEW.created_at<>OLD.created_at
 OR OLD.state<>'CLAIMED' OR NEW.state NOT IN ('COMPLETED','EMPTY','BUDGET_FULL') THEN RAISE EXCEPTION 'slot immutable' USING ERRCODE='23514';END IF;
 ELSE
 IF NEW.state<>'CLAIMED' THEN RAISE EXCEPTION 'slot initial state invalid' USING ERRCODE='23514';END IF;NEW.created_at:=clock_timestamp();
 END IF;
 -- Both Go and PG must agree on the exact earliest wall time. A gap has no
 -- candidate; a repeated wall time picks min UTC, regardless of time.Date.
 SELECT * INTO p FROM native_notification_schedules WHERE owner_id=NEW.owner_id;
 first_at:=birdtie_notification_schedule_wall_time(p.settings,NEW.local_date);
 IF first_at IS NULL OR first_at<>NEW.due_at OR NOT birdtie_notification_schedule_current(NEW,true)
 THEN RAISE EXCEPTION 'slot source changed' USING ERRCODE='23514';END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER native_notification_schedule_slot_guard BEFORE INSERT OR UPDATE ON native_notification_schedule_slots FOR EACH ROW EXECUTE FUNCTION birdtie_guard_notification_schedule_slot();
CREATE FUNCTION birdtie_guard_notification_schedule_delivery() RETURNS trigger LANGUAGE plpgsql AS $$
DECLARE d native_notification_decisions; slot native_notification_schedule_slots;
BEGIN
 IF TG_OP='UPDATE' THEN
 IF NEW.owner_id<>OLD.owner_id OR (NEW.slot_id IS DISTINCT FROM OLD.slot_id AND NEW.slot_id IS NOT NULL) OR NEW.delivered<>OLD.delivered OR NEW.created_at<>OLD.created_at
 OR (NEW.decision_id IS DISTINCT FROM OLD.decision_id AND NEW.decision_id IS NOT NULL)
 OR (NEW.inbox_item_id IS DISTINCT FROM OLD.inbox_item_id AND NEW.inbox_item_id IS NOT NULL)
 THEN RAISE EXCEPTION 'delivery immutable' USING ERRCODE='23514';END IF;RETURN NEW;
 END IF;
 SELECT * INTO d FROM native_notification_decisions WHERE id=NEW.decision_id;
 SELECT * INTO slot FROM native_notification_schedule_slots WHERE id=NEW.slot_id;
 IF d.id IS NULL OR slot.id IS NULL OR NEW.owner_id<>d.recipient_id OR NEW.owner_id<>slot.owner_id
 OR NOT birdtie_notification_schedule_delivery_allowed(slot.id,d,true)
 OR NEW.delivered IS DISTINCT FROM EXISTS(SELECT 1 FROM inbox_items i WHERE i.id=NEW.inbox_item_id AND i.recipient_account_id=NEW.owner_id AND i.routing_decision_id=d.id)
 THEN RAISE EXCEPTION 'delivery current source unavailable' USING ERRCODE='23514';END IF;
 NEW.created_at:=clock_timestamp();RETURN NEW;
END $$;
CREATE TRIGGER native_notification_schedule_delivery_guard BEFORE INSERT OR UPDATE ON native_notification_schedule_deliveries FOR EACH ROW EXECUTE FUNCTION birdtie_guard_notification_schedule_delivery();

-- Preserve all pre099 NORMAL/IMMEDIATE behavior through the exact old function.
ALTER FUNCTION birdtie_native_notification_visible(native_notification_decisions) RENAME TO birdtie_native_notification_visible_v098;
CREATE FUNCTION birdtie_native_notification_visible(d native_notification_decisions) RETURNS boolean LANGUAGE sql VOLATILE AS $$
 SELECT birdtie_native_notification_visible_v098(d) OR (birdtie_notification_digest_current(d) AND EXISTS(
 SELECT 1 FROM native_notification_schedule_deliveries receipt JOIN native_notification_schedule_slots slot ON slot.id=receipt.slot_id
 JOIN native_notification_schedules p ON p.owner_id=slot.owner_id JOIN sessions ss ON ss.id=p.session_id
 JOIN agents ag ON ag.id=p.agent_id AND ag.principal_account_id=p.owner_id AND ag.agent_type='personal' AND ag.status='active'
 JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=p.owner_id AND ap.owner_type='PERSON'
 WHERE receipt.decision_id=d.id AND receipt.owner_id=d.recipient_id AND receipt.delivered AND slot.state='COMPLETED'
 AND p.version=slot.schedule_version AND p.xmin::text=slot.schedule_token AND (p.settings->>'enabled')::boolean
 AND p.expires_at>clock_timestamp() AND ss.account_id=p.owner_id AND ss.revoked_at IS NULL AND ss.expires_at>clock_timestamp() AND ss.idle_expires_at>clock_timestamp()))
$$;
COMMIT;
