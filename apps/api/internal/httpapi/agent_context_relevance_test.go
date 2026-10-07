package httpapi

import (
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"testing"
)

func TestRelevanceHTTPWireDoesNotAddBudgetOrSelection(t *testing.T) {
	for _, raw := range []string{`{"grantId":"x","budget":1}`, `{"grantId":"x","ownerId":"y"}`, `{"grantId":"x","query":"羽毛球"}`, `{"grantId":"x","memoryIds":[]}`} {
		if _, e := contextPurposeObject([]byte(raw), "grantId"); e != acb.ErrInvalid {
			t.Fatal("relevance added new client authority", raw, e)
		}
	}
}
