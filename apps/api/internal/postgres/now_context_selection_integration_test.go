package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	ncs "github.com/birdtie/birdtie/apps/api/internal/nowcontextselection"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

type nowSelectionFixture struct {
	p      *agentPrivateFixture
	g      *nowContextSelection
	a      ncs.Access
	cities []string
	online string
}

func nowSelectionNative(t *testing.T) *nowSelectionFixture {
	t.Helper()
	ownedMigrationDatabase(t)
	p := agentPrivateTestFixture(t)
	b := p.base
	actor, e := b.store.Authenticate(b.ctx, p.owner.SessionDigest)
	if e != nil {
		t.Fatal(e)
	}
	port, e := NewNowContextSelection(b.store)
	if e != nil {
		t.Fatal(e)
	}
	f := &nowSelectionFixture{p: p, g: port.(*nowContextSelection), a: ncs.Access{Actor: actor, Digest: p.owner.SessionDigest}}
	key := "now006-" + strings.ReplaceAll(b.person.ID, "-", "")
	f.cities = []string{key + "-current", key + "-destination"}
	f.online = key + "-online"
	for _, id := range f.cities {
		b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,$2,'LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:NOW006','合成维护者',$3)`, id, "合成城市"+id[len(id)-8:], b.other.ID)
		b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, id)
	}
	for _, v := range []contextgraph.DeclarationInput{{Type: contextgraph.City, SourceKey: f.cities[0], Relation: "current"}, {Type: contextgraph.City, SourceKey: f.cities[1], Relation: "destination"}, {Type: contextgraph.City, SourceKey: f.cities[0], Relation: "past"}, {Type: contextgraph.Online, SourceKey: f.online, Relation: "interest"}, {Type: contextgraph.Institution, SourceKey: key + "-school", Relation: "past"}} {
		if _, e = b.store.DeclareContext(b.ctx, b.person.ID, v); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = b.store.DeclareContext(b.ctx, b.other.ID, contextgraph.DeclarationInput{Type: contextgraph.Online, SourceKey: key + "-other-private", Relation: "interest"}); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, q := range []string{`DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, `DELETE FROM contexts WHERE city_id IN(SELECT id FROM cities WHERE maintainer_account_id=ANY($1::uuid[])) OR online_key LIKE 'now006-'||replace($2::text,'-','')||'%' OR institution_key LIKE 'now006-'||replace($2::text,'-','')||'%'`, `DELETE FROM city_contexts WHERE city_id IN(SELECT id FROM cities WHERE maintainer_account_id=ANY($1::uuid[]))`, `DELETE FROM cities WHERE maintainer_account_id=ANY($1::uuid[])`} {
			var e error
			if strings.Contains(q, "$2") {
				_, e = b.pool.Exec(ctx, q, b.accounts, b.person.ID)
			} else {
				_, e = b.pool.Exec(ctx, q, b.accounts)
			}
			if e != nil {
				t.Error("NOW006 owned cleanup", e)
			}
		}
	})
	return f
}
func (f *nowSelectionFixture) read(t *testing.T) ncs.OptionsReceipt {
	t.Helper()
	out, e := f.g.ReadOptions(f.p.base.ctx, f.a)
	if e != nil {
		tx, err := f.p.base.pool.Begin(f.p.base.ctx)
		if err == nil {
			defer tx.Rollback(context.Background())
			var a, b, c, d, e, timestamp, g any
			err = tx.QueryRow(f.p.base.ctx, nowSelectionSQL, f.a.Actor.ID, f.a.Digest[:], false).Scan(&a, &b, &c, &d, &e, &timestamp, &g)
		}
		t.Fatal("read native options", e, "SQL diagnostic", err)
	}
	if ncs.ValidateOptions(out.Response, f.a.Actor.ID) != nil {
		t.Fatal("bad native DTO")
	}
	return out
}
func TestNowSelectionNativeReadResolveNoDomainWritesAndTypedModes(t *testing.T) {
	f := nowSelectionNative(t)
	b := f.p.base
	before := enrichmentAllPublic(t, b.pool, b.ctx)
	o := f.read(t)
	modes := map[string]bool{}
	for _, v := range o.Response.Items {
		if strings.Contains(v.Label, "other-private") {
			t.Fatal("foreign private context leaked")
		}
		if v.Declared {
			modes[v.ViewMode] = true
		}
		s, e := f.g.Resolve(b.ctx, f.a, ncs.Input{OptionsToken: o.Response.OptionsToken, OptionID: v.OptionID})
		if e != nil || s.Response.Choice != v || s.Response.Owner.ID != f.a.Actor.ID {
			t.Fatal("view resolution", e)
		}
		if e = f.g.RevalidateSelection(b.ctx, f.a, s); e != nil {
			t.Fatal(e)
		}
	}
	for _, mode := range []string{"CURRENT", "DESTINATION", "PAST", "ONLINE"} {
		if !modes[mode] {
			t.Fatal("lost real mode", mode)
		}
	}
	if e := f.g.RevalidateOptions(b.ctx, f.a, o); e != nil {
		t.Fatal(e)
	}
	if enrichmentAllPublic(t, b.pool, b.ctx) != before {
		t.Fatal("GET/resolve/revalidation wrote complete public domain")
	}
}
func TestNowSelectionNativeSourceSessionABAExpiryAndRestart(t *testing.T) {
	for _, kind := range []string{"context-ABA", "declaration-ABA", "city-ABA", "metadata-ABA", "agent-retired", "session-revoked", "session-replaced", "expired-options", "wrong-owner", "restart", "forged-response", "hidden-city"} {
		t.Run(kind, func(t *testing.T) {
			f := nowSelectionNative(t)
			b := f.p.base
			o := f.read(t)
			choice := o.Response.Items[0]
			switch kind {
			case "context-ABA":
				b.exec(`UPDATE contexts SET online_key=online_key||'-changed' WHERE online_key=$1`, f.online)
				b.exec(`UPDATE contexts SET online_key=$1 WHERE online_key=$1||'-changed'`, f.online)
			case "declaration-ABA":
				b.exec(`DELETE FROM person_contexts WHERE person_account_id=$1 AND context_id=(SELECT id FROM contexts WHERE online_key=$2)`, f.a.Actor.ID, f.online)
				if _, e := b.store.DeclareContext(b.ctx, f.a.Actor.ID, contextgraph.DeclarationInput{Type: contextgraph.Online, SourceKey: f.online, Relation: "interest"}); e != nil {
					t.Fatal(e)
				}
			case "city-ABA":
				b.exec(`UPDATE cities SET name=name||'-changed' WHERE id=$1`, f.cities[0])
				b.exec(`UPDATE cities SET name=replace(name,'-changed','') WHERE id=$1`, f.cities[0])
			case "metadata-ABA":
				b.exec(`UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, b.personID)
			case "agent-retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			case "session-revoked":
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.Digest[:])
			case "session-replaced":
				f.a.Digest = f.p.peer.SessionDigest
			case "expired-options":
				p, e := f.g.open(o.Response.OptionsToken)
				if e != nil {
					t.Fatal(e)
				}
				p.ExpiresAt = time.Now().Add(-time.Second)
				o.Response.OptionsToken, e = f.g.seal(p)
				if e != nil {
					t.Fatal(e)
				}
			case "wrong-owner":
				a, e := b.store.Authenticate(b.ctx, f.p.peer.SessionDigest)
				if e != nil {
					t.Fatal(e)
				}
				f.a = ncs.Access{Actor: a, Digest: f.p.peer.SessionDigest}
			case "restart":
				g, e := NewNowContextSelection(b.store)
				if e != nil {
					t.Fatal(e)
				}
				f.g = g.(*nowContextSelection)
			case "hidden-city":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.cities[0])
			case "forged-response":
				o.Response.Items[0].Label = "伪造私密内容"
				if e := f.g.RevalidateOptions(b.ctx, f.a, o); !errors.Is(e, ncs.ErrDenied) {
					t.Fatal("forged DTO accepted", e)
				}
				return
			}
			if _, e := f.g.Resolve(b.ctx, f.a, ncs.Input{OptionsToken: o.Response.OptionsToken, OptionID: choice.OptionID}); e == nil {
				t.Fatal("stale authority/source accepted", kind)
			}
			if e := f.g.RevalidateOptions(b.ctx, f.a, o); e == nil {
				t.Fatal("stale response accepted", kind)
			}
		})
	}
}
func TestNowSelectionNativeRealFinalSessionWait(t *testing.T) {
	for _, kind := range []string{"revoke", "natural-idle-expiry", "context-ABA", "city-hidden", "agent-retired"} {
		t.Run(kind, func(t *testing.T) {
			f := nowSelectionNative(t)
			b := f.p.base
			o := f.read(t)
			if kind == "natural-idle-expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '400 milliseconds' WHERE token_sha256=$1`, f.a.Digest[:])
			}
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if _, e = held.Exec(b.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, f.a.Digest[:]); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				_, e := f.g.Resolve(b.ctx, f.a, ncs.Input{OptionsToken: o.Response.OptionsToken, OptionID: o.Response.Items[0].OptionID})
				done <- e
			}()
			seen := false
			for until := time.Now().Add(3 * time.Second); time.Now().Before(until); {
				var n int
				if e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM sessions%FOR SHARE%'`).Scan(&n); e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					seen = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !seen {
				t.Fatal("native actual final Session lock wait absent")
			}
			switch kind {
			case "revoke":
				if _, e = held.Exec(b.ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, f.a.Digest[:]); e != nil {
					t.Fatal(e)
				}
			case "natural-idle-expiry":
				time.Sleep(500 * time.Millisecond)
			case "context-ABA":
				b.exec(`UPDATE contexts SET online_key=online_key||'-changed' WHERE online_key=$1`, f.online)
				b.exec(`UPDATE contexts SET online_key=$1 WHERE online_key=$1||'-changed'`, f.online)
			case "city-hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, f.cities[0])
			case "agent-retired":
				b.exec(`UPDATE agents SET status='retired' WHERE id=$1`, b.personID)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-done; e == nil {
				t.Fatal("final wait released stale selection", kind)
			}
		})
	}
}
func TestNowSelectionNativeReceiptCannotSerialize(t *testing.T) {
	if _, e := json.Marshal(ncs.OptionsReceipt{}); !errors.Is(e, ncs.ErrDenied) {
		t.Fatal(e)
	}
}

