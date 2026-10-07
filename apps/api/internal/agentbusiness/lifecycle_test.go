package agentbusiness

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"testing"
	"time"
)

func identityFixture() BusinessIdentity {
	at := time.Now().UTC()
	p, _ := agentprofile.New("be000000-0000-4000-8000-000000000004", actorref.PrincipalRef{Type: actorref.Business, ID: "be000000-0000-4000-8000-000000000003"}, at)
	return BusinessIdentity{Schema: IdentitySchema, BusinessID: "be000000-0000-4000-8000-000000000001", BusinessName: "合成商家", Principal: pOwner(p), Role: "owner", ClaimStatus: "verified", ClaimState: "verified", ClaimVersion: 2, Agent: &BusinessIdentityAgent{ID: p.AgentID, Type: actorref.Business, Status: "suspended", Profile: p}, ObservedAt: at, ValidUntil: at.Add(30 * time.Second), MetadataOnly: true, Tools: []string{}, SourceVersion: Digest([]byte("native"))}
}
func pOwner(p agentprofile.Record) actorref.PrincipalRef {
	return actorref.PrincipalRef{Type: p.OwnerType, ID: p.OwnerID}
}
func TestBusinessAgentIdentityMetadataOnly(t *testing.T) {
	v := identityFixture()
	if e := ValidateBusinessIdentity(v); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(v)
	var m map[string]any
	json.Unmarshal(raw, &m)
	if _, ok := m["sourceVersion"]; ok {
		t.Fatal("native binding leaked")
	}
	v.Agent.Status = "active"
	if ValidateBusinessIdentity(v) == nil {
		t.Fatal("active business rejected")
	}
}
func TestBusinessAgentIdentityClosedConfirmation(t *testing.T) {
	for _, raw := range []string{`{"expectedClaimVersion":2}`, `{"expectedClaimVersion":0}`, `{"expectedClaimVersion":2,"enabled":true}`, `{"expectedClaimVersion":2,"expectedClaimVersion":2}`} {
		v, e := DecodeEstablishIdentity([]byte(raw))
		if (e == nil) != (raw == `{"expectedClaimVersion":2}`) || e == nil && v.ExpectedClaimVersion != 2 {
			t.Fatalf("%s %v", raw, e)
		}
	}
}
func TestBusinessAgentIdentityNoClaimIsNotEligible(t *testing.T) {
	v := identityFixture()
	v.Agent = nil
	v.ClaimVersion = 0
	v.ClaimState = "pending"
	if ValidateBusinessIdentity(v) != nil {
		t.Fatal("unconfigured metadata must be readable")
	}
	v.Role = "member"
	if ValidateBusinessIdentity(v) == nil {
		t.Fatal("member not manager")
	}
}
func TestBusinessAgentIdentityProfileOwnerBound(t *testing.T) {
	v := identityFixture()
	v.Agent.Profile.OwnerID = "be000000-0000-4000-8000-000000000005"
	if ValidateBusinessIdentity(v) == nil {
		t.Fatal("mismatched principal")
	}
}
