package postgres

import (
	"testing"
	"time"
)

func TestOrganizationTaskContextNativeSQLShape(t *testing.T) {
	f := agentPrivateTestFixture(t)
	b := f.base
	var proof string
	var deadline, observed time.Time
	if err := b.pool.QueryRow(b.ctx, organizationPublicSourceSQL, "aberdeen-gb", b.accounts[0]).Scan(&proof, &deadline, &observed); err != nil {
		t.Fatal("native source SQL shape", err)
	}
	if proof == "" || deadline.IsZero() {
		t.Fatal("missing native source proof")
	}
}
