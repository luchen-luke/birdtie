BEGIN;
-- Extend the original point publication audit; historical correlation is unknown.
ALTER TABLE organization_map_location_audit ADD COLUMN request_id text;
ALTER TABLE organization_map_location_audit ALTER COLUMN request_id
 SET DEFAULT NULLIF(current_setting('birdtie.audit_request_id',true),'');
ALTER TABLE organization_map_location_audit ADD CONSTRAINT organization_map_location_audit_request_id_safe
 CHECK(request_id IS NULL OR (octet_length(request_id) BETWEEN 8 AND 64
 AND request_id COLLATE "C" ~ '^[A-Za-z0-9_-]{8,64}$'));
CREATE INDEX organization_map_location_audit_request_correlation
 ON organization_map_location_audit(request_id) WHERE request_id IS NOT NULL;
COMMENT ON COLUMN organization_map_location_audit.request_id IS
 'Validated untrusted HTTP correlation only; NULL means unknown/background. No identity, authorization, coordinates or review body.';
COMMIT;
