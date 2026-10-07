package postgres

import (
	"strings"
	"testing"
)

func TestMapSessionProvenanceNativeSQLKeepsCurrentBoundaries(t *testing.T) {
	if !strings.Contains(mapProjectionSessionAuthoritySQL, "se.authorization_generation") || strings.Contains(mapProjectionSessionAuthoritySQL, "se.xmin") || strings.Contains(mapProjectionSessionAuthoritySQL, "se.idle_expires_at") {
		t.Fatal("ordinary idle refresh still retires the authority proof")
	}
	for _, private := range []bool{false, true} {
		query := mapProjectionSQL(private)
		for _, boundary := range []string{"a.xmin::text", "se.created_at", "se.expires_at", "se.authentication_method", "encode(se.token_sha256,'hex')", "se.revoked_at IS NULL", "se.expires_at>stamp.at", "se.idle_expires_at>stamp.at", "se.authorization_generation", "(SELECT deadline FROM native_actor)", "c.publication_status='published'", "p.publication_status='published'"} {
			if !strings.Contains(query, boundary) {
				t.Fatal("map native current boundary disappeared", boundary)
			}
		}
		if strings.Contains(query, "PRIVATE_SOURCE_TOKENS") || strings.Contains(query, "PRIVATE_HUMAN_PAYLOAD") {
			t.Fatal("native map SQL template unresolved")
		}
	}
}
