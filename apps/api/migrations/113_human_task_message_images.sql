BEGIN;
-- Original Task/message association only: no new Task/Run, conversation rewrite,
-- public projection, model payload, grant, price or queue mutation.
CREATE TABLE human_task_message_images (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 owner_account_id uuid NOT NULL REFERENCES accounts(id) ON DELETE RESTRICT,
 task_id uuid NOT NULL REFERENCES agent_tasks(id) ON DELETE RESTRICT,
 operation_id uuid NOT NULL,
 message_index integer NOT NULL CHECK(message_index BETWEEN 0 AND 255),
 prefix_digest text NOT NULL CHECK(prefix_digest ~ '^[0-9a-f]{64}$'),
 expected_task_updated_at timestamptz NOT NULL CHECK(isfinite(expected_task_updated_at)),
 source_binding text NOT NULL,
 session_id uuid NOT NULL REFERENCES sessions(id) ON DELETE RESTRICT,
 session_authorization_generation bigint NOT NULL CHECK(session_authorization_generation>0),
 mime_type text NOT NULL CHECK(mime_type IN ('image/png','image/jpeg')),
 input_size bigint NOT NULL CHECK(input_size BETWEEN 1 AND 10485760),
 input_sha256 text NOT NULL CHECK(input_sha256 ~ '^[0-9a-f]{64}$'),
 derivative bytea,
 derivative_sha256 text,
 pixel_risk text NOT NULL CHECK(pixel_risk IN ('UNKNOWN','USER_MASKED')),
 purpose text NOT NULL CHECK(purpose='HUMAN_TASK_MESSAGE_ATTACHMENT'),
 status text NOT NULL DEFAULT 'preview' CHECK(status IN ('preview','ready_private','deleted')),
 revision bigint NOT NULL DEFAULT 1 CHECK(revision>0),
 preview_expires_at timestamptz NOT NULL,
 retain_until timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp(),
 UNIQUE(owner_account_id,operation_id),
 CHECK(isfinite(created_at) AND isfinite(updated_at) AND updated_at>=created_at
  AND isfinite(preview_expires_at) AND isfinite(retain_until) AND retain_until>preview_expires_at),
 CHECK((status='preview' AND derivative IS NULL AND derivative_sha256 IS NULL)
  OR (status='ready_private' AND derivative IS NOT NULL AND octet_length(derivative) BETWEEN 1 AND 16777216
   AND derivative_sha256 IS NOT NULL AND derivative_sha256 ~ '^[0-9a-f]{64}$')
  OR (status='deleted' AND derivative IS NULL AND derivative_sha256 IS NULL))
);
CREATE INDEX human_task_message_images_task ON human_task_message_images(owner_account_id,task_id,message_index,created_at,id);
COMMIT;
