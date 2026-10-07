package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	acb "github.com/birdtie/birdtie/apps/api/internal/agentcontextbuilder"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/jackc/pgx/v5"
	"strings"
	"testing"
	"time"
)

type taskInventoryTx struct {
	pgx.Tx
	at, final time.Time
	raw       []byte
	alive     bool
	err       error
	calls     []string
	args      [][]any
}

func (x *taskInventoryTx) QueryRow(_ context.Context, q string, args ...any) pgx.Row {
	x.calls = append(x.calls, q)
	x.args = append(x.args, args)
	if q == taskContextInventorySQL {
		return currentReadonlyUnitRow{values: []any{x.at, x.raw}, err: x.err}
	}
	if strings.Contains(q, "se.expires_at>clock.at") {
		return currentReadonlyUnitRow{values: []any{x.final, x.alive}}
	}
	return currentReadonlyUnitRow{err: errors.New("UNEXPECTED_UNIT_SQL")}
}
func taskInventoryPG(n int) (*Store, *taskInventoryTx, contextBuilderBinding) {
	at := time.Date(2026, 10, 7, 0, 0, 0, 0, time.UTC)
	b := contextBuilderBinding{owner: "81000000-0000-4000-8000-000000000001", agent: "81000000-0000-4000-8000-000000000002", session: "81000000-0000-4000-8000-000000000009", limit: at.Add(time.Minute)}
	gs := []acb.PurposeInventoryGrant{}
	for i := 0; i < n; i++ {
		gs = append(gs, acb.PurposeInventoryGrant{ID: fmt.Sprintf("81000000-0000-4000-8000-%012d", i+100), Revision: 1, Purpose: acb.TaskContextRead, TaskID: "81000000-0000-4000-8000-000000000004", CityID: "aberdeen", TaskUpdatedAt: at.Add(-time.Minute), ProfileFields: []string{"agentNotes"}, MemoryIDs: []string{}, PlaceIDs: []string{}, ActivityIDs: []string{}, RelationshipTieIDs: []string{}, PolicyFamilies: []agentpolicysettings.Family{}, CreatedAt: at.Add(-time.Second), ExpiresAt: at.Add(time.Minute)})
	}
	raw, _ := json.Marshal(gs)
	return &Store{}, &taskInventoryTx{at: at, final: at.Add(time.Millisecond), raw: raw, alive: true}, b
}
func TestTaskContextInventoryNativeHelperSessionBoundSnapshot(t *testing.T) {
	for _, n := range []int{0, 1, 51} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			s, x, b := taskInventoryPG(n)
			out, e := s.listTaskContextInventoryTx(context.Background(), x, b)
			want := n
			if want > 50 {
				want = 50
			}
			if e != nil || len(out.Grants) != want || out.Truncated != (n == 51) || !out.ObservedAt.Equal(x.at) || len(x.calls) != 2 {
				t.Fatal(out, e)
			}
			if x.args[0][0] != b.owner || x.args[0][1] != b.agent || x.args[0][2] != b.session || x.args[1][0] != b.session {
				t.Fatal("same native session lost")
			}
			for _, part := range []string{"WITH clk AS MATERIALIZED", "p.session_id=$3", "g.owner_account_id=$1 AND g.recipient_account_id=$1", "g.resource_id=p.id::text", "g.purpose='TASK_CONTEXT_READ'", "g.actions=ARRAY['read']", "ORDER BY current_permit DESC,g.created_at DESC,g.id LIMIT 51"} {
				if !strings.Contains(x.calls[0], part) {
					t.Fatal("actual SQL missing", part)
				}
			}
			for _, bad := range []string{"queryDigest", "currentQuery", "p.sources", "p.authority", "p.review", "FOR UPDATE"} {
				if strings.Contains(x.calls[0], bad) {
					t.Fatal("private or lock data included", bad)
				}
			}
		})
	}
}
func TestTaskContextInventoryNativeHelperRejectFinalAndCorruption(t *testing.T) {
	for _, name := range []string{"SQL", "null", "Session", "deadline", "backwards", "sentinel"} {
		t.Run(name, func(t *testing.T) {
			s, x, b := taskInventoryPG(1)
			switch name {
			case "SQL":
				x.err = errors.New("synthetic failure")
			case "null":
				x.raw = []byte("null")
			case "Session":
				x.alive = false
			case "deadline":
				x.final = x.at.Add(30 * time.Second)
			case "backwards":
				x.final = x.at.Add(-time.Second)
			case "sentinel":
				s, x, b = taskInventoryPG(51)
				var gs []acb.PurposeInventoryGrant
				_ = json.Unmarshal(x.raw, &gs)
				gs[50].Revision = 0
				x.raw, _ = json.Marshal(gs)
			}
			v, e := s.listTaskContextInventoryTx(context.Background(), x, b)
			if e == nil || v.SchemaVersion != "" {
				t.Fatal("failure disguised as empty", v, e)
			}
		})
	}
}
