package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"
)

func seedNative(t *testing.T) (*agentPrivateFixture, identity.Actor) {
	t.Helper()
	f := agentPrivateTestFixture(t)
	a, e := f.base.store.Authenticate(f.base.ctx, f.owner.SessionDigest)
	if e != nil {
		t.Fatal(e)
	}
	return f, a
}
func seedDraft(t *testing.T, r agentseed.Record) agentseed.Input {
	t.Helper()
	if len(r.Cities) == 0 {
		t.Fatal("requires actual published city")
	}
	return agentseed.Input{ExpectedSnapshot: r.Snapshot, Action: "SAVE", DisplayName: "种子昵称", CurrentCityID: r.Cities[0].ID, CurrentCitySnapshot: r.Cities[0].Snapshot, LanguagePreferences: []string{"zh-CN"}, BasicIntent: "DISCOVER_PLACES", InterestChoice: "SKIP", Interests: []string{}}
}
func seedEffects(t *testing.T, f *agentPrivateFixture) string {
	t.Helper()
	var raw []byte
	e := f.base.pool.QueryRow(f.base.ctx, `SELECT jsonb_build_object('profile',(SELECT to_jsonb(p) FROM user_profiles p WHERE account_id=$1),'private',(SELECT to_jsonb(p) FROM agent_private_profiles p WHERE owner_id=$1),'metadata',(SELECT to_jsonb(p) FROM agent_profiles p WHERE owner_id=$1),'contexts',(SELECT coalesce(jsonb_agg(to_jsonb(pc) ORDER BY pc.context_id),'[]') FROM person_contexts pc WHERE person_account_id=$1),'intent',(SELECT to_jsonb(i) FROM agent_seed_user_intents i WHERE owner_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY a.id),'[]') FROM audit_events a WHERE actor_account_id=$1))`, f.owner.WorkspacePrincipal.ID).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestAgentSeedNativeAtomicSourcesAndSkip(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	first, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	if first.Intent.Version != 0 || first.Intent.BasicIntent != "" || !first.NeedsPrompt || first.CurrentCity != nil {
		t.Fatal("unknown must stay unknown", first)
	}
	p, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	p.Fields.AgentNotes = "原私密笔记不能变成意图"
	p.Fields.PersonalPreferences = []string{"原有明确兴趣"}
	p.Fields.SocialPreferences = []string{"原有偏好"}
	_, e = b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: p.Profile.ProfileVersion, Fields: p.Fields})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, seedDraft(t, first)); !errors.Is(e, agentseed.ErrConflict) {
		t.Fatal("private source CAS", e)
	}
	first, e = b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	draft := seedDraft(t, first)
	v, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, draft)
	if e != nil {
		t.Fatal(e)
	}
	if v.Intent.Version != 1 || v.Intent.Progress != "COMPLETED" || v.Intent.BasicIntent != "DISCOVER_PLACES" || v.CurrentCity == nil || v.NeedsPrompt || v.ProfileVisibility != first.ProfileVisibility || !reflect.DeepEqual(v.Interests, []string{"原有明确兴趣"}) {
		t.Fatal(v)
	}
	after, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil || after.Fields.AgentNotes != p.Fields.AgentNotes || !reflect.DeepEqual(after.Fields.SocialPreferences, p.Fields.SocialPreferences) {
		t.Fatal("other fields", after, e)
	}
	before := seedEffects(t, f)
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, draft); !errors.Is(e, agentseed.ErrConflict) {
		t.Fatal("stale replay", e)
	}
	if seedEffects(t, f) != before {
		t.Fatal("replay effects")
	}
	draft = seedDraft(t, v)
	draft.InterestChoice = "SET"
	draft.Interests = []string{"羽毛球", "咖啡"}
	next, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, draft)
	if e != nil || next.Intent.Version != 2 || !reflect.DeepEqual(next.Interests, draft.Interests) {
		t.Fatal(next, e)
	}
	fresh, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil || fresh.Snapshot != next.Snapshot {
		t.Fatal("find actual result", fresh, e)
	}
	t.Log("LOCAL_SYNTHETIC_ONLY explicit selected published City is a private declaration, not residence or model permission")
}
func TestAgentSeedNativeDeferAndIsolation(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	v, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, agentseed.Input{ExpectedSnapshot: r.Snapshot, Action: "DEFER"})
	if e != nil || v.Intent.BasicIntent != "" || v.Intent.Progress != "DEFERRED" || v.NeedsPrompt || v.CurrentCity != nil || len(v.LanguagePreferences) != 0 {
		t.Fatal(v, e)
	}
	for _, access := range []agentprofile.PrivateAccess{f.peer, f.org, f.biz} {
		other, e := b.store.Authenticate(b.ctx, access.SessionDigest)
		if e != nil {
			t.Fatal(e)
		}
		_, e = b.store.SaveOwnAgentSeed(b.ctx, access.SessionDigest, other, seedDraft(t, r))
		if access == f.peer {
			if !errors.Is(e, agentseed.ErrConflict) {
				t.Fatal("other source", e)
			}
		} else if !errors.Is(e, agentseed.ErrForbidden) {
			t.Fatal(e)
		}
	}
	if _, e = b.store.ReadOwnAgentSeed(b.ctx, [32]byte{}, a); !errors.Is(e, agentseed.ErrForbidden) {
		t.Fatal(e)
	}
	before := seedEffects(t, f)
	b.exec(`UPDATE user_profiles SET bio='另一次本人更新',updated_at=clock_timestamp() WHERE account_id=$1`, a.ID)
	_, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, seedDraft(t, v))
	if !errors.Is(e, agentseed.ErrConflict) {
		t.Fatal("profile source stale", e)
	}
	if seedEffects(t, f) == before {
		t.Fatal("fixture update not actual")
	}
}
func TestAgentSeedNativeConcurrentCAS(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	input := seedDraft(t, r)
	var wg sync.WaitGroup
	ch := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input)
			ch <- e
		}()
	}
	wg.Wait()
	close(ch)
	ok, conflict := 0, 0
	for e := range ch {
		if e == nil {
			ok++
		} else if errors.Is(e, agentseed.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if ok != 1 || conflict != 1 {
		t.Fatal(ok, conflict)
	}
}
func TestAgentSeedNativeLateSessionAndRollback(t *testing.T) {
	for _, mode := range []string{"revoke", "expire", "cancel"} {
		t.Run(mode, func(t *testing.T) {
			f, a := seedNative(t)
			b := f.base
			r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
			if e != nil {
				t.Fatal(e)
			}
			input := seedDraft(t, r)
			before := seedEffects(t, f)
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if _, e = lock.Exec(b.ctx, `SELECT agent_id FROM agent_profiles WHERE agent_id=$1 FOR UPDATE`, r.AgentID); e != nil {
				t.Fatal(e)
			}
			if mode == "expire" {
				b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET expires_at=n.t+interval '1 second',idle_expires_at=n.t+interval '1 second' FROM n WHERE id=$1`, f.ownerSession)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() { _, e := b.store.SaveOwnAgentSeed(ctx, f.owner.SessionDigest, a, input); done <- e }()
			deadline := time.Now().Add(4 * time.Second)
			for {
				var waits bool
				e = b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM agent_profiles%' AND query LIKE '%FOR UPDATE%')`).Scan(&waits)
				if e != nil {
					t.Fatal(e)
				}
				if waits {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("actual metadata wait absent")
				}
				time.Sleep(10 * time.Millisecond)
			}
			switch mode {
			case "revoke":
				if e = b.store.RevokeSession(b.ctx, f.owner.SessionDigest); e != nil {
					t.Fatal(e)
				}
			case "expire":
				for {
					var expired bool
					if e = b.pool.QueryRow(b.ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE id=$1`, f.ownerSession).Scan(&expired); e != nil {
						t.Fatal(e)
					}
					if expired {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
			case "cancel":
				cancel()
			}
			if e = lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if mode == "cancel" {
					if !errors.Is(e, agentseed.ErrUnavailable) {
						t.Fatal(e)
					}
				} else if !errors.Is(e, agentseed.ErrForbidden) {
					t.Fatal(e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("save stuck")
			}
			if seedEffects(t, f) != before {
				t.Fatal("denied effect")
			}
		})
	}
}
func TestAgentSeedNativeFieldFailureRollsBack(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	input := seedDraft(t, r)
	before := seedEffects(t, f)
	// A real table constraint forces failure after Profile/City/private writes.
	// Identifier and UUID originate in the isolated fixture, never user input.
	var encoded string
	raw, _ := json.Marshal(a.ID)
	if json.Unmarshal(raw, &encoded) != nil {
		t.Fatal("fixture")
	}
	if _, e = b.pool.Exec(b.ctx, `ALTER TABLE agent_seed_user_intents ADD CONSTRAINT seed_test_reject CHECK(owner_id<>'`+encoded+`'::uuid) NOT VALID`); e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		b.pool.Exec(context.Background(), `ALTER TABLE agent_seed_user_intents DROP CONSTRAINT seed_test_reject`)
	})
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input); !errors.Is(e, agentseed.ErrUnavailable) {
		t.Fatal(e)
	}
	if seedEffects(t, f) != before {
		t.Fatal("partial effect")
	}
}

func TestAgentSeedNativeSelectedCityScopeAndReplay100(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	input := seedDraft(t, r)
	bad := input
	bad.CurrentCitySnapshot = strings.Repeat("0", 64)
	before := seedEffects(t, f)
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, bad); !errors.Is(e, agentseed.ErrConflict) || seedEffects(t, f) != before {
		t.Fatal("selected City source", e)
	}
	unrelated := "seed-city-" + a.ID
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'LOCAL_SYNTHETIC_CITY','test','GB','UTC','published','LOCAL_SYNTHETIC_ONLY','owned-fixture','synthetic')`, unrelated)
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, unrelated)
	t.Cleanup(func() {
		b.exec(`DELETE FROM contexts WHERE city_id=$1`, unrelated)
		b.exec(`DELETE FROM city_contexts WHERE city_id=$1`, unrelated)
		b.exec(`DELETE FROM cities WHERE id=$1`, unrelated)
	})
	current, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil || current.Snapshot != r.Snapshot {
		t.Fatal("unselected public City is not approval source", e)
	}
	saved, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input)
	if e != nil {
		t.Fatal(e)
	}
	stable := seedEffects(t, f)
	noop := seedDraft(t, saved)
	for i := 0; i < 100; i++ {
		v, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, noop)
		if e != nil || v.Snapshot != saved.Snapshot || v.Intent.Version != saved.Intent.Version {
			t.Fatal("stable semantic replay", i, e)
		}
	}
	if seedEffects(t, f) != stable {
		t.Fatal("100 repeat effects")
	}
	// Deleting/recreating the actual private Intent must not restore old source.
	b.exec(`DELETE FROM agent_seed_user_intents WHERE owner_id=$1`, a.ID)
	b.exec(`INSERT INTO agent_seed_user_intents(agent_id,owner_id,version,basic_intent,progress,interest_choice) VALUES($1,$2,$3,$4,'COMPLETED','SKIP')`, r.AgentID, a.ID, saved.Intent.Version, saved.Intent.BasicIntent)
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, noop); !errors.Is(e, agentseed.ErrConflict) {
		t.Fatal("source rebuilt with old version", e)
	}
}

func TestAgentSeedNativeCurrentAuthorityBoundaries(t *testing.T) {
	for _, mode := range []string{"accountPaused", "agentPaused", "metadataMissing", "agentMissing", "idleExpired", "forgedActor"} {
		t.Run(mode, func(t *testing.T) {
			f, a := seedNative(t)
			b := f.base
			r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
			if e != nil {
				t.Fatal(e)
			}
			input := seedDraft(t, r)
			switch mode {
			case "accountPaused":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, a.ID)
			case "agentPaused":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, r.AgentID)
			case "metadataMissing":
				b.exec(`DELETE FROM agent_profiles WHERE agent_id=$1`, r.AgentID)
			case "agentMissing":
				b.exec(`DELETE FROM agents WHERE id=$1`, r.AgentID)
			case "idleExpired":
				b.exec(`WITH n AS MATERIALIZED(SELECT clock_timestamp() t) UPDATE sessions SET created_at=n.t-interval '2 hours',idle_expires_at=n.t-interval '1 hour' FROM n WHERE id=$1`, f.ownerSession)
			case "forgedActor":
				a.AccountType = "organization"
			}
			before := seedEffects(t, f)
			if _, e = b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a); !errors.Is(e, agentseed.ErrForbidden) {
				t.Fatal("read authority", e)
			}
			if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input); !errors.Is(e, agentseed.ErrForbidden) {
				t.Fatal("save authority", e)
			}
			if seedEffects(t, f) != before {
				t.Fatal("denied domain effects")
			}
		})
	}
}

