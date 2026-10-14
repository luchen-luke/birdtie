BEGIN;
CREATE TABLE community_avatars (
 community_id uuid PRIMARY KEY REFERENCES communities(id) ON DELETE CASCADE,
 version bigint NOT NULL CHECK(version BETWEEN 1 AND 9007199254740991),
 thumbnail bytea,sha256 text,updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 CHECK((thumbnail IS NULL AND sha256 IS NULL) OR(thumbnail IS NOT NULL AND sha256 IS NOT NULL AND octet_length(thumbnail) BETWEEN 1 AND 131072 AND sha256 ~ '^[0-9a-f]{64}$'))
);
CREATE TABLE community_avatar_operations (
 community_id uuid NOT NULL REFERENCES communities(id) ON DELETE CASCADE,
 actor_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE CASCADE,
 operation_id uuid NOT NULL,action text NOT NULL CHECK(action IN('SAVE','DELETE')),
 expected_version bigint NOT NULL CHECK(expected_version BETWEEN 0 AND 9007199254740990),applied_version bigint NOT NULL CHECK(applied_version BETWEEN 1 AND 9007199254740991 AND applied_version=expected_version+1),
 input_sha256 text NOT NULL CHECK((action='DELETE' AND input_sha256='') OR(action='SAVE' AND input_sha256 ~ '^[0-9a-f]{64}$')),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),PRIMARY KEY(community_id,actor_account_id,operation_id)
);
-- The managed thumbnail follows the existing community's discoverability.
-- Legacy external avatar_url is not read, fetched, imported or granted here.
CREATE FUNCTION birdtie_community_avatar_metadata(target uuid,viewer uuid) RETURNS jsonb LANGUAGE sql VOLATILE AS $$
 SELECT jsonb_build_object('communityId',a.community_id,'version',a.version,'thumbnailPath','/v1/communities/'||a.community_id::text||'/avatar/'||a.version::text||'/content','width',128,'height',128,'byteCount',octet_length(a.thumbnail),'sha256',a.sha256)
 FROM community_avatars a JOIN communities c ON c.id=a.community_id JOIN accounts creator ON creator.id=c.owner_account_id JOIN accounts v ON v.id=viewer
 WHERE a.community_id=target AND a.thumbnail IS NOT NULL AND creator.account_type='person' AND creator.status='active' AND v.account_type='person' AND v.status='active'
 AND c.lifecycle_status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 AND(c.visibility<>'hidden' OR EXISTS(SELECT 1 FROM community_memberships m WHERE m.community_id=c.id AND m.user_account_id=viewer AND m.status IN('active','invited')))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE(b.blocker_account_id=viewer AND b.blocked_account_id=c.owner_account_id) OR(b.blocker_account_id=c.owner_account_id AND b.blocked_account_id=viewer))
$$;
COMMIT;
