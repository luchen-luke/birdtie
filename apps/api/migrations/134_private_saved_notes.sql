BEGIN;
-- Private bookmark annotations are separate from the canonical saved row.
-- No source token, xmin, Agent event, Memory evidence or public content changes.
CREATE TABLE private_saved_notes (
 saved_item_id uuid PRIMARY KEY REFERENCES saved_items(id) ON DELETE CASCADE,
 note text NOT NULL CHECK (octet_length(note)<=2000),
 revision bigint NOT NULL CHECK (revision BETWEEN 1 AND 9007199254740991),
 updated_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK (isfinite(updated_at))
);
CREATE TABLE private_saved_note_operations (
 saved_item_id uuid NOT NULL REFERENCES saved_items(id) ON DELETE CASCADE,
 operation_id uuid NOT NULL CHECK (operation_id<>'00000000-0000-0000-0000-000000000000'::uuid),
 input_sha256 bytea NOT NULL CHECK (octet_length(input_sha256)=32),
 result_revision bigint NOT NULL CHECK (result_revision BETWEEN 1 AND 9007199254740991),
 created_at timestamptz NOT NULL DEFAULT clock_timestamp() CHECK (isfinite(created_at)),
 PRIMARY KEY(saved_item_id,operation_id)
);
COMMENT ON TABLE private_saved_notes IS 'Owner-only bookmark notes; never an Agent source or public projection';
COMMENT ON TABLE private_saved_note_operations IS 'Private idempotent note receipts; no note body; cascade with bookmark deletion';
COMMIT;
