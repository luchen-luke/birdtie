package agentmemory

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

func memoryAPIPure(t *testing.T) (DetailProjection, agentprofile.PrivateAccess) {
	t.Helper()
	at := time.Date(2026, 10, 6, 1, 0, 0, 0, time.UTC)
	owner := actorref.PrincipalRef{Type: actorref.Person, ID: "22222222-2222-4222-8222-222222222222"}
	a := agentprofile.PrivateAccess{WorkspacePrincipal: owner}
	a.SessionDigest[0] = 1
	m, e := NewExplicit("11111111-1111-4111-8111-111111111111", "33333333-3333-4333-8333-333333333333", owner, 1, PutInput{MemoryType: TypePreference, MemoryKey: "human.detail", Summary: "合成本人声明", StructuredValue: json.RawMessage(`{"native":true}`), Visibility: VisibilityPrivate, ValidUntil: at.Add(time.Hour)}, at, at)
	if e != nil {
		t.Fatal(e)
	}
	p := DetailProjection{SchemaVersion: DetailSchema, Owner: owner, AgentID: m.AgentID, Target: DetailTarget{ID: m.ID, Version: m.Version, Status: m.Status}, Memory: &m, ObservedAt: at, ExpiresAt: at.Add(time.Minute), Explanation: DetailExplanation}
	p, e = IssueDetail(p, a, strings.Repeat("a", 64))
	if e != nil {
		t.Fatal(e)
	}
	return p, a
}
func TestMemoryAPIPureDetailSealedHumanContract(t *testing.T) {
	p, a := memoryAPIPure(t)
	if _, e := DetailNativeProof(p, a); e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(p)
	if e != nil || strings.Contains(string(raw), "proof") || strings.Contains(string(raw), strings.Repeat("a", 64)) {
		t.Fatal("private proof on wire")
	}
	var decoded DetailProjection
	if json.Unmarshal(raw, &decoded) == nil || decoded.proof != nil {
		t.Fatal("JSON reconstructed proof")
	}
	for _, status := range []Status{StatusDeleted, StatusExpired} {
		v := p
		v.proof = nil
		v.Target.Status = status
		v.Memory = nil
		if _, e = IssueDetail(v, a, strings.Repeat("b", 64)); e != nil {
			t.Fatal("metadata only", e)
		}
		v.Memory = p.Memory
		if ValidateDetail(v, v.ObservedAt) == nil {
			t.Fatal("expired/deleted body exposed")
		}
	}
}
func TestMemoryAPIPureDetailAllMutationAndFiniteBoundaries(t *testing.T) {
	for _, mode := range []string{"owner", "agent", "id", "version", "status", "summary", "value", "lease", "observed", "explanation", "model", "futurefrom", "futureupdated", "wrongsession", "unissued", "expired", "yearzero", "oversourceexpiry"} {
		t.Run(mode, func(t *testing.T) {
			p, a := memoryAPIPure(t)
			m := *p.Memory
			m.StructuredValue = append([]byte(nil), m.StructuredValue...)
			p.Memory = &m
			switch mode {
			case "owner":
				p.Owner.ID = p.Target.ID
			case "agent":
				p.AgentID = p.Target.ID
			case "id":
				p.Target.ID = p.AgentID
			case "version":
				p.Target.Version++
			case "status":
				p.Target.Status = StatusExpired
			case "summary":
				p.Memory.Summary = "改变合成声明"
			case "value":
				p.Memory.StructuredValue = json.RawMessage(`{"native":false}`)
			case "lease":
				p.ExpiresAt = p.ExpiresAt.Add(time.Second)
			case "observed":
				p.ObservedAt = p.ObservedAt.Add(time.Second)
			case "explanation":
				p.Explanation = "confirmed"
			case "model":
				p.ModelAccess = true
			case "futurefrom":
				p.Memory.ValidFrom = p.ObservedAt.Add(time.Second)
			case "futureupdated":
				p.Memory.UpdatedAt = p.ObservedAt.Add(time.Second)
			case "wrongsession":
				a.SessionDigest[0]++
			case "unissued":
				p.proof = nil
			case "expired":
				if ValidateDetail(p, p.ExpiresAt) == nil {
					t.Fatal("deadline accepted")
				}
				return
			case "yearzero":
				p.ObservedAt = time.Date(1, 1, 1, 0, 0, 0, 0, time.FixedZone("offset", 23*3600))
			case "oversourceexpiry":
				p.Memory.ValidUntil = p.ObservedAt.Add(30 * time.Second)
			}
			if _, e := DetailNativeProof(p, a); e == nil {
				t.Fatal("mutated proof accepted")
			}
		})
	}
}
func TestMemoryAPIPureRejectClosedInput(t *testing.T) {
	good := `{"operationId":"11111111-1111-4111-8111-111111111111","planDigest":"` + strings.Repeat("a", 64) + `"}`
	if _, e := DecodeRejectInput([]byte(good)); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{"", `null`, `[]`, `{}`, good + good, strings.Replace(good, "operationId", "OperationId", 1), strings.Replace(good, `"planDigest":`, `"confirmed":true,"planDigest":`, 1), strings.Replace(good, `"planDigest":`, `"operationId":"22222222-2222-4222-8222-222222222222","planDigest":`, 1), strings.Replace(good, strings.Repeat("a", 64), strings.Repeat("A", 64), 1), strings.Replace(good, `"operationId":"11111111-1111-4111-8111-111111111111"`, `"operationId":1`, 1), strings.Repeat("x", MaxBodyBytes+1)} {
		if _, e := DecodeRejectInput([]byte(bad)); e == nil {
			t.Fatal("invalid input accepted")
		}
	}
}
