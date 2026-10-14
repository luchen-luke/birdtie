-- Explicit human profile pictures. The original image is never retained.
CREATE TABLE person_avatars (
 account_id uuid PRIMARY KEY REFERENCES accounts(id),
 version bigint NOT NULL CHECK (version > 0),
 thumbnail bytea,
 sha256 text,
 updated_at timestamptz NOT NULL DEFAULT now(),
 CHECK ((thumbnail IS NULL AND sha256 IS NULL) OR
        (octet_length(thumbnail) BETWEEN 1 AND 131072 AND sha256 ~ '^[0-9a-f]{64}$'))
);
CREATE TABLE person_avatar_operations (
 account_id uuid NOT NULL REFERENCES accounts(id),
 operation_id uuid NOT NULL,
 action text NOT NULL CHECK (action IN ('SAVE','DELETE')),
 expected_version bigint NOT NULL CHECK (expected_version >= 0),
 applied_version bigint NOT NULL CHECK (applied_version > 0),
 input_sha256 text NOT NULL CHECK ((action='DELETE' AND input_sha256='') OR (action='SAVE' AND input_sha256 ~ '^[0-9a-f]{64}$')),
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY (account_id,operation_id)
);
-- Only current, permitted personal profile images enter public/social DTOs.
CREATE FUNCTION birdtie_person_avatar_metadata(target uuid, viewer uuid)
RETURNS jsonb LANGUAGE sql VOLATILE AS $$
 SELECT jsonb_build_object('accountId',a.account_id,'version',a.version,
  'thumbnailPath','/v1/accounts/'||a.account_id::text||'/avatar/'||a.version::text||'/content',
  'width',128,'height',128,'byteCount',octet_length(a.thumbnail),'sha256',a.sha256)
 FROM person_avatars a JOIN accounts owner ON owner.id=a.account_id
 WHERE a.account_id=target AND owner.account_type='person' AND owner.status='active'
 AND a.thumbnail IS NOT NULL AND birdtie_profile_visible_to(target,viewer)
$$;
