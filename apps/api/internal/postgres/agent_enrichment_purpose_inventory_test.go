package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	aep "github.com/birdtie/birdtie/apps/api/internal/agentenrichmentpurpose"
	"github.com/jackc/pgx/v5"
)

// Actual same-Tx native helper with pgx contract spies; SQL is not executed.
type enrichmentInventoryUnitTx struct {
	pgx.Tx
	at, final time.Time
	raw       []byte
	alive     bool
	err       error
	calls     []string
	args      [][]any
}

func (x *enrichmentInventoryUnitTx) QueryRow(_ context.Context, q string, a ...any) pgx.Row {
	x.calls = append(x.calls, q)
	x.args = append(x.args, a)
	if q == enrichmentInventorySQL {
		return currentReadonlyUnitRow{values: []any{x.at, x.raw}, err: x.err}
	}
	if strings.Contains(q, "se.expires_at>clock.at") {
		return currentReadonlyUnitRow{values: []any{x.final, x.alive}}
	}
	return currentReadonlyUnitRow{err: errors.New("UNEXPECTED_UNIT_SQL")}
}
func inventoryPGFixture(count int) (*Store, *enrichmentInventoryUnitTx, contextBuilderBinding) {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	b := contextBuilderBinding{owner: "49000000-0000-4000-8000-000000000001", agent: "49000000-0000-4000-8000-000000000004", session: "49000000-0000-4000-8000-000000000007", limit: at.Add(time.Minute)}
	gs := make([]aep.Grant, 0, count)
	for i := 0; i < count; i++ {
		gs = append(gs, aep.Grant{SchemaVersion: aep.Schema, ID: fmt.Sprintf("49000000-0000-4000-8000-%012d", i+100), PreviewID: "49000000-0000-4000-8000-000000000003", Purpose: aep.Purpose, Owner: actorref.PrincipalRef{Type: actorref.Person, ID: b.owner}, AgentID: b.agent, Revision: 1, CreatedAt: at.Add(-time.Minute), ExpiresAt: at.Add(time.Minute), ObservedAt: at, Selection: aep.Selection{TaskID: "49000000-0000-4000-8000-000000000005", MomentID: "49000000-0000-4000-8000-000000000006", MomentRevision: 2, Fields: []string{"title"}, DeadlineAt: at.Add(time.Minute)}})
	}
	raw, _ := json.Marshal(gs)
	return &Store{}, &enrichmentInventoryUnitTx{at: at, final: at.Add(time.Millisecond), raw: raw, alive: true}, b
}
func TestEnrichmentInventoryNativeHelperBoundedCurrentSnapshot(t *testing.T) {
	for _, n := range []int{0, 1, 51} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, x, b := inventoryPGFixture(n)
			v, e := s.listEnrichmentInventoryTx(context.Background(), x, b)
			if e != nil {
				t.Fatal(e)
			}
			want := n
			if want > 50 {
				want = 50
			}
			if len(v.Grants) != want || v.Truncated != (n == 51) || !v.ObservedAt.Equal(x.at) || len(x.calls) != 2 {
				t.Fatal("not original bounded snapshot/final clock", v)
			}
			if x.args[0][0] != b.owner || x.args[0][1] != b.agent || x.args[1][0] != b.session || x.args[1][1] != b.owner || x.args[1][2] != b.agent {
				t.Fatal("same native identity lost")
			}
			for _, required := range []string{"g.recipient_account_id=$1", "p.owner_id=$1 AND p.agent_id=$2", "g.resource_id=p.id::text", "g.purpose='MOMENT_LOCAL_ANALYSIS'", "g.actions=ARRAY['analyze_local']", "LIMIT 51"} {
				if !strings.Contains(x.calls[0], required) {
					t.Fatal("fixed native qualification missing", required)
				}
			}
			for _, forbidden := range []string{"FOR UPDATE", "p.review", "'review'", "source_binding", "task_binding", "authority", "taskQuery"} {
				if strings.Contains(x.calls[0], forbidden) {
					t.Fatal("inventory reads body or upgrades locks", forbidden)
				}
			}
		})
	}
}
func TestEnrichmentInventoryNativeHelperFinalDenialNotFakeEmpty(t *testing.T) {
	for _, name := range []string{"SQL failure", "corrupt metadata", "final revoked Session", "late clock", "clock backwards", "corrupt 51st row"} {
		t.Run(name, func(t *testing.T) {
			s, x, b := inventoryPGFixture(1)
			want := aep.ErrUnavailable
			switch name {
			case "SQL failure":
				x.err = errors.New("PRIVATE_UNIT_CANARY")
			case "corrupt metadata":
				x.raw = []byte("null")
			case "final revoked Session":
				x.alive = false
				want = aep.ErrDenied
			case "late clock":
				x.final = x.at.Add(30 * time.Second)
			case "clock backwards":
				x.final = x.at.Add(-time.Second)
			case "corrupt 51st row":
				s, x, b = inventoryPGFixture(51)
				var g []aep.Grant
				_ = json.Unmarshal(x.raw, &g)
				g[50].Purpose = "MODEL_EGRESS"
				x.raw, _ = json.Marshal(g)
			}
			v, e := s.listEnrichmentInventoryTx(context.Background(), x, b)
			if !errors.Is(e, want) || v.SchemaVersion != "" {
				t.Fatal("native error fabricated authorized empty", v, e)
			}
		})
	}
}

func TestEnrichmentInventoryNativeActiveMetadataPriority(t *testing.T) {
	s, x, b := inventoryPGFixture(1)
	if _, e := s.listEnrichmentInventoryTx(context.Background(), x, b); e != nil {
		t.Fatal(e)
	}
	// UNIT_STATIC/pgx spy: inspect the SQL actually passed by this helper;
	// this does not execute PostgreSQL or establish a sorting/clock guarantee.
	q := x.calls[0]
	for _, part := range []string{
		"(g.revoked_at IS NULL AND g.expires_at>clk.at) current_permit",
		"CROSS JOIN clk",
		"ORDER BY current_permit DESC,g.created_at DESC,g.id LIMIT 51",
		"ORDER BY s.current_permit DESC,s.created_at DESC,s.id",
	} {
		if !strings.Contains(q, part) {
			t.Fatal("older active metadata may be hidden by withdrawn/expired history", part)
		}
	}
	if strings.Contains(q, "'currentPermit'") || strings.Contains(q, "'canAnalyze'") {
		t.Fatal("inventory priority must not export a new analysis authorization")
	}
}
