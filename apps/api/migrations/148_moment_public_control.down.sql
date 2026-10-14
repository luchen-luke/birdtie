BEGIN;
DO $$ BEGIN
 IF EXISTS(SELECT 1 FROM human_moment_control_operations WHERE action IN('PUBLISH','UPDATE_PUBLIC')) THEN RAISE EXCEPTION 'public Moment operation history cannot be discarded';END IF;
END $$;
DROP TRIGGER human_moment_control_immutable ON human_moment_control_operations;
DROP FUNCTION birdtie_guard_human_moment_public_control();
ALTER TABLE human_moment_control_operations DROP CONSTRAINT human_moment_control_effect_shape;
ALTER TABLE human_moment_control_operations ADD CONSTRAINT human_moment_control_effect_shape CHECK(
 (status='STOPPED' AND action IS NULL AND moment_id IS NULL AND expected_revision IS NULL AND applied_revision=0 AND payload_sha256 IS NULL)
 OR(status='SUCCEEDED' AND action IN('CREATE','UPDATE','WITHDRAW') AND moment_id IS NOT NULL AND expected_revision IS NOT NULL AND expected_revision BETWEEN 0 AND 8999999999999999 AND applied_revision BETWEEN 1 AND 8999999999999999 AND payload_sha256 IS NOT NULL AND octet_length(payload_sha256)=32
 AND((action='CREATE' AND expected_revision=0 AND applied_revision=1) OR(action='UPDATE' AND expected_revision BETWEEN 1 AND 8999999999999998 AND applied_revision=expected_revision+1) OR(action='WITHDRAW' AND expected_revision>=1 AND applied_revision=least(expected_revision+1,8999999999999999)))))
;
CREATE TRIGGER human_moment_control_immutable BEFORE INSERT OR UPDATE OR DELETE ON human_moment_control_operations FOR EACH ROW EXECUTE FUNCTION birdtie_guard_human_moment_control();
COMMIT;