func TestAgentSeedNativeSelectedCitySourceAndUTC(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	cityID := "seed-city-current-" + a.ID
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label) VALUES($1,'LOCAL_SYNTHETIC_CITY','test','GB','UTC','published','LOCAL_SYNTHETIC_ONLY','owned-fixture','synthetic')`, cityID)
	b.exec(`INSERT INTO city_contexts(city_id) VALUES($1)`, cityID)
	t.Cleanup(func() {
		b.exec(`DELETE FROM person_contexts WHERE person_account_id=$1`, a.ID)
		b.exec(`DELETE FROM contexts WHERE city_id=$1`, cityID)
		b.exec(`DELETE FROM city_contexts WHERE city_id=$1`, cityID)
		b.exec(`DELETE FROM cities WHERE id=$1`, cityID)
	})
	read := func() agentseed.Record {
		r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
		if e != nil {
			t.Fatal(e)
		}
		return r
	}
	r := read()
	input := seedDraft(t, r)
	for _, c := range r.Cities {
		if c.ID == cityID {
			input.CurrentCityID = c.ID
			input.CurrentCitySnapshot = c.Snapshot
		}
	}
	b.exec(`UPDATE cities SET name='LOCAL_SYNTHETIC_CHANGED_CITY' WHERE id=$1`, cityID)
	before := seedEffects(t, f)
	if _, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input); !errors.Is(e, agentseed.ErrConflict) || seedEffects(t, f) != before {
		t.Fatal("changed selected source", e)
	}
	r = read()
	input.ExpectedSnapshot = r.Snapshot
	for _, c := range r.Cities {
		if c.ID == cityID {
			input.CurrentCitySnapshot = c.Snapshot
		}
	}
	saved, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input)
	if e != nil {
		t.Fatal(e)
	}
	// Existing current declarations are saved with the explicitly reviewed private scope.
	b.exec(`UPDATE person_contexts SET visibility='public' WHERE person_account_id=$1 AND relation='current'`, a.ID)
	r = read()
	input.ExpectedSnapshot = r.Snapshot
	saved, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input)
	if e != nil {
		t.Fatal(e)
	}
	var visibility string
	if e = b.pool.QueryRow(b.ctx, `SELECT visibility FROM person_contexts WHERE person_account_id=$1 AND relation='current'`, a.ID).Scan(&visibility); e != nil || visibility != "private" {
		t.Fatal(visibility, e)
	}
	// SET on every currently idle pooled connection, then load with transaction-local UTC.
	conn, e := b.pool.Acquire(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = conn.Exec(b.ctx, `SET TIME ZONE 'Pacific/Honolulu'`); e != nil {
		t.Fatal(e)
	}
	conn.Release()
	if utc := read(); utc.Snapshot != saved.Snapshot {
		t.Fatal("connection timezone changed source")
	}
	input.ExpectedSnapshot = saved.Snapshot
	b.exec(`UPDATE cities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, cityID)
	before = seedEffects(t, f)
	if _, e = b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input); !errors.Is(e, agentseed.ErrConflict) || seedEffects(t, f) != before {
		t.Fatal("expired selected City", e)
	}
	if expired := read(); expired.CurrentCity != nil || !expired.NeedsPrompt {
		t.Fatal("expired City inherited", expired)
	}
}

