package postgres

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
)

type humanRecoveryForwardUnit struct {
	aa.Port // Every unimplemented execution method panics if invoked.
	access  agentevent.Access
	flags   *agentfeature.Controller
	id      string
	calls   int
	d       aa.Dispatch
	err     error
	after   func()
}

func (p *humanRecoveryForwardUnit) ReconcileOwnSandboxApproval(_ context.Context, a agentevent.Access, id string, c *agentfeature.Controller) (aa.Dispatch, error) {
	p.calls++
	p.access = a
	p.id = id
	p.flags = c
	if p.after != nil {
		p.after()
	}
	return p.d, p.err
}
func sandboxRecoveryUnitFlags(t *testing.T, on bool) *agentfeature.Controller {
	t.Helper()
	config := agentfeature.DefaultConfig()
	if on {
		var e error
		config, e = agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
		if e != nil {
			t.Fatal(e)
		}
	}
	c, e := agentfeature.NewController(config)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestSandboxRecoveryHumanNativeGatewayOnlyOriginalServiceAndController(t *testing.T) {
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "38000000-0000-4000-8000-000000000001"}, SessionDigest: [32]byte{1, 2, 3}}
	id := "38000000-0000-4000-8000-000000000003"
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	p := &humanRecoveryForwardUnit{d: aa.Dispatch{Schema: aa.Schema, ID: "38000000-0000-4000-8000-000000000002", ApprovalID: id, TenantID: a.WorkspacePrincipal.ID, EffectKey: strings.Repeat("a", 64), State: aa.NoEffect, CommittedAt: at}}
	c := sandboxRecoveryUnitFlags(t, true)
	g := &humanSandboxRecovery{service: aa.NewService(p), flags: c}
	first, e := g.RecoverOwn(context.Background(), a, id)
	if e != nil || first.Status != aa.NoEffect {
		t.Fatal(e, first)
	}
	second, e := g.RecoverOwn(context.Background(), a, id)
	if e != nil || second != first || p.calls != 2 || p.access.SessionDigest != a.SessionDigest || p.flags != c || p.id != id {
		t.Fatal("not original receipt-only forwarding", e, p.calls)
	}
	p.err = aa.ErrUnknown
	if _, e = g.RecoverOwn(context.Background(), a, id); !errors.Is(e, aa.ErrUnknown) || p.calls != 3 {
		t.Fatal("unknown converted or retried", e, p.calls)
	}
	p.err = nil
	p.after = func() { _ = c.Disable(agentfeature.Enrichment) }
	if _, e = g.RecoverOwn(context.Background(), a, id); !errors.Is(e, aa.ErrUnknown) {
		t.Fatal("late disabled result published", e)
	}
	before := p.calls
	if _, e = g.RecoverOwn(context.Background(), a, id); !errors.Is(e, aa.ErrUnavailable) || p.calls != before {
		t.Fatal("disabled controller reached port", e)
	}
}
func TestSandboxRecoveryHumanNativeGatewayMissingAndDefaultOffStayClosed(t *testing.T) {
	a := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: "38000000-0000-4000-8000-000000000001"}, SessionDigest: [32]byte{1}}
	for _, c := range []*agentfeature.Controller{nil, sandboxRecoveryUnitFlags(t, false), sandboxRecoveryUnitFlags(t, true)} {
		if v, e := NewHumanSandboxRecovery(nil, c).RecoverOwn(context.Background(), a, "38000000-0000-4000-8000-000000000003"); !errors.Is(e, aa.ErrUnavailable) || v.SchemaVersion != "" {
			t.Fatal("missing nativeStore supplied fake receipt", e, v)
		}
	}
}
