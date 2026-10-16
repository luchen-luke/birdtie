-- Local development fixture for the deterministic Agent MVP.
-- Synthetic venue and event details; never apply this file to shared or production data.
BEGIN;

INSERT INTO accounts (id, account_type, status, handle)
VALUES ('b1700000-0000-4000-8000-000000000001', 'organization', 'active', 'birdtie-dev-badminton')
ON CONFLICT (id) DO UPDATE SET status = 'active';

INSERT INTO organizations (id, account_id, organization_type, name, slug, description, profile, status)
VALUES ('b1700000-0000-4000-8000-000000000002', 'b1700000-0000-4000-8000-000000000001',
        'club', 'Birdtie 本地测试羽毛球社', 'birdtie-development-badminton',
        '仅供本地开发测试的虚构社团。', '{"environment":"local-development"}', 'active')
ON CONFLICT (id) DO UPDATE SET name = EXCLUDED.name, slug = EXCLUDED.slug,
    description = EXCLUDED.description, status = 'active';

INSERT INTO agents (id, agent_type, principal_account_id, status)
VALUES ('b1700000-0000-4000-8000-000000000003', 'organization', 'b1700000-0000-4000-8000-000000000001', 'active')
ON CONFLICT (id) DO UPDATE SET status = 'active';

INSERT INTO places (id, city_id, name, category_code, summary, latitude, longitude,
                    coordinate_system, location_precision, publication_status,
                    source_label, source_ref, maintainer_label, updated_at, verified_at)
VALUES ('b1700000-0000-4000-8000-000000000004', 'aberdeen-gb',
        'Birdtie 测试体育馆', 'sports_venue',
        '仅供本地开发测试的虚构场地。',
        57.1500, -2.1000, 'wgs84', 'point', 'published',
        '本地开发示例', 'dev-seed://badminton-weekend',
        'Birdtie 本地开发环境', now(), now())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, summary = EXCLUDED.summary, latitude = EXCLUDED.latitude,
    longitude = EXCLUDED.longitude, location_precision = 'point', publication_status = 'published',
    source_label = EXCLUDED.source_label, source_ref = EXCLUDED.source_ref,
    maintainer_label = EXCLUDED.maintainer_label, updated_at = now(), verified_at = now();

INSERT INTO places (id, city_id, name, category_code, summary, latitude, longitude,
                    coordinate_system, location_precision, publication_status,
                    source_label, source_ref, maintainer_label, updated_at, verified_at)
VALUES ('b1700000-0000-4000-8000-000000000007', 'aberdeen-gb',
        'Birdtie 测试北区体育馆', 'sports_venue',
        '仅供本地开发测试的第二处虚构场地。',
        57.1600, -2.0800, 'wgs84', 'point', 'published',
        '本地开发示例', 'dev-seed://badminton-weekend',
        'Birdtie 本地开发环境', now(), now())
ON CONFLICT (id) DO UPDATE SET
    name = EXCLUDED.name, summary = EXCLUDED.summary, latitude = EXCLUDED.latitude,
    longitude = EXCLUDED.longitude, location_precision = 'point', publication_status = 'published',
    source_label = EXCLUDED.source_label, source_ref = EXCLUDED.source_ref,
    maintainer_label = EXCLUDED.maintainer_label, updated_at = now(), verified_at = now();

WITH local_week AS (
    SELECT ((date_trunc('week', now() AT TIME ZONE 'Europe/London') + interval '5 days')
              AT TIME ZONE 'Europe/London') AS saturday
)
INSERT INTO activities (id, host_account_id, organization_id, city_id, place_id, host_label,
                        title, summary, category_code, starts_at, ends_at, time_zone,
                        publication_status, published_at,
                        source_label, source_ref, maintainer_label, verified_at, expires_at)
SELECT seed.id, 'b1700000-0000-4000-8000-000000000001',
       'b1700000-0000-4000-8000-000000000002', 'aberdeen-gb',
       seed.place_id, 'Birdtie 本地测试羽毛球社', seed.title, seed.summary,
       'badminton',
       local_week.saturday + seed.start_offset, local_week.saturday + seed.end_offset,
       'Europe/London', 'published', now(), '本地开发示例',
       'dev-seed://badminton-weekend', 'Birdtie 本地开发环境', now(),
       local_week.saturday + interval '9 days'
FROM local_week
CROSS JOIN (VALUES
    ('b1700000-0000-4000-8000-000000000005'::uuid,
     'b1700000-0000-4000-8000-000000000004'::uuid,
     '周六羽毛球交流（本地测试）',
     '仅供本地开发测试的虚构羽毛球活动。',
     interval '11 hours', interval '13 hours'),
    ('b1700000-0000-4000-8000-000000000006'::uuid,
     'b1700000-0000-4000-8000-000000000007'::uuid,
     '周日羽毛球双打（本地测试）',
     '仅供本地开发测试的虚构双打活动。',
     interval '1 day 14 hours', interval '1 day 16 hours')
) AS seed(id, place_id, title, summary, start_offset, end_offset)
ON CONFLICT (id) DO UPDATE SET
    organization_id = EXCLUDED.organization_id, place_id = EXCLUDED.place_id,
    host_label = EXCLUDED.host_label, title = EXCLUDED.title,
    summary = EXCLUDED.summary, category_code = EXCLUDED.category_code,
    starts_at = EXCLUDED.starts_at,
    ends_at = EXCLUDED.ends_at, publication_status = 'published',
    published_at = coalesce(activities.published_at, EXCLUDED.published_at),
    source_label = EXCLUDED.source_label, source_ref = EXCLUDED.source_ref,
    maintainer_label = EXCLUDED.maintainer_label, verified_at = now(), expires_at = EXCLUDED.expires_at;

COMMIT;
