package postgres

import (
	"strings"
	"testing"
)

// SQL assembly regression only; native row/lock execution is a separate root acceptance.
func TestPublicMomentAuthorProjectionRequiresExplicitPublicField(t *testing.T) {
	sql := publicMomentWithAuthorProjectionSQL
	for _, required := range []string{"user_profiles", "visibility='public'", "birdtie_agent_profile_field_allowed(a.id,NULL::uuid,'displayName')", "'accountId'", "'displayName'", "ELSE NULL", "profile.xmin", "agent_profile_field_visibility", "agent_profiles", "agents"} {
		if !strings.Contains(sql, required) {
			t.Fatalf("missing author authority/proof %s", required)
		}
	}
	for _, forbidden := range []string{"a.handle", "profile.bio", "consent_grants", "personalPreferences", "authorAccountId"} {
		if strings.Contains(sql, forbidden) {
			t.Fatalf("unauthorized fallback %s", forbidden)
		}
	}
}

func TestPublicMomentAuthorProjectionRetainsDomainAndRetainedGates(t *testing.T) {
	for _, sql := range []string{publicMomentProjectionSQL, publicMomentWithAuthorProjectionSQL} {
		for _, required := range []string{"m.visibility='public'", "m.status='published'", "m.author_confirmed_at<=m.published_at", "a.account_type='person' AND a.status='active'", "account_blocks", "ss.revoked_at IS NULL", "ss.idle_expires_at>cl.at", currentRetainedPlaceVisibilitySQL("p", "cl.at")} {
			if !strings.Contains(sql, required) {
				t.Fatalf("missing original/current gate %s", required)
			}
		}
	}
	if !strings.Contains(publicMomentWithAuthorProjectionSQL, retainedPlaceReadBindingSQL("p")) {
		t.Fatal("source generation not included in final read proof")
	}
}
