package connection_test

import (
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

const closeOwner = "b1000000-0000-0000-0000-000000000001"
const closePerson = "b1000000-0000-0000-0000-000000000002"
const closeTie = "b1000000-0000-0000-0000-000000000003"
const closeRequest = "b1000000-0000-0000-0000-000000000004"

func TestCloseContractStrictSpecificConfirmation(t *testing.T) {
	put := `{"tieId":"` + closeTie + `","requestId":"` + closeRequest + `","sourceVersion":"` + strings.Repeat("a", 64) + `","validUntil":"2026-10-10T18:00:00.000001Z","confirmed":true}`
	v, e := connection.DecodeClosePut([]byte(put))
	if e != nil || v.TieID != closeTie || v.RequestID != closeRequest || v.ValidUntil.Nanosecond() != 1000 {
		t.Fatal("specific source and UTC microsecond contract", e)
	}
	for _, tc := range []struct{ name, body string }{
		{"unconfirmed", strings.Replace(put, "true", "false", 1)},
		{"duplicate", strings.Replace(put, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1)},
		{"owner-override", strings.Replace(put, `"confirmed":true`, `"confirmed":true,"ownerId":"`+closeOwner+`"`, 1)},
		{"model-authority", strings.Replace(put, `"confirmed":true`, `"confirmed":true,"modelAccess":true`, 1)},
		{"bad-calendar", strings.Replace(put, "2026-10-10", "2026-02-30", 1)},
		{"year-zero", strings.Replace(put, "2026-10-10", "0000-10-10", 1)},
		{"non-utc", strings.Replace(put, "000001Z", "000001+08:00", 1)},
		{"excess-precision", strings.Replace(put, "000001Z", "0000001Z", 1)},
		{"comma-precision", strings.Replace(put, "00.000001Z", "00,000001Z", 1)},
		{"uppercase-source", strings.Replace(put, strings.Repeat("a", 64), strings.Repeat("A", 64), 1)},
		{"zero-tie", strings.Replace(put, closeTie, "00000000-0000-0000-0000-000000000000", 1)},
		{"trailing", put + `{}`}, {"array", `[]`}, {"oversize", strings.Repeat(" ", 4097) + put},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := connection.DecodeClosePut([]byte(tc.body)); e == nil {
				t.Fatal("unsafe confirmation accepted")
			}
		})
	}
	del := `{"tieId":"` + closeTie + `","requestId":"` + closeRequest + `","expectedRevision":3,"confirmed":true}`
	d, e := connection.DecodeCloseDelete([]byte(del))
	if e != nil || d.ExpectedRevision != 3 {
		t.Fatal(e)
	}
	for _, tc := range []struct{ name, body string }{{"delete-unconfirmed", strings.Replace(del, "true", "false", 1)}, {"delete-zero-revision", strings.Replace(del, ":3", ":0", 1)}, {"delete-fraction", strings.Replace(del, ":3", ":3.0", 1)}, {"delete-revision-overflow", strings.Replace(del, ":3", ":9223372036854775808", 1)}, {"delete-duplicate", strings.Replace(del, `"confirmed":true`, `"confirmed":true,"confirmed":true`, 1)}} {
		t.Run(tc.name, func(t *testing.T) {
			if _, e := connection.DecodeCloseDelete([]byte(tc.body)); e == nil {
				t.Fatal("unsafe revoke accepted")
			}
		})
	}
}
func TestCloseContractScopeAndWire(t *testing.T) {
	now := time.Date(2026, 10, 10, 18, 0, 0, 0, time.UTC)
	entry := connection.CloseEntry{PersonID: closePerson, TieID: closeTie, RequestID: closeRequest, DisplayName: "当前好友名字", SourceVersion: strings.Repeat("a", 64)}
	view := connection.CloseView{SchemaVersion: connection.CloseSchema, OwnerID: closeOwner, ObservedAt: now, ValidUntil: now.Add(30 * time.Second), Connections: []connection.CloseEntry{entry}, Limit: 100, ResponseProof: "PROOF_PRIVATE_CANARY"}
	if !connection.ValidCloseView(view, closeOwner) {
		t.Fatal("current eligible friend")
	}
	raw, _ := json.Marshal(view)
	if strings.Contains(string(raw), "PROOF_PRIVATE_CANARY") || !strings.Contains(string(raw), `"modelAccess":false`) {
		t.Fatal("proof or model authority leaked")
	}
	for _, tc := range []string{"foreign-owner", "model", "too-long", "duplicate", "nil-vector", "truncated-short", "fake-source", "close-without-revision"} {
		t.Run(tc, func(t *testing.T) {
			v := view
			v.Connections = append([]connection.CloseEntry{}, view.Connections...)
			switch tc {
			case "foreign-owner":
				v.OwnerID = closePerson
			case "model":
				v.ModelAccess = true
			case "too-long":
				v.ValidUntil = now.Add(31 * time.Second)
			case "duplicate":
				v.Connections = append(v.Connections, entry)
			case "nil-vector":
				v.Connections = nil
			case "truncated-short":
				v.Truncated = true
			case "fake-source":
				v.Connections[0].SourceVersion = "client-claim"
			case "close-without-revision":
				v.Connections[0].IsClose = true
			}
			if connection.ValidCloseView(v, closeOwner) {
				t.Fatal("unsafe view accepted")
			}
		})
	}
	a := ea.Access{Actor: identity.Actor{ID: closeOwner, AccountType: "person"}, SessionDigest: [32]byte{1}}
	if !connection.ValidCloseAccess(a) {
		t.Fatal("current human")
	}
	a.Public = true
	if connection.ValidCloseAccess(a) {
		t.Fatal("public cannot manage Close")
	}
	a.Public = false
	a.SessionDigest = [32]byte{}
	if connection.ValidCloseAccess(a) {
		t.Fatal("owner metadata cannot replace session")
	}
	raw, _ = json.Marshal(connection.CloseDeleteReceipt{PersonID: closePerson, ResponseProof: "DELETE_PROOF_CANARY"})
	if string(raw) != "{}" {
		t.Fatal("delete receipt is server-only")
	}
}
