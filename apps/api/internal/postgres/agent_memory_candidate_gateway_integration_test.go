package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemorycandidate"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestMemoryCandidateGatewayNativeHumanLifecycle(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	g := NewMemoryCandidateHumanGateway(b.store, s.flags)
	d := memoryCandidateDraft(t, f)
	r, e := g.Save(b.ctx, f.base.private.owner, agentmemorycandidate.HumanDraft{Category: d.Category, Sources: d.Sources, ValidUntil: d.ValidUntil})
	if e != nil {
		t.Fatal(e)
	}
	if r.Assessment.Value != nil || r.Assessment.Level != "LOW" {
		t.Fatal("fake probability", r)
	}
	retry, e := g.Save(b.ctx, f.base.private.owner, agentmemorycandidate.HumanDraft{Category: d.Category, Sources: d.Sources, ValidUntil: d.ValidUntil})
	if e != nil || !reflect.DeepEqual(r, retry) {
		t.Fatal("intent retry", e)
	}
	listed, e := g.List(b.ctx, f.base.private.owner)
	if e != nil || len(listed) != 1 || !reflect.DeepEqual(listed[0], r) {
		t.Fatal("list", e, listed)
	}
	in := agentmemorycandidate.HumanPreviewInput{PreviewID: strings.Repeat("a", 64), ExpectedVersion: r.Version, MemoryValidUntil: d.ValidUntil}
	p, e := g.Preview(b.ctx, f.base.private.owner, r.ID, in)
	if e != nil {
		t.Fatal(e)
	}
	again, e := g.Preview(b.ctx, f.base.private.owner, r.ID, in)
	if e != nil || !reflect.DeepEqual(p, again) {
		t.Fatal("preview renewed", e)
	}
	var memories int
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, b.personID).Scan(&memories) != nil || memories != 0 {
		t.Fatal("preview wrote")
	}
	for i := 0; i < 10; i++ {
		accepted, e := g.Accept(b.ctx, f.base.private.owner, r.ID, p.PreviewID)
		if e != nil || accepted.Status != agentmemorycandidate.Active || accepted.MemoryID == nil || *accepted.MemoryID != p.Review.TargetMemoryID {
			t.Fatal("native accept", e)
		}
	}
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memories WHERE agent_id=$1 AND source_type='EXPLICIT' AND visibility='PRIVATE'`, b.personID).Scan(&memories) != nil || memories != 1 {
		t.Fatal("duplicate/nonexplicit")
	}
	restarted := NewMemoryCandidateHumanGateway(b.store, s.flags)
	if _, e = restarted.Accept(b.ctx, f.base.private.owner, r.ID, p.PreviewID); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("restart resurrected preview", e)
	}
	authoritative, e := restarted.Read(b.ctx, f.base.private.owner, r.ID)
	if e != nil || authoritative.Status != agentmemorycandidate.Active {
		t.Fatal("unknown receipt", e)
	}
}
func TestMemoryCandidateGatewayNativeNegativeBoundaries(t *testing.T) {
	for _, name := range []string{"off", "nil", "foreign", "changedParams", "sourceEdit", "revoke", "flagABA", "sameActivity", "reject", "expiryKey"} {
		t.Run(name, func(t *testing.T) {
			f, s := memoryCandidateFixture(t)
			b := f.base.private.base
			g := NewMemoryCandidateHumanGateway(b.store, s.flags)
			if name == "off" || name == "nil" {
				var flags *agentfeature.Controller
				if name == "off" {
					flags, _ = agentfeature.NewController(agentfeature.DefaultConfig())
				}
				g = NewMemoryCandidateHumanGateway(b.store, flags)
				if _, e := g.List(b.ctx, f.base.private.owner); !errors.Is(e, agentmemory.ErrUnavailable) {
					t.Fatal(e)
				}
				return
			}
			r := memoryCandidateSave(t, f, s)
			in := agentmemorycandidate.HumanPreviewInput{PreviewID: strings.Repeat("b", 64), ExpectedVersion: r.Version, MemoryValidUntil: r.ValidUntil}
			if name == "sameActivity" {
				d := memoryCandidateDraft(t, f)
				d.Sources = d.Sources[:1]
				r, e := g.Save(b.ctx, f.base.private.owner, agentmemorycandidate.HumanDraft{Category: "badminton", Sources: d.Sources, ValidUntil: d.ValidUntil})
				if e != nil {
					t.Fatal(e)
				}
				if _, e = g.Preview(b.ctx, f.base.private.owner, r.ID, in); e == nil {
					t.Fatal("single cluster accepted")
				}
				return
			}
			if name == "expiryKey" {
				if _, e := b.pool.Exec(b.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '600 milliseconds' WHERE id=$1`, f.base.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			}
			p, e := g.Preview(b.ctx, f.base.private.owner, r.ID, in)
			if e != nil {
				t.Fatal(e)
			}
			a := f.base.private.owner
			switch name {
			case "foreign":
				a = f.base.private.peer
			case "changedParams":
				in.MemoryValidUntil = in.MemoryValidUntil.Add(time.Second)
				if _, e = g.Preview(b.ctx, a, r.ID, in); !errors.Is(e, agentmemory.ErrConflict) {
					t.Fatal(e)
				}
				return
			case "sourceEdit":
				if _, e = b.pool.Exec(b.ctx, `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp(),body='真实原生改动' WHERE id=$1`, f.sources[agentevent.MomentCreated]); e != nil {
					t.Fatal(e)
				}
			case "revoke":
				if _, e = b.pool.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.base.private.ownerSession); e != nil {
					t.Fatal(e)
				}
			case "flagABA":
				_ = s.flags.Disable(agentfeature.Memory)
				cfg, err := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":true,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
				if err != nil {
					t.Fatal(err)
				}
				if err = s.flags.Replace(s.flags.Revision(), cfg); err != nil {
					t.Fatal(err)
				}
			case "reject":
				if _, e = g.Reject(b.ctx, a, r.ID, r.Version); e != nil {
					t.Fatal(e)
				}
			case "expiryKey":
				time.Sleep(700 * time.Millisecond)
				if _, e = g.Preview(b.ctx, a, r.ID, in); !errors.Is(e, agentmemory.ErrConflict) {
					t.Fatal("expired key renewed", e)
				}
			}
			if _, e = g.Accept(b.ctx, a, r.ID, p.PreviewID); e == nil {
				t.Fatal("invalid authority accepted")
			}
			var n int
			if b.pool.QueryRow(context.Background(), `SELECT count(*) FROM agent_memories WHERE agent_id=$1`, b.personID).Scan(&n) != nil || n != 0 {
				t.Fatal("denied effect")
			}
		})
	}
}
func TestMemoryCandidateGatewayNativeBoundedFifty(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	g := NewMemoryCandidateHumanGateway(b.store, s.flags)
	d := memoryCandidateDraft(t, f)
	ids := []string{}
	for i := 0; i < 51; i++ {
		m, e := b.store.CreateMomentDraft(b.ctx, b.person.ID, content.MomentInput{CityID: "aberdeen-gb", Title: "独占合成候选来源", TimePrecision: "unknown", LocationPrecision: "city"})
		if e != nil {
			t.Fatal(e)
		}
		r, e := g.Save(b.ctx, f.base.private.owner, agentmemorycandidate.HumanDraft{Category: "sports", Sources: []agentmemorycandidate.Selector{{Type: agentevent.MomentSource, ID: m.ID}, {Type: agentevent.SavedPlaceSource, ID: f.sources[agentevent.PlaceSaved]}}, ValidUntil: d.ValidUntil})
		if e != nil {
			t.Fatal(e)
		}
		ids = append(ids, r.ID)
	}
	list, e := g.List(b.ctx, f.base.private.owner)
	if e != nil || len(list) != 50 {
		t.Fatal("bounded actual list", e, len(list))
	}
	if list[0].ID != ids[50] || list[49].ID != ids[1] {
		t.Fatal("actual stable order")
	}
	var n int
	if b.pool.QueryRow(b.ctx, `SELECT count(*) FROM agent_memory_candidates WHERE agent_id=$1`, b.personID).Scan(&n) != nil || n != 51 {
		t.Fatal("list removed history")
	}
}
func TestMemoryCandidateGatewayNativeListRefreshAndIsolation(t *testing.T) {
	f, s := memoryCandidateFixture(t)
	b := f.base.private.base
	g := NewMemoryCandidateHumanGateway(b.store, s.flags)
	r := memoryCandidateSave(t, f, s)
	other, e := g.List(b.ctx, f.base.private.peer)
	if e != nil || len(other) != 0 {
		t.Fatal("foreign list", e)
	}
	if _, e = b.pool.Exec(b.ctx, `UPDATE moments SET revision=revision+1,updated_at=clock_timestamp(),body='来源版本变更' WHERE id=$1`, f.sources[agentevent.MomentCreated]); e != nil {
		t.Fatal(e)
	}
	list, e := g.List(b.ctx, f.base.private.owner)
	if e != nil || len(list) != 1 || list[0].ID != r.ID || list[0].Status != agentmemorycandidate.Expired || list[0].Category != "" || len(list[0].Sources) != 0 {
		t.Fatal("list stale body", e, list)
	}
}
