package agentorganizationmemory

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"strings"
	"testing"
	"time"
)

func orgWireInput() PutInput {
	return PutInput{ExpectedVersion: 0, Category: agentmemory.OrgFAQ, Key: "how-to", Summary: "合成主办方明确声明", StructuredValue: json.RawMessage(`{"note":"不接私密源","tags":[]}`), Visibility: agentmemory.VisibilityAgentOnly, ValidUntil: time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)}
}
func TestOrganizationMemoryAccessJSONCannotRetainAuthority(t *testing.T) {
	a := Access{ActingPersonID: "11111111-1111-1111-1111-111111111111", OrganizationID: "22222222-2222-2222-2222-222222222222"}
	a.SessionDigest[0] = 1
	if ValidateAccess(a) != nil {
		t.Fatal("pure shape fixture invalid")
	}
	if _, e := json.Marshal(a); e == nil {
		t.Fatal("internal Access serialized")
	}
	if e := json.Unmarshal([]byte(`{}`), &a); e == nil || a != (Access{}) {
		t.Fatal("rejected decode retained preexisting authority")
	}
	var missing *Access
	if e := missing.UnmarshalJSON(nil); e == nil {
		t.Fatal("nil decode did not reject")
	}
}
func TestOrganizationMemoryWireExactBodyAndNoAuthority(t *testing.T) {
	in := orgWireInput()
	raw, _ := json.Marshal(in)
	decoded, e := DecodePut(raw)
	if e != nil {
		t.Fatal(e)
	}
	n, e := decoded.Native(time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC))
	if e != nil || n.MemoryKey != "org.v1.faq.how-to" || n.MemoryType != agentmemory.TypeOrganization {
		t.Fatal(e)
	}
	for name, body := range map[string]string{"duplicate": strings.Replace(string(raw), `"category":"FAQ"`, `"category":"FAQ","category":"FAQ"`, 1), "role": strings.TrimSuffix(string(raw), "}") + `,"role":"owner"}`, "owner": strings.TrimSuffix(string(raw), "}") + `,"ownerId":"fake"}`, "source": strings.TrimSuffix(string(raw), "}") + `,"sourceGrant":"fake"}`, "confirmed": strings.TrimSuffix(string(raw), "}") + `,"confirmed":true}`, "case": strings.Replace(string(raw), "expectedVersion", "ExpectedVersion", 1), "null_version": strings.Replace(string(raw), `"expectedVersion":0`, `"expectedVersion":null`, 1), "fraction": strings.Replace(string(raw), `"expectedVersion":0`, `"expectedVersion":1.2`, 1), "trailing": string(raw) + `{}`, "nested_duplicate": strings.Replace(string(raw), `"note":"不接私密源"`, `"note":"不接私密源","note":"重复"`, 1)} {
		t.Run(name, func(t *testing.T) {
			if _, e := DecodePut([]byte(body)); e == nil {
				t.Fatal("untrusted wire accepted", name)
			}
		})
	}
}
func TestOrganizationMemoryInputBoundariesAndUnavailablePurpose(t *testing.T) {
	now := time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)
	for name, change := range map[string]func(*PutInput){"unknown_category": func(p *PutInput) { p.Category = "UNKNOWN" }, "oversize_key": func(p *PutInput) { p.Key = strings.Repeat("x", 61) }, "empty_summary": func(p *PutInput) { p.Summary = " " }, "expired": func(p *PutInput) { p.ValidUntil = now }, "excess_lifetime": func(p *PutInput) { p.ValidUntil = now.Add(366 * 24 * time.Hour) }, "public": func(p *PutInput) { p.Visibility = "PUBLIC" }, "negative_version": func(p *PutInput) { p.ExpectedVersion = -1 }, "grant_in_value": func(p *PutInput) { p.StructuredValue = json.RawMessage(`{"sourceGrant":"invented"}`) }} {
		t.Run(name, func(t *testing.T) {
			p := orgWireInput()
			change(&p)
			if _, e := p.Native(now); e == nil {
				t.Fatal("invalid input accepted")
			}
		})
	}
	// Access is an internal binding only. A blank or JSON-generated value cannot
	// be an authenticated administrator; no inference/purpose callback exists.
	var a Access
	if json.Unmarshal([]byte(`{"ActingPersonID":"fake","OrganizationID":"fake","SessionDigest":1}`), &a) == nil {
		t.Fatal("server-only access decoded from JSON")
	}
	if ValidateAccess(a) == nil {
		t.Fatal("body manufactured access")
	}
}
