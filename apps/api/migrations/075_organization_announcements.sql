BEGIN;
CREATE TABLE organization_announcements (
 id uuid PRIMARY KEY CHECK(id<>'00000000-0000-0000-0000-000000000000'),
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 organization_account_id uuid NOT NULL REFERENCES accounts(id),
 revision bigint NOT NULL CHECK(revision>0),
 title text NOT NULL CHECK(length(btrim(title)) BETWEEN 1 AND 160 AND octet_length(title)<=480),
 body text NOT NULL CHECK(length(btrim(body))>0 AND octet_length(body)<=6000),
 audience text NOT NULL DEFAULT 'PUBLIC' CHECK(audience='PUBLIC'),
 state text NOT NULL CHECK(state IN ('DRAFT','PUBLISHED','WITHDRAWN')),
 valid_until timestamptz NOT NULL CHECK(isfinite(valid_until)),
 created_by uuid NOT NULL REFERENCES accounts(id), updated_by uuid NOT NULL REFERENCES accounts(id),
 created_at timestamptz NOT NULL CHECK(isfinite(created_at)), updated_at timestamptz NOT NULL CHECK(isfinite(updated_at)),
 published_at timestamptz, withdrawn_at timestamptz, publication_context jsonb,
 CHECK(updated_at>=created_at AND valid_until>created_at),
 CHECK(coalesce((state='DRAFT' AND published_at IS NULL AND withdrawn_at IS NULL AND publication_context IS NULL)
 OR (state='PUBLISHED' AND published_at IS NOT NULL AND isfinite(published_at) AND published_at BETWEEN created_at AND updated_at
 AND valid_until>published_at AND withdrawn_at IS NULL AND jsonb_typeof(publication_context)='object')
 OR (state='WITHDRAWN' AND withdrawn_at IS NOT NULL AND isfinite(withdrawn_at) AND withdrawn_at BETWEEN created_at AND updated_at),false))
);
CREATE INDEX organization_announcements_org ON organization_announcements(organization_id,updated_at DESC,id);
CREATE FUNCTION birdtie_organization_announcement_guard() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP='DELETE' THEN
  IF EXISTS(SELECT 1 FROM organizations WHERE id=OLD.organization_id) THEN RAISE EXCEPTION 'withdraw announcement; preserve history'; END IF;
  RETURN OLD;
 END IF;
 IF NOT EXISTS(SELECT 1 FROM organizations o JOIN accounts a ON a.id=o.account_id WHERE o.id=NEW.organization_id AND o.account_id=NEW.organization_account_id AND a.account_type='organization')
 OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.created_by AND account_type='person')
 OR NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.updated_by AND account_type='person') THEN RAISE EXCEPTION 'announcement principal binding'; END IF;
 IF TG_OP='INSERT' THEN
  IF NEW.revision<>1 OR NEW.state<>'DRAFT' THEN RAISE EXCEPTION 'announcement starts as draft'; END IF;
 ELSE
  IF NEW.id<>OLD.id OR NEW.organization_id<>OLD.organization_id OR NEW.organization_account_id<>OLD.organization_account_id
   OR NEW.created_by<>OLD.created_by OR NEW.created_at<>OLD.created_at OR OLD.state='WITHDRAWN'
   OR NEW.revision<>OLD.revision+1 OR NEW.updated_at<OLD.updated_at THEN RAISE EXCEPTION 'announcement stable revision'; END IF;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_announcement_guard BEFORE INSERT OR UPDATE OR DELETE ON organization_announcements FOR EACH ROW EXECUTE FUNCTION birdtie_organization_announcement_guard();
