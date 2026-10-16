-- LOCAL DEVELOPMENT ONLY. All three Communities below are fictional fixtures.
-- Apply after migrations 030-032 and 002_functional_mvp.sql. Never run in
-- shared, pilot or production environments.
BEGIN;

-- DGS: explicit isolated fixture identity; never infer permission from local-looking DSNs.
DO $birdtie_fixture_guard$
DECLARE
  fixture_db text := current_database();
  fixture_marker text;
  fixture_owner text;
BEGIN
  IF fixture_db !~ '^(birdtie_fixture_[a-z0-9_]{1,40}|birdtie_owned_migration_[0-9a-f]{24})$' THEN
    RAISE EXCEPTION 'dedicated fixture database required' USING ERRCODE='42501';
  END IF;
  SELECT shobj_description(d.oid, 'pg_database'), pg_get_userbyid(d.datdba)
    INTO fixture_marker, fixture_owner FROM pg_database d
    WHERE d.datname = fixture_db AND NOT d.datistemplate AND d.datallowconn;
  IF fixture_marker IS DISTINCT FROM 'BIRDTIE_ISOLATED_FIXTURE_V1:' || fixture_db
     OR fixture_owner IS DISTINCT FROM current_user::text THEN
    RAISE EXCEPTION 'exact owned fixture database marker required' USING ERRCODE='42501';
  END IF;
  IF current_setting('birdtie.fixture_opt_in', true) IS DISTINCT FROM 'isolated-fixtures-v1'
     OR current_setting('birdtie.fixture_database', true) IS DISTINCT FROM fixture_db THEN
    RAISE EXCEPTION 'explicit fixture session opt-in required' USING ERRCODE='42501';
  END IF;
END
$birdtie_fixture_guard$;


INSERT INTO communities
  (id,city_id,owner_account_id,name,summary,visibility,join_policy,
   lifecycle_status,publication_status,owner_confirmed_at,
   source_label,source_ref,maintainer_label)
VALUES
  ('b1700000-0000-4000-8000-000000000030','aberdeen-gb',
   'b1700000-0000-4000-8000-000000000010',
   'Birdtie 公开运动圈（本地测试）','虚构公开社群，仅供开发验收。',
   'public','open','active','published',now(),
   '本地开发示例','dev-seed://community/public-open','Birdtie 本地开发环境'),
  ('b1700000-0000-4000-8000-000000000031','aberdeen-gb',
   'b1700000-0000-4000-8000-000000000010',
   'Birdtie 申请加入圈（本地测试）','虚构私密社群，仅供开发验收。',
   'private','request','active','published',now(),
   '本地开发示例','dev-seed://community/private-request','Birdtie 本地开发环境'),
  ('b1700000-0000-4000-8000-000000000032','aberdeen-gb',
   'b1700000-0000-4000-8000-000000000010',
   'Birdtie 邀请圈（本地测试）','虚构隐藏社群，仅供开发验收。',
   'hidden','invite_only','active','published',now(),
   '本地开发示例','dev-seed://community/hidden-invite','Birdtie 本地开发环境')
ON CONFLICT (id) DO NOTHING;

-- The Community insert trigger creates each owner in the same transaction.
-- These rows cover a pending request and an unaccepted invitation.
INSERT INTO community_memberships
  (community_id,user_account_id,role,status,invited_by_account_id)
VALUES
  ('b1700000-0000-4000-8000-000000000031',
   'b1700000-0000-4000-8000-000000000011','member','pending',NULL),
  ('b1700000-0000-4000-8000-000000000032',
   'b1700000-0000-4000-8000-000000000011','member','invited',
   'b1700000-0000-4000-8000-000000000010')
ON CONFLICT (community_id,user_account_id) DO NOTHING;

COMMIT;