func TestNowSelectionNativeDeclaredCommunityACLAndNativeDeadline(t *testing.T) {
	for _, kind := range []string{"declared-private", "hidden", "expired", "blocked", "native-expiry-after-final-wait", "block-after-final-wait"} {
		t.Run(kind, func(t *testing.T) {
			ownedMigrationDatabase(t)
			f := interestNative(t)
			f.approve(f.preview("PRIVATE"))
			actor, e := f.s.Authenticate(f.ctx, f.a.SessionDigest)
			if e != nil {
				t.Fatal(e)
			}
			a := ncs.Access{Actor: actor, Digest: f.a.SessionDigest}
			port, e := NewNowContextSelection(f.s)
			if e != nil {
				t.Fatal(e)
			}
			g := port.(*nowContextSelection)
			if kind == "hidden" {
				f.exec(`UPDATE communities SET publication_status='hidden' WHERE id=$1`, f.group)
			}
			if kind == "expired" {
				f.exec(`UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.group)
			}
			if kind == "blocked" {
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.Actor.ID, f.owner)
			}
			if kind == "native-expiry-after-final-wait" {
				f.exec(`UPDATE communities SET expires_at=clock_timestamp()+interval '400 milliseconds' WHERE id=$1`, f.group)
			}
			before := f.snapshot()
			o, e := g.ReadOptions(f.ctx, a)
			if e != nil {
				t.Fatal(e)
			}
			var choice *ncs.Option
			for _, x := range o.Response.Items {
				if x.ContextType == "COMMUNITY" {
					v := x
					choice = &v
				}
			}
			if kind == "hidden" || kind == "expired" || kind == "blocked" {
				if choice != nil {
					t.Fatal("denied community private label emitted")
				}
				if before != f.snapshot() {
					t.Fatal("denied source reader wrote domain")
				}
				return
			}
			if choice == nil || choice.QueryRoute != "UNAVAILABLE" || !choice.Declared || choice.Relation != "interest" {
				t.Fatal("missing native private self declaration or invented Community query")
			}
			if kind == "declared-private" {
				v, e := g.Resolve(f.ctx, a, ncs.Input{OptionsToken: o.Response.OptionsToken, OptionID: choice.OptionID})
				if e != nil || v.Response.Choice != *choice {
					t.Fatal(e)
				}
				if before != f.snapshot() {
					t.Fatal("view selected wrote membership/declaration/audit")
				}
				return
			}
			held, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if _, e = held.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, a.Digest[:]); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() {
				_, e := g.Resolve(f.ctx, a, ncs.Input{OptionsToken: o.Response.OptionsToken, OptionID: choice.OptionID})
				done <- e
			}()
			seen := false
			for until := time.Now().Add(2 * time.Second); time.Now().Before(until); {
				var n int
				if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM sessions%FOR SHARE%'`).Scan(&n); e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					seen = true
					break
				}
				time.Sleep(10 * time.Millisecond)
			}
			if !seen {
				t.Fatal("real session wait missing")
			}
			if kind == "native-expiry-after-final-wait" {
				time.Sleep(500 * time.Millisecond)
			} else {
				f.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, a.Actor.ID, f.owner)
			}
			if e = held.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			if e = <-done; e == nil {
				t.Fatal("source final deadline/block leaked selection")
			}
		})
	}
}
func TestNowSelectionNativeOptionProcessRestart(t *testing.T) {
	f := nowSelectionNative(t)
	o := f.read(t)
	child := exec.CommandContext(f.p.base.ctx, os.Args[0], "-test.run=^TestNowSelectionProcessKeyHelper$", "-test.v")
	child.Env = append(os.Environ(), "BIRDTIE_NOW006_OLD_OPTIONS="+o.Response.OptionsToken)
	raw, e := child.CombinedOutput()
	if e != nil || !strings.Contains(string(raw), "restarted-process-refused-old-view-token") {
		t.Fatal("actual OS key restart boundary", e, string(raw))
	}
}
func TestNowSelectionProcessKeyHelper(t *testing.T) {
	token := os.Getenv("BIRDTIE_NOW006_OLD_OPTIONS")
	if token == "" {
		return
	}
	port, e := NewNowContextSelection(nil)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = port.(*nowContextSelection).open(token); !errors.Is(e, ncs.ErrConflict) {
		t.Fatal("old key accepted", e)
	}
	t.Log("restarted-process-refused-old-view-token")
}
