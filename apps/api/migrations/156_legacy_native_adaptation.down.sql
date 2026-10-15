BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM legacy_native_adaptation_runs) OR EXISTS(SELECT 1 FROM moments WHERE city_id IS NULL OR title='' OR(status='published' AND location_precision='none')) THEN
  RAISE EXCEPTION 'native converted data and provenance must be retained; rollback is unavailable';
 END IF;
END $$;
DROP TABLE legacy_native_adaptation_residuals;
DROP TABLE legacy_native_catalog_aliases;
DROP TABLE legacy_native_adaptation_entities;
DROP FUNCTION birdtie_legacy_native_target_binding();
DROP TABLE legacy_native_adaptation_runs;
DROP TRIGGER moment_publishable_content ON moments;
DROP FUNCTION birdtie_check_moment_publishable_content();
CREATE OR REPLACE FUNCTION birdtie_guard_human_moment_public_control() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_OP<>'INSERT' THEN RAISE EXCEPTION 'immutable Moment operation';END IF;
 PERFORM pg_advisory_xact_lock(hashtextextended(NEW.actor_account_id::text||':MOMENT:'||NEW.operation_id::text,144));
 IF NOT EXISTS(SELECT 1 FROM accounts WHERE id=NEW.actor_account_id AND account_type='person' AND status='active') THEN RAISE EXCEPTION 'current Person required';END IF;
 IF NEW.status='SUCCEEDED' AND NOT EXISTS(SELECT 1 FROM moments m WHERE m.id=NEW.moment_id AND m.author_account_id=NEW.actor_account_id AND m.revision=NEW.applied_revision AND((NEW.action IN('CREATE','UPDATE') AND m.status='draft' AND m.visibility='private') OR(NEW.action='WITHDRAW' AND m.status='withdrawn' AND m.visibility='private') OR(NEW.action IN('PUBLISH','UPDATE_PUBLIC') AND m.status='published' AND m.visibility='public' AND ((m.place_id IS NOT NULL AND m.location_precision='place') OR(m.place_id IS NULL AND m.location_precision='city')) AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at))) THEN RAISE EXCEPTION 'canonical owned Moment effect required';END IF;
 RETURN NEW;
END $$;
CREATE OR REPLACE FUNCTION birdtie_moment_public_binding(mid uuid,viewer uuid,read_at timestamptz) RETURNS text LANGUAGE sql VOLATILE AS $$
 SELECT encode(sha256(convert_to(jsonb_build_array(m.xmin::text,a.xmin::text,c.xmin::text,source.binding,
 (SELECT account.xmin::text FROM accounts account WHERE account.id=viewer),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb) FROM person_block_control_epochs ep WHERE viewer IS NOT NULL AND((ep.owner_account_id=viewer AND ep.target_account_id=a.id) OR(ep.owner_account_id=a.id AND ep.target_account_id=viewer))))::text,'UTF8')),'hex')
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN cities c ON c.id=m.city_id
 LEFT JOIN LATERAL birdtie_public_moment_photo_source(m.id,read_at) source ON true
 WHERE m.id=mid AND m.status='published' AND m.visibility='public' AND m.revision>=2
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=read_at
 AND a.account_type='person' AND a.status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>read_at)
 AND((m.place_id IS NULL AND m.location_precision='city') OR(m.place_id IS NOT NULL AND m.location_precision='place' AND source.binding IS NOT NULL))
 AND(viewer IS NULL OR EXISTS(SELECT 1 FROM accounts account WHERE account.id=viewer AND account.account_type='person' AND account.status='active'))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE viewer IS NOT NULL AND((b.blocker_account_id=viewer AND b.blocked_account_id=a.id) OR(b.blocker_account_id=a.id AND b.blocked_account_id=viewer)))
$$;
CREATE OR REPLACE FUNCTION birdtie_public_moment_photo_source(moment uuid,read_at timestamptz)
RETURNS TABLE(binding text,deadline timestamptz) LANGUAGE sql STABLE AS $$
 SELECT jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,jsonb_build_array(
 (SELECT coalesce(jsonb_agg(jsonb_build_array(src.candidate_id,src.xmin::text,src.permission_id) ORDER BY src.candidate_id),'[]'::jsonb) FROM place_sources src WHERE src.place_id=p.id AND src.permission_id IS NOT NULL),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(f.id,f.xmin::text,rights.xmin::text,origin.xmin::text) ORDER BY f.id),'[]'::jsonb) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates')))::text)::text,
 least(c.expires_at,least(p.expires_at,
 (SELECT min(least(f.valid_to,f.refresh_due,rights.retention_until)) FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND f.field_key IN('name','address','coordinates') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL AND rights.version=f.permission_version
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND rights.retention_until>cl.at AND f.conflict_state='NONE')))
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id JOIN cities c ON c.id=m.city_id
 CROSS JOIN(SELECT read_at at) cl
 WHERE m.id=moment AND m.revision>=2 AND m.status='published' AND m.visibility='public' AND m.location_precision='place'
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
 AND a.account_type='person' AND a.status='active' AND p.publication_status='published' AND c.publication_status='published'
 AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND(c.expires_at IS NULL OR c.expires_at>cl.at) AND (
 NOT EXISTS(SELECT 1 FROM place_sources managed_source WHERE managed_source.place_id=p.id AND managed_source.permission_id IS NOT NULL)
 OR (
 EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='name' AND f.value=to_jsonb(p.name))
 AND (coalesce(p.address_label,'')='' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='address' AND f.value=to_jsonb(p.address_label)))
 AND (p.location_precision<>'point' OR EXISTS(SELECT 1 FROM place_field_evidence f JOIN place_source_permissions rights ON rights.id=f.permission_id JOIN place_candidates origin ON origin.id=f.source_candidate_id
 WHERE f.place_id=p.id AND rights.source_url=p.source_ref AND origin.permission_id=rights.id AND origin.status IN('published','linked_duplicate') AND rights.state='CONFIRMED' AND rights.revoked_at IS NULL
 AND rights.version=f.permission_version AND rights.reviewed_by IS NOT NULL AND rights.reviewed_by<>rights.declared_by AND rights.reviewed_at<=cl.at AND rights.retention_until>cl.at
 AND f.valid_from<=cl.at AND f.valid_to>cl.at AND f.refresh_due>cl.at AND f.conflict_state='NONE' AND f.observed_at<=cl.at AND f.verified_at<=cl.at
 AND f.field_key='coordinates' AND f.value=jsonb_build_object('coordinateSystem',p.coordinate_system,'latitude',p.latitude,'longitude',p.longitude)))
 ))
$$;

DROP FUNCTION birdtie_moment_has_publishable_content(uuid,timestamptz);
DROP FUNCTION birdtie_moment_media_parent_raw(uuid);
DROP FUNCTION birdtie_moment_media_rights_current(jsonb,timestamptz);
DROP FUNCTION birdtie_optional_moment_content_contract();
ALTER TABLE moments DROP CONSTRAINT moment_optional_geography;
ALTER TABLE moments DROP CONSTRAINT moment_optional_place_fk;
ALTER TABLE moments DROP CONSTRAINT moments_title_check;
ALTER TABLE moments ADD CONSTRAINT moments_title_check CHECK(length(title) BETWEEN 1 AND 160);
ALTER TABLE moments ALTER COLUMN city_id SET NOT NULL;
COMMIT;
