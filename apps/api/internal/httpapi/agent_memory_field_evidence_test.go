package httpapi

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
)

// This is the registered human GET and its existing sealed native-port spy.
// It proves wire behavior only, not a PostgreSQL session or media analysis.
func TestFieldEvidenceMemoryRegisteredGETExposesCurrentDeclarationMetadata(t *testing.T) {
	h, f, token := memoryAPIHTTPFixture(t)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, privateProfileHTTPRequest("GET", memoryHTTPList+"/"+memoryHTTPID, "", token, ""))
	if w.Code != 200 || f.reads != 1 || f.revalidates != 1 {
		t.Fatalf("original GET/current boundary: %d reads=%d revalidates=%d", w.Code, f.reads, f.revalidates)
	}
	var wire struct {
		Data struct {
			FieldEvidenceSet *acb.FieldEvidenceSet `json:"fieldEvidenceSet"`
		} `json:"data"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &wire); err != nil {
		t.Fatal(err)
	}
	if wire.Data.FieldEvidenceSet == nil {
		t.Fatal("registered current Memory GET has no field-level declaration/source/time evidence")
	}
	s := wire.Data.FieldEvidenceSet
	if acb.ValidateFieldEvidenceShape(*s) != nil || len(s.Claims) != 1 || s.GrantsAuthority || s.ModelAccess != "UNAVAILABLE" || s.MediaAccess != "UNAVAILABLE" || !s.Owner.Equal(f.p.Owner) {
		t.Fatal("invalid native metadata")
	}
	c := s.Claims[0]
	if c.ClaimantID != f.p.Owner.ID || c.Source.ID != memoryHTTPID || c.Source.Version.Revision != f.p.Target.Version || c.CapturedAt != nil || c.SourceCreatedAt == nil || c.ValidFrom == nil || !c.SourceUpdatedAt.Equal(f.p.Memory.UpdatedAt) || !c.ObservedAt.Equal(f.p.ObservedAt) {
		t.Fatal("declaration/observation/creation/capture conflated")
	}
}

func TestFieldEvidenceMemoryGETStillFailsClosedBeforePrivateMetadata(t *testing.T) {
	for name, late := range map[string]error{"expired": agentmemory.ErrConflict, "revoked": agentmemory.ErrForbidden, "unknown": errors.New("PRIVATE_ERROR_CANARY")} {
		t.Run(name, func(t *testing.T) {
			h, f, token := memoryAPIHTTPFixture(t)
			f.late = late
			w := httptest.NewRecorder()
			h.ServeHTTP(w, privateProfileHTTPRequest("GET", memoryHTTPList+"/"+memoryHTTPID, "", token, ""))
			if w.Code == 200 || strings.Contains(w.Body.String(), "fieldEvidenceSet") || strings.Contains(w.Body.String(), memoryHTTPID) || strings.Contains(w.Body.String(), "PRIVATE_ERROR_CANARY") {
				t.Fatal("late boundary released metadata", w.Code)
			}
		})
	}
}
