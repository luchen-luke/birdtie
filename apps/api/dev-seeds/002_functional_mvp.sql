-- LOCAL DEVELOPMENT ONLY. Every person, organization, venue and activity below is fictional.
-- The CSSA-labelled row is a UI/API fixture, not an official or verified CSSA account.
-- Apply after all migrations and 001_badminton.sql; never use in shared or production data.
BEGIN;

INSERT INTO accounts (id, account_type, status, handle) VALUES
  ('b1700000-0000-4000-8000-000000000010', 'person', 'active', 'birdtie-dev-admin'),
  ('b1700000-0000-4000-8000-000000000011', 'person', 'active', 'birdtie-dev-student'),
  ('b1700000-0000-4000-8000-000000000012', 'organization', 'active', 'birdtie-dev-cssa')
ON CONFLICT (id) DO UPDATE SET status = 'active';

INSERT INTO user_profiles (account_id, display_name, bio, visibility) VALUES
  ('b1700000-0000-4000-8000-000000000010', '本地测试管理员', '虚构开发账号', 'private'),
  ('b1700000-0000-4000-8000-000000000011', '本地测试学生', '虚构开发账号', 'private')
ON CONFLICT (account_id) DO UPDATE SET display_name = EXCLUDED.display_name,
  bio = EXCLUDED.bio, visibility = 'private';

INSERT INTO agents (id, agent_type, principal_account_id, status) VALUES
  ('b1700000-0000-4000-8000-000000000020', 'personal', 'b1700000-0000-4000-8000-000000000010', 'active'),
  ('b1700000-0000-4000-8000-000000000021', 'personal', 'b1700000-0000-4000-8000-000000000011', 'active'),
  ('b1700000-0000-4000-8000-000000000014', 'organization', 'b1700000-0000-4000-8000-000000000012', 'active')
ON CONFLICT (agent_type, principal_account_id) DO UPDATE SET status = 'active';

-- These subjects are SHA-256 of birdtie-dev-phone:+999000000071/72.
-- The numbers are fictional development identifiers accepted only by local dev auth.
INSERT INTO account_auth_identities (issuer, subject, account_id, verified_at) VALUES
  ('urn:birdtie:local-dev-phone', '2c19dc0505f0da59b25a9254f5f1c17a05050a2280ce971df4b670b097e70377',
   'b1700000-0000-4000-8000-000000000010', now()),
  ('urn:birdtie:local-dev-phone', '1bd14d21917d624c4fdc278c48335b108ecfe934004f43b1c34aca2490f048ee',
   'b1700000-0000-4000-8000-000000000011', now())
ON CONFLICT (issuer, subject) DO UPDATE SET account_id = EXCLUDED.account_id;

INSERT INTO organizations
  (id, account_id, organization_type, name, slug, description, profile,
   status, verification_status, visibility)
VALUES
  ('b1700000-0000-4000-8000-000000000013', 'b1700000-0000-4000-8000-000000000012',
   'student_society', 'Aberdeen CSSA（虚构本地测试，非官方）', 'birdtie-dev-cssa',
   '仅供本地开发验收的虚构组织，不代表 Aberdeen CSSA 注册、授权或合作。',
   '{"environment":"local-development","fictional":true}'::jsonb,
   'active', 'unverified', 'public')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, description = EXCLUDED.description,
  verification_status = 'unverified', profile = EXCLUDED.profile;

INSERT INTO organization_memberships
  (id, organization_id, user_account_id, role, status) VALUES
  ('b1700000-0000-4000-8000-000000000019',
   'b1700000-0000-4000-8000-000000000013',
   'b1700000-0000-4000-8000-000000000010', 'owner', 'active'),
  ('b1700000-0000-4000-8000-000000000022',
   'b1700000-0000-4000-8000-000000000013',
   'b1700000-0000-4000-8000-000000000011', 'member', 'active'),
  ('b1700000-0000-4000-8000-000000000023',
   'b1700000-0000-4000-8000-000000000002',
   'b1700000-0000-4000-8000-000000000011', 'owner', 'active')