CREATE TABLE organization_announcement_previews (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 announcement_id uuid NOT NULL REFERENCES organization_announcements(id) ON DELETE CASCADE,
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 organization_account_id uuid NOT NULL REFERENCES accounts(id),
 actor_id uuid NOT NULL REFERENCES accounts(id), session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
 announcement_revision bigint NOT NULL CHECK(announcement_revision>0),
 source_snapshot text NOT NULL CHECK(source_snapshot ~ '^[0-9a-f]{64}$'),
 authority_snapshot text NOT NULL CHECK(authority_snapshot ~ '^[0-9a-f]{64}$'),
 created_at timestamptz NOT NULL CHECK(isfinite(created_at)), expires_at timestamptz NOT NULL CHECK(isfinite(expires_at)),
 consumed_at timestamptz, published_revision bigint,
 CHECK(expires_at>created_at AND expires_at<=created_at+interval '10 minutes'),
 CHECK((consumed_at IS NULL AND published_revision IS NULL) OR (consumed_at IS NOT NULL AND isfinite(consumed_at) AND consumed_at>=created_at AND published_revision=announcement_revision+1))
);
CREATE TABLE organization_announcement_audit (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 announcement_id uuid NOT NULL REFERENCES organization_announcements(id) ON DELETE CASCADE,
 actor_id uuid NOT NULL REFERENCES accounts(id),
 action text NOT NULL CHECK(action IN ('DRAFT','PUBLISH','WITHDRAW')),
 revision bigint NOT NULL CHECK(revision>0), created_at timestamptz NOT NULL CHECK(isfinite(created_at)),
 UNIQUE(announcement_id,action,revision)
);
CREATE TABLE organization_announcement_evidence_controls (
 evidence_id uuid PRIMARY KEY REFERENCES agent_memory_evidence(id) ON DELETE CASCADE,
 source_kind text NOT NULL CHECK(source_kind='ORGANIZATION_ANNOUNCEMENT')
);
CREATE FUNCTION birdtie_organization_announcement_evidence_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.owner_type='ORGANIZATION' AND NEW.source_type='ORGANIZATION_ANNOUNCEMENT' THEN
  INSERT INTO organization_announcement_evidence_controls(evidence_id,source_kind) VALUES(NEW.id,NEW.source_type) ON CONFLICT DO NOTHING;
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER organization_announcement_evidence_control AFTER INSERT OR UPDATE ON agent_memory_evidence FOR EACH ROW EXECUTE FUNCTION birdtie_organization_announcement_evidence_control();
-- 073 PERSON branch remains exact; only Organization's native revision sources grow.
LOCK TABLE agent_memory_evidence IN ACCESS EXCLUSIVE MODE;
ALTER TABLE agent_memory_evidence DROP CONSTRAINT memory_evidence_shape;
ALTER TABLE agent_memory_evidence ADD CONSTRAINT memory_evidence_shape CHECK (
 (owner_type='PERSON' AND (coalesce((
      (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
        AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
      OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
        AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
        AND ((source_type='MOMENT' AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL)
          OR (source_type='ACTIVITY_PARTICIPATION' AND source_version_kind='UPDATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')
          OR (source_type='SAVED_PLACE' AND source_version_kind='CREATED_AT_DIGEST' AND source_revision IS NULL
            AND source_token ~ '^[0-9a-f]{64}$')))
    ),false))) OR
 (owner_type='ORGANIZATION' AND (coalesce((
 (status='REMOVED' AND source_type IS NULL AND source_id IS NULL AND source_version_kind IS NULL
 AND source_revision IS NULL AND source_token IS NULL AND signal_type IS NULL AND weight=0 AND event_time IS NULL)
 OR (status='CURRENT' AND source_id IS NOT NULL AND source_id<>'00000000-0000-0000-0000-000000000000'::uuid
 AND signal_type='MANUAL_REFERENCE' AND weight=1 AND event_time IS NOT NULL
 AND ((source_type IN ('ORGANIZATION_ACTIVITY','ORGANIZATION_ADMIN_INPUT','ORGANIZATION_ANNOUNCEMENT') AND source_version_kind='REVISION' AND source_revision>0 AND source_token IS NULL
 AND (source_type<>'ORGANIZATION_ADMIN_INPUT' OR source_id<>memory_id))
 OR (source_type IN ('ORGANIZATION_PROFILE','ORGANIZATION_PUBLIC_FAQ') AND source_version_kind='UPDATED_AT_DIGEST'
 AND source_revision IS NULL AND source_token ~ '^[0-9a-f]{64}$')))
),false))));
COMMIT;