func seedWaitFor(t *testing.T, f *agentPrivateFixture, query string) {
	t.Helper()
	until := time.Now().Add(4 * time.Second)
	for {
		var waiting bool
		if e := f.base.pool.QueryRow(f.base.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE $1)`, query).Scan(&waiting); e != nil {
			t.Fatal(e)
		}
		if waiting {
			return
		}
		if time.Now().After(until) {
			t.Fatal("native wait absent", query)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func TestAgentSeedNativeLateReadRevocation(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	before := seedEffects(t, f)
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT account_id FROM user_profiles WHERE account_id=$1 FOR UPDATE`, a.ID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a); done <- e }()
	seedWaitFor(t, f, "%FROM user_profiles%FOR SHARE%")
	if e = b.store.RevokeSession(b.ctx, f.owner.SessionDigest); e != nil {
		t.Fatal(e)
	}
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, agentseed.ErrForbidden) {
			t.Fatal(e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late read stuck")
	}
	if seedEffects(t, f) != before {
		t.Fatal("read created domain effects")
	}
}
func TestAgentSeedNativeLegacyPrivateConcurrencyAndRevoke(t *testing.T) {
	f, a := seedNative(t)
	b := f.base
	r, e := b.store.ReadOwnAgentSeed(b.ctx, f.owner.SessionDigest, a)
	if e != nil {
		t.Fatal(e)
	}
	p, e := b.store.ReadOwnAgentPrivateProfile(b.ctx, f.owner)
	if e != nil {
		t.Fatal(e)
	}
	input := seedDraft(t, r)
	lock, e := b.pool.Begin(b.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(b.ctx, `SELECT account_id FROM user_profiles WHERE account_id=$1 FOR UPDATE`, a.ID); e != nil {
		t.Fatal(e)
	}
	var beforeDeadlocks int64
	if e = b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&beforeDeadlocks); e != nil {
		t.Fatal(e)
	}
	seedDone := make(chan error, 1)
	privateDone := make(chan error, 1)
	revokeDone := make(chan error, 1)
	go func() { _, e := b.store.SaveOwnAgentSeed(b.ctx, f.owner.SessionDigest, a, input); seedDone <- e }()
	seedWaitFor(t, f, "%FROM user_profiles%FOR UPDATE%")
	p.Fields.AgentNotes = "LOCAL_SYNTHETIC_CONCURRENT_PRIVATE_ONLY"
	go func() {
		_, e := b.store.ReplaceOwnAgentPrivateProfile(b.ctx, f.owner, agentprofile.ReplacePrivateInput{ExpectedVersion: p.Profile.ProfileVersion, Fields: p.Fields})
		privateDone <- e
	}()
	seedWaitFor(t, f, "%FROM agent_profiles%FOR UPDATE%")
	go func() { revokeDone <- b.store.RevokeSession(b.ctx, f.owner.SessionDigest) }()
	seedWaitFor(t, f, "%UPDATE sessions%")
	if e = lock.Commit(b.ctx); e != nil {
		t.Fatal(e)
	}
	for index, done := range []<-chan error{seedDone, privateDone, revokeDone} {
		select {
		case operationErr := <-done:
			if operationErr != nil && !((index == 0 && errors.Is(operationErr, agentseed.ErrForbidden)) || (index == 1 && (errors.Is(operationErr, agentprofile.ErrConflict) || errors.Is(operationErr, agentprofile.ErrForbidden)))) {
				t.Fatal("unexpected concurrent operation", index, operationErr)
			}
		case <-time.After(6 * time.Second):
			t.Fatal("native legacy/seed/revoke stuck")
		}
	}
	var after int64
	if e = b.pool.QueryRow(b.ctx, `SELECT deadlocks FROM pg_stat_database WHERE datname=current_database()`).Scan(&after); e != nil || after != beforeDeadlocks {
		t.Fatal("native deadlock increased", beforeDeadlocks, after, e)
	}
	t.Log("Actual native paths only; successful operations linearize before final current revocation; rejected operations roll back")
}
