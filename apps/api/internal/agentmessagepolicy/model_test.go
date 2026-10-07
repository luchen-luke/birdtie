package agentmessagepolicy

import (
	"strings"
	"testing"
	"time"
)

func TestMessagePolicyUnitFourStatesAndRestrictivePrecedence(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, mode := range []Disposition{Request, Screen, Block, Allow, "UNKNOWN"} {
		for _, blocked := range []bool{false, true} {
			for _, tie := range []bool{false, true} {
				for _, public := range []bool{false, true} {
					for _, binding := range []bool{false, true} {
						for _, configured := range []bool{false, true} {
							f := RoutingFacts{Blocked: blocked, AcceptedTie: tie, PublicPerson: public, CurrentBinding: binding, Configured: configured, Incoming: mode, ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(time.Hour)}
							want := Block
							if !blocked {
								if tie {
									want = Allow
								} else if public && binding {
									if !configured {
										want = Request
									} else if mode == Request || mode == Screen {
										want = mode
									}
								}
							}
							if got := Route(f, now); got != want {
								t.Fatalf("facts=%+v got=%s want=%s", f, got, want)
							}
						}
					}
				}
			}
		}
	}
}
func TestMessagePolicyUnitPolicyExpiryAndFutureFailClosed(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, mode := range []Disposition{Request, Screen, Block} {
		for _, delta := range []time.Duration{-time.Second, 0, time.Second} {
			f := RoutingFacts{PublicPerson: true, CurrentBinding: true, Configured: true, Incoming: mode, ValidFrom: now.Add(-time.Hour), ExpiresAt: now.Add(delta)}
			want := Block
			if delta > 0 {
				want = mode
			}
			if Route(f, now) != want {
				t.Fatal("expiry must use supplied current native time")
			}
			f.ValidFrom = now.Add(time.Second)
			if Route(f, now) != Block {
				t.Fatal("future policy granted route")
			}
			f.AcceptedTie = true
			if Route(f, now) != Allow {
				t.Fatal("incoming-only preference must not revoke accepted Tie")
			}
			f.Blocked = true
			if Route(f, now) != Block {
				t.Fatal("Block must override accepted Tie")
			}
		}
	}
	if Route(RoutingFacts{AcceptedTie: true}, time.Time{}) != Block {
		t.Fatal("missing native clock")
	}
}
func TestMessagePolicyUnitInputAndWireResponseBounds(t *testing.T) {
	now := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	for _, mode := range []Disposition{Request, Screen, Block} {
		if !ValidInput(PutInput{IncomingRequests: mode, ExpiresAt: now}) {
			t.Fatal("valid incoming")
		}
	}
	for _, in := range []PutInput{{ExpectedVersion: -1, IncomingRequests: Request, ExpiresAt: now}, {ExpectedVersion: 9223372036854775807, IncomingRequests: Request, ExpiresAt: now}, {IncomingRequests: Allow, ExpiresAt: now}, {IncomingRequests: Request}, {IncomingRequests: Screen, ExpiresAt: now.Add(time.Nanosecond)}} {
		if ValidInput(in) {
			t.Fatal("invalid input accepted")
		}
	}
	if !ValidSourceVersion(strings.Repeat("a", 64)) || ValidSourceVersion(strings.Repeat("A", 64)) || ValidSourceVersion(strings.Repeat("a", 63)) {
		t.Fatal("source version format")
	}
	for _, mode := range []Disposition{Allow, Request, Screen, Block} {
		d := Decision{Schema: Schema, Disposition: mode, SourceVersion: strings.Repeat("b", 64), ObservedAt: now, ValidUntil: now.Add(time.Second), Authority: Authority}
		if mode == Block {
			d.SourceVersion = ""
		}
		if !ValidDecision(d) {
			t.Fatal("real shape rejected")
		}
		d.AutomaticAcceptance = true
		if ValidDecision(d) {
			t.Fatal("route granted acceptance")
		}
		d.AutomaticAcceptance = false
		d.ScreeningAvailable = true
		if ValidDecision(d) {
			t.Fatal("no screening adapter exists")
		}
	}
	from, until := now.Add(-time.Hour), now.Add(time.Hour)
	r := Record{Schema: Schema, OwnerID: "owner", AgentID: "agent", Configured: true, NativeRevision: 1, Status: "ACTIVE", IncomingRequests: Screen, ObservedAt: now, ValidFrom: &from, ExpiresAt: &until}
	if !ValidRecord(r, "owner") || ValidRecord(r, "foreign") {
		t.Fatal("exact returned owner")
	}
	r.Status = "UNCONFIGURED"
	if ValidRecord(r, "owner") {
		t.Fatal("configured record cannot claim default")
	}
	r = Record{Schema: Schema, OwnerID: "owner", AgentID: "agent", Status: "UNCONFIGURED", IncomingRequests: Request, ObservedAt: now}
	if !ValidRecord(r, "owner") {
		t.Fatal("legacy unconfigured read")
	}
	r.IncomingRequests = Allow
	if ValidRecord(r, "owner") {
		t.Fatal("configuration cannot confer ALLOW")
	}
}
