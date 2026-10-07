package postgres

import (
	"os"
	"strings"
	"testing"
)

func TestConnectionDecisionOperationMigrationStaticContract(t *testing.T) {
	// UNIT_STATIC only: migration/PG constraints and down are NOT_RUN.
	b, e := os.ReadFile("../../migrations/103_connection_request_decision_receipts.sql")
	if e != nil {
		t.Fatal(e)
	}
	s := string(b)
	for _, part := range []string{"a.resource_id=NEW.request_id::text", "reason IS NOT NULL", "NEW.reason IS NULL OR NOT (", "PRIMARY KEY(owner_account_id,operation_id)", "REFERENCES connection_requests(id) ON DELETE RESTRICT", "same transaction original decision required", "a.xmin::text=", "original.xmin::text=", "BEFORE INSERT OR UPDATE OR DELETE", "authoritative pre-effect rejection required"} {
		if !strings.Contains(s, part) {
			t.Fatal("missing original-domain guard", part)
		}
	}
	b, e = os.ReadFile("../../migrations/103_connection_request_decision_receipts.down.sql")
	if e != nil || !strings.Contains(string(b), "IF EXISTS(SELECT 1 FROM connection_request_decision_receipts)") {
		t.Fatal("used history must survive down", e)
	}
}
