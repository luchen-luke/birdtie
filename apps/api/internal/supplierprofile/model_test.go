package supplierprofile

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestSupplierPermissionClosedInput(t *testing.T) {
	now := time.Date(2026, 10, 3, 12, 0, 0, 0, time.UTC)
	good := PermissionInput{ExpectedVersion: 0, ExpectedProfileVersion: 2, Action: "publish", ValidUntil: now.Add(time.Hour).Format(time.RFC3339Nano), SourceSnapshot: strings.Repeat("a", 64)}
	if _, e := ValidatePermission(good, now); e != nil {
		t.Fatal(e)
	}
	for name, edit := range map[string]func(*PermissionInput){"negative": func(p *PermissionInput) { p.ExpectedVersion = -1 }, "overflow": func(p *PermissionInput) { p.ExpectedVersion = int64(^uint64(0) >> 1) }, "no_profile": func(p *PermissionInput) { p.ExpectedProfileVersion = 0 }, "no_snapshot": func(p *PermissionInput) { p.SourceSnapshot = "" }, "expired": func(p *PermissionInput) { p.ValidUntil = now.Format(time.RFC3339) }, "unknown": func(p *PermissionInput) { p.Action = "approve" }, "bad_time": func(p *PermissionInput) { p.ValidUntil = "2026-13-03T12:00:00Z" }} {
		t.Run(name, func(t *testing.T) {
			p := good
			edit(&p)
			if _, e := ValidatePermission(p, now); e == nil {
				t.Fatal("invalid accepted")
			}
		})
	}
	revoke := PermissionInput{ExpectedVersion: 1, Action: "revoke"}
	if _, e := ValidatePermission(revoke, now); e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(good)
	if _, e := DecodePermission(raw); e != nil {
		t.Fatal(e)
	}
	for _, s := range []string{`{"action":"publish","action":"revoke"}`, `null`, `[]`, `{"expectedVersion":0,"expectedProfileVersion":2,"action":"publish","validUntil":"","sourceSnapshot":"","approvedBy":"fake"}`} {
		if _, e := DecodePermission([]byte(s)); e == nil {
			t.Fatal("wire accepted", s)
		}
	}
}