ON CONFLICT (organization_id, user_account_id) DO UPDATE SET
  role = EXCLUDED.role, status = 'active';

UPDATE activities SET created_by_account_id = 'b1700000-0000-4000-8000-000000000011'
WHERE id IN ('b1700000-0000-4000-8000-000000000005',
             'b1700000-0000-4000-8000-000000000006')
  AND created_by_account_id IS NULL;

INSERT INTO places
  (id, city_id, name, category_code, summary, latitude, longitude,
   coordinate_system, location_precision, publication_status,
   source_label, source_ref, maintainer_label, updated_at)
VALUES
  ('b1700000-0000-4000-8000-000000000015', 'aberdeen-gb',
   'Birdtie 测试社区中心', 'community_venue',
   '虚构场地，仅供本地开发测试。', 57.1450, -2.0950,
   'wgs84', 'point', 'published', '本地开发示例',
   'dev-seed://functional-mvp', 'Birdtie 本地开发环境', now())
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, summary = EXCLUDED.summary,
  latitude = EXCLUDED.latitude, longitude = EXCLUDED.longitude,
  publication_status = 'published', updated_at = now();

-- All three additional Activities are scheduled for next week, so this seed
-- remains discoverable even when applied late on a Sunday.
WITH next_week AS (
  SELECT ((date_trunc('week', now() AT TIME ZONE 'Europe/London') + interval '12 days')
          AT TIME ZONE 'Europe/London') AS saturday
)
INSERT INTO activities
  (id, host_account_id, organization_id, created_by_account_id, city_id,
   place_id, host_label, title, summary, category_code,
   starts_at, ends_at, time_zone, publication_status, published_at,
   source_label, source_ref, maintainer_label, expires_at)
SELECT row.id, 'b1700000-0000-4000-8000-000000000012',
  'b1700000-0000-4000-8000-000000000013',
  'b1700000-0000-4000-8000-000000000010', 'aberdeen-gb', row.place_id,
  'Aberdeen CSSA（虚构本地测试，非官方）', row.title, row.summary,
  row.category_code, next_week.saturday + row.start_offset,
  next_week.saturday + row.end_offset, 'Europe/London', 'published', now(),
  '本地开发示例', 'dev-seed://functional-mvp', 'Birdtie 本地开发环境',
  next_week.saturday + interval '16 days'
FROM next_week CROSS JOIN (VALUES
  ('b1700000-0000-4000-8000-000000000016'::uuid,
   'b1700000-0000-4000-8000-000000000015'::uuid,
   '留学生周末见面会（本地测试）', '虚构社交活动，仅供开发测试。',
   'social', interval '11 hours', interval '13 hours'),
  ('b1700000-0000-4000-8000-000000000017'::uuid,
   'b1700000-0000-4000-8000-000000000004'::uuid,
   '周末羽毛球练习（本地测试）', '虚构体育活动，仅供开发测试。',
   'badminton', interval '1 day 14 hours', interval '1 day 16 hours'),
  ('b1700000-0000-4000-8000-000000000018'::uuid,
   'b1700000-0000-4000-8000-000000000015'::uuid,
   '新生生活交流（本地测试）', '虚构交流活动，仅供开发测试。',
   'social', interval '2 days 17 hours', interval '2 days 19 hours')
) AS row(id, place_id, title, summary, category_code, start_offset, end_offset)
ON CONFLICT (id) DO UPDATE SET
  organization_id = EXCLUDED.organization_id,
  created_by_account_id = EXCLUDED.created_by_account_id,
  place_id = EXCLUDED.place_id, host_label = EXCLUDED.host_label,
  title = EXCLUDED.title, summary = EXCLUDED.summary,
  category_code = EXCLUDED.category_code,
  starts_at = EXCLUDED.starts_at, ends_at = EXCLUDED.ends_at,
  publication_status = 'published', cancelled_at = NULL,
  source_label = EXCLUDED.source_label, source_ref = EXCLUDED.source_ref,
  maintainer_label = EXCLUDED.maintainer_label, expires_at = EXCLUDED.expires_at;

COMMIT;
