package postgres

import (
	"context"
	"encoding/json"
	"errors"
	apd "github.com/birdtie/birdtie/apps/api/internal/activityparticipationdisclosure"
	ai "github.com/birdtie/birdtie/apps/api/internal/agentintroduction"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/socialcontext"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"reflect"
	"strings"
	"testing"
	"time"
)

type introductionFixture struct {
	p               *agentPrivateFixture
	n               *newPeopleFixture
	source, peer    socialintent.Record
	community, node string
}

func introductionNative(t *testing.T) *introductionFixture {
	t.Helper()
	p := policyNativeFixture(t)
	b := p.base
	f := &introductionFixture{p: p, n: &newPeopleFixture{t: t, ctx: b.ctx, pool: b.pool, store: b.store, ids: []string{b.person.ID, b.other.ID, b.business.ID, b.org.ID}, category: "badminton"}}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, sql := range []string{`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM person_contexts WHERE person_account_id=ANY($1::uuid[])`, `DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(ctx, sql, b.accounts); e != nil {
				t.Error(e)
			}
		}
		if f.node != "" {
			if _, e := b.pool.Exec(ctx, `DELETE FROM contexts WHERE id=$1`, f.node); e != nil {
				t.Error(e)
			}
		}
		if f.community != "" {
			if _, e := b.pool.Exec(ctx, `DELETE FROM communities WHERE id=$1`, f.community); e != nil {
				t.Error(e)
			}
		}
		if len(f.n.places) > 0 {
			if _, e := b.pool.Exec(ctx, `DELETE FROM places WHERE id=ANY($1::uuid[])`, f.n.places); e != nil {
				t.Error(e)
			}
		}
		for _, sql := range []string{`DELETE FROM contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM city_contexts WHERE city_id=ANY($1::text[])`, `DELETE FROM cities WHERE id=ANY($1::text[])`} {
			if _, e := b.pool.Exec(ctx, sql, f.n.cities); e != nil {
				t.Error(e)
			}
		}
	})
	savePrivateCanaries(t, p)
	f.n.enable()
	f.n.scopes()
	f.source = f.n.activate(f.n.draft(0, "IN_PERSON", 0, "", "测试区域", ""))
	f.peer = f.n.activate(f.n.draft(1, "IN_PERSON", 0, "", "测试区域", ""))
	for _, a := range []agentprofile.PrivateAccess{p.owner, p.peer} {
		if _, e := b.store.PutOwnPolicy(b.ctx, a, agentpolicysettings.Social, introductionPolicyInput(0, true, true, time.Now().UTC().Add(time.Hour))); e != nil {
			t.Fatal(e)
		}
	}
	return f
}
func introductionPolicyInput(version int64, unknown, community bool, expiry time.Time) agentpolicysettings.PutInput {
	pref := func(b bool) string {
		if b {
			return "REVIEW_REQUIRED"
		}
		return "DISABLED"
	}
	raw := `{"rules":[{"category":"UNKNOWN_PERSON","preference":"` + pref(unknown) + `"},{"category":"SHARED_COMMUNITY","preference":"` + pref(community) + `"}]}`
	return agentpolicysettings.PutInput{ExpectedVersion: version, Settings: json.RawMessage(raw), ExpiresAt: expiry.UTC().Truncate(time.Microsecond)}
}
func (f *introductionFixture) publicCommunityFixture(t *testing.T) {
	t.Helper()
	b := f.p.base
	// 033 admits these explicit-public native rows. There is currently no ordinary
	// human publication gateway for COMMUNITY declarations: this is schema/resolver
	// evidence only, never evidence that users can publish this source in the app.
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO communities(city_id,owner_account_id,name,summary,visibility,join_policy,lifecycle_status,publication_status,owner_confirmed_at,source_label,source_ref,maintainer_label) VALUES($1,$2,'合成公开社群','开发验收','public','request','active','published',clock_timestamp(),'合成','disposable://introduction','合成') RETURNING id`, f.n.cities[0], b.person.ID).Scan(&f.community); e != nil {
		t.Fatal(e)
	}
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO contexts(context_type,community_id) VALUES('COMMUNITY',$1) RETURNING id`, f.community).Scan(&f.node); e != nil {
		t.Fatal(e)
	}
	for _, person := range []string{b.person.ID, b.other.ID} {
		b.exec(`INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'interest','public')`, person, f.node)
	}
}
func (f *introductionFixture) read(t *testing.T) ai.Response {
	t.Helper()
	out, e := f.p.base.store.ReadOwnIntroductionSuggestions(f.p.base.ctx, f.p.owner, f.source.ID)
	if e != nil || ai.ValidateResponse(out, f.source.ID) != nil {
		t.Fatal("native public suggestion", e, out)
	}
	return out
}
func TestIntroductionNativePublicSourcesPolicyAndNoEffects(t *testing.T) {
	f := introductionNative(t)
	b := f.p.base
	before := policyNativeSnapshot(t, f.p, true)
	got := f.read(t)
	if len(got.Candidates) != 1 || len(got.Candidates[0].Basis) != 2 || got.Candidates[0].CandidateIntentID != f.peer.ID {
		t.Fatal(got)
	}
	raw, _ := json.Marshal(got)
	for _, secret := range []string{"PRIVATE", "私密", "constraints", "测试区域", "native_revision", "SHARED_ACTIVITY\":\"AVAILABLE", "rowToken"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("private/effect leak", secret)
		}
	}
	if policyNativeSnapshot(t, f.p, true) != before {
		t.Fatal("read mutated private/policy/grant source")
	}
	f.publicCommunityFixture(t)
	got = f.read(t)
	if len(got.Candidates) != 1 || len(got.Candidates[0].Basis) != 3 {
		t.Fatal("public declaration pair", got)
	}
	// Non-public declaration is not a basis, even when ordinary memberships exist.
	b.exec(`UPDATE person_contexts SET visibility='private' WHERE person_account_id=$1 AND context_id=$2`, b.other.ID, f.node)
	got = f.read(t)
	if len(got.Candidates) != 1 || len(got.Candidates[0].Basis) != 2 {
		t.Fatal("private declaration leaked", got)
	}
	b.exec(`UPDATE person_contexts SET visibility='public' WHERE person_account_id=$1 AND context_id=$2`, b.other.ID, f.node)
	if _, e := b.store.PutOwnPolicy(b.ctx, f.p.peer, agentpolicysettings.Social, introductionPolicyInput(1, true, false, time.Now().UTC().Add(time.Hour))); e != nil {
		t.Fatal(e)
	}
	got = f.read(t)
	if len(got.Candidates) != 0 {
		t.Fatal("shared community DISABLED bypassed by fallback", got)
	}
}

// The community's city can differ from the city chosen in both public intents.
// It is a separate selected public source, with its own expiry and native version.
func TestIntroductionNativeCommunityCityExpiryAndABA(t *testing.T) {
	for _, mode := range []string{"expiry_bound", "city_ABA", "hidden", "natural_expiry", "null_city"} {
		t.Run(mode, func(t *testing.T) {
			f := introductionNative(t)
			b := f.p.base
			f.publicCommunityFixture(t)
			communityCity := f.n.cities[1]
			if communityCity == f.n.cities[0] {
				t.Fatal("fixture must isolate the community city from both intent cities")
			}
			b.exec(`UPDATE communities SET city_id=$1 WHERE id=$2`, communityCity, f.community)
			hasCommunity := func(r ai.Response) bool {
				if len(r.Candidates) != 1 {
					t.Fatal("public intent candidate missing", r)
				}
				for _, basis := range r.Candidates[0].Basis {
					if basis.Kind == "SHARED_COMMUNITY" {
						return true
					}
				}
				return false
			}
			var expiry time.Time
			if mode == "expiry_bound" || mode == "natural_expiry" {
				if e := b.pool.QueryRow(b.ctx, `UPDATE cities SET expires_at=clock_timestamp()+interval '1 second' WHERE id=$1 RETURNING expires_at`, communityCity).Scan(&expiry); e != nil {
					t.Fatal(e)
				}
			}
			first := f.read(t)
			if !hasCommunity(first) {
				t.Fatal("current public community source missing", first)
			}
			switch mode {
			case "expiry_bound":
				if first.Candidates[0].ExpiresAt.After(expiry) {
					t.Fatal("selected community city expiry not bound", first.Candidates[0].ExpiresAt, expiry)
				}
			case "city_ABA":
				// Both updates are legal, separately committed native source versions.
				// Restoring visibility must not restore the old current-source receipt.
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, communityCity)
				if hasCommunity(f.read(t)) {
					t.Fatal("hidden community city still contributed a basis")
				}
				b.exec(`UPDATE cities SET publication_status='published' WHERE id=$1`, communityCity)
				last := f.read(t)
				if !hasCommunity(last) || last.Candidates[0].SourceBinding == first.Candidates[0].SourceBinding {
					t.Fatal("community city ABA reused old current-source receipt", last)
				}
			case "hidden":
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, communityCity)
				if hasCommunity(f.read(t)) {
					t.Fatal("hidden community city leaked a public basis")
				}
			case "natural_expiry":
				if _, e := b.pool.Exec(b.ctx, `SELECT pg_sleep(greatest(0,extract(epoch FROM ($1::timestamptz-clock_timestamp())))+0.05)`, expiry); e != nil {
					t.Fatal(e)
				}
				if hasCommunity(f.read(t)) {
					t.Fatal("naturally expired community city leaked a public basis")
				}
			case "null_city":
				b.exec(`UPDATE communities SET city_id=NULL WHERE id=$1`, f.community)
				b.exec(`UPDATE cities SET publication_status='hidden' WHERE id=$1`, communityCity)
				if !hasCommunity(f.read(t)) {
					t.Fatal("a community without a city acquired an unrelated city requirement")
				}
			}
		})
	}
}
func TestIntroductionNativeCurrentDenialsAndReceiptABA(t *testing.T) {
	for _, mode := range []string{"peer_optout", "owner_optout", "peer_policy_disabled", "owner_policy_disabled", "peer_policy_missing", "owner_policy_missing", "peer_private", "source_private", "peer_cancelled", "source_expired", "peer_expired", "block_forward", "block_reverse", "city_hidden", "place_expired", "peer_suspended", "agent_suspended", "wrong_session", "workspace", "ABA"} {
		t.Run(mode, func(t *testing.T) {
			f := introductionNative(t)
			b := f.p.base
			access := f.p.owner
			wantDenied := false
			old := f.read(t)
			switch mode {
			case "peer_optout":
				_, e := b.store.SetNewPeopleConsent(b.ctx, b.other.ID, false)
				if e != nil {
					t.Fatal(e)
				}
			case "owner_optout":
				_, e := b.store.SetNewPeopleConsent(b.ctx, b.person.ID, false)
				if e != nil {
					t.Fatal(e)
				}
				wantDenied = true
			case "peer_policy_disabled":
				_, e := b.store.PutOwnPolicy(b.ctx, f.p.peer, agentpolicysettings.Social, introductionPolicyInput(1, false, true, time.Now().UTC().Add(time.Hour)))
				if e != nil {
					t.Fatal(e)
				}
			case "owner_policy_disabled":
				_, e := b.store.PutOwnPolicy(b.ctx, f.p.owner, agentpolicysettings.Social, introductionPolicyInput(1, false, true, time.Now().UTC().Add(time.Hour)))
				if e != nil {
					t.Fatal(e)
				}
				wantDenied = true
			case "peer_policy_missing":
				b.exec(`DELETE FROM agent_policy_settings WHERE owner_id=$1 AND family='SOCIAL'`, b.other.ID)
			case "owner_policy_missing":
				b.exec(`DELETE FROM agent_policy_settings WHERE owner_id=$1 AND family='SOCIAL'`, b.person.ID)
				wantDenied = true
			case "peer_private":
				f.n.audience(f.peer.ID, "PRIVATE", "")
			case "source_private":
				f.n.audience(f.source.ID, "PRIVATE", "")
				wantDenied = true
			case "peer_cancelled":
				_, e := b.store.CancelSocialIntent(b.ctx, b.other.ID, f.peer.ID)
				if e != nil {
					t.Fatal(e)
				}
			case "source_expired":
				b.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '80 milliseconds' WHERE id=$1`, f.source.ID)
				time.Sleep(120 * time.Millisecond)
				wantDenied = true
			case "peer_expired":
				b.exec(`UPDATE social_intents SET expires_at=clock_timestamp()+interval '80 milliseconds' WHERE id=$1`, f.peer.ID)
				time.Sleep(120 * time.Millisecond)
			case "block_forward":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.person.ID, b.other.ID)
			case "block_reverse":
				b.exec(`INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, b.other.ID, b.person.ID)
			case "city_hidden":
				b.exec(`UPDATE cities SET publication_status='draft' WHERE id=$1`, f.n.cities[0])
				wantDenied = true
			case "place_expired": // An explicit stale Place cannot fall back to City/area.
				b.exec(`UPDATE social_intents SET constraints=jsonb_set(constraints,'{placeId}',to_jsonb($2::text)) WHERE id=ANY($1::uuid[])`, []string{f.source.ID, f.peer.ID}, f.n.places[0])
				b.exec(`UPDATE places SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.n.places[0])
				wantDenied = true
			case "peer_suspended":
				b.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, b.other.ID)
			case "agent_suspended":
				b.exec(`UPDATE agents SET status='suspended' WHERE id=$1`, b.personID)
				wantDenied = true
			case "wrong_session":
				access.SessionDigest = f.p.peer.SessionDigest
				wantDenied = true
			case "workspace":
				access.WorkspacePrincipal = b.org
				wantDenied = true
			case "ABA":
				for i, v := range []bool{false, true} {
					if _, e := b.store.PutOwnPolicy(b.ctx, f.p.peer, agentpolicysettings.Social, introductionPolicyInput(int64(i+1), v, true, time.Now().UTC().Add(time.Hour))); e != nil {
						t.Fatal(e)
					}
				}
			}
			got, e := b.store.ReadOwnIntroductionSuggestions(b.ctx, access, f.source.ID)
			if wantDenied {
				if !errors.Is(e, ai.ErrDenied) || !reflect.DeepEqual(got, ai.Response{}) {
					t.Fatal("expected empty denied", e, got)
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			if mode == "ABA" {
				if len(got.Candidates) != 1 || got.Candidates[0].SourceBinding == old.Candidates[0].SourceBinding {
					t.Fatal("ABA reused old native receipt", got)
				}
			} else if len(got.Candidates) != 0 {
				t.Fatal("current denial leaked peer", mode, got)
			}
		})
	}
}
func TestIntroductionNativeWaitSessionRevokeAndNaturalExpiry(t *testing.T) {
	for _, mode := range []string{"revoke", "natural_expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := introductionNative(t)
			b := f.p.base
			held, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer held.Rollback(context.Background())
			if mode == "natural_expiry" {
				b.exec(`UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '500 milliseconds' WHERE id=$1`, f.p.ownerSession)
			}
			if _, e = held.Exec(b.ctx, `LOCK TABLE agent_policy_settings IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			type result struct {
				o ai.Response
				e error
			}
			done := make(chan result, 1)
			go func() {
				o, e := b.store.ReadOwnIntroductionSuggestions(b.ctx, f.p.owner, f.source.ID)
				done <- result{o, e}
			}()
			deadline := time.Now().Add(3 * time.Second)
			waiting := false
			for time.Now().Before(deadline) {
				var n int
				e = b.pool.QueryRow(b.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%current_source AS MATERIALIZED%'`).Scan(&n)
				if e != nil {
					t.Fatal(e)
				}
				if n > 0 {
					waiting = true
					break
				}
				time.Sleep(15 * time.Millisecond)
			}
			if !waiting {
				t.Fatal("actual native table wait not reached")
			}
			if mode == "revoke" {
				b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.p.ownerSession)
			} else {
				time.Sleep(600 * time.Millisecond)
			}
			if e = held.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case r := <-done:
				if !errors.Is(r.e, ai.ErrDenied) || !reflect.DeepEqual(r.o, ai.Response{}) {
					t.Fatal("stale session after real wait", r)
				}
			case <-time.After(3 * time.Second):
				t.Fatal("native operation did not finish")
			}
		})
	}
}

var _ pgx.Tx // compile-time dependency keeps explicit native transaction tests.

func TestIntroductionNativeExplicitSharedRegistrationNoAttendance(t *testing.T) {
	f := participationDisclosureNative(t)
	b := f.f
	sources := []socialintent.Record{}
	policy := func(version int64, shared bool) agentpolicysettings.PutInput {
		pref := "DISABLED"
		if shared {
			pref = "REVIEW_REQUIRED"
		}
		return agentpolicysettings.PutInput{ExpectedVersion: version, Settings: json.RawMessage(`{"rules":[{"category":"UNKNOWN_PERSON","preference":"REVIEW_REQUIRED"},{"category":"SHARED_ACTIVITY","preference":"` + pref + `"}]}`), ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
	}
	for _, a := range []agentprofile.PrivateAccess{b.a, b.other} {
		b.exec(`INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'合成明确报名用户','public') ON CONFLICT DO NOTHING`, a.WorkspacePrincipal.ID)
		if _, e := b.s.SetNewPeopleConsent(b.ctx, a.WorkspacePrincipal.ID, true); e != nil {
			t.Fatal(e)
		}
		if _, e := b.s.SetSocialDisclosure(b.ctx, a.WorkspacePrincipal.ID, socialcontext.Disclosure{SharedActivities: true}); e != nil {
			t.Fatal(e)
		}
		if _, e := b.s.PutOwnPolicy(b.ctx, a, agentpolicysettings.Social, policy(0, true)); e != nil {
			t.Fatal(e)
		}
		d, e := b.s.CreateNewPeopleIntent(b.ctx, a.WorkspacePrincipal.ID, newpeople.DraftInput{Title: "合成具体公开意图", Category: "badminton", Modality: "ONLINE", ExpiresAt: time.Now().UTC().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		d, e = b.s.ActivateSocialIntent(b.ctx, a.WorkspacePrincipal.ID, d.ID)
		if e != nil {
			t.Fatal(e)
		}
		sources = append(sources, d)
	}
	read := func(wantActivity bool) ai.Response {
		t.Helper()
		v, e := b.s.ReadOwnIntroductionSuggestions(b.ctx, b.a, sources[0].ID)
		if e != nil || ai.ValidateResponse(v, sources[0].ID) != nil || len(v.Candidates) != 1 {
			t.Fatal(e, v)
		}
		found := false
		for _, basis := range v.Candidates[0].Basis {
			if basis.Kind == "SHARED_ACTIVITY" {
				found = true
				if !strings.Contains(basis.Explanation, "报名") || !strings.Contains(basis.Explanation, "不代表到场") {
					t.Fatal(basis)
				}
			}
		}
		if found != wantActivity {
			t.Fatal("explicit activity availability", wantActivity, v)
		}
		return v
	}
	read(false) // 048 alone is not concrete disclosure.
	f.approve(t, f.preview(t, "PUBLIC"))
	read(false) // One-sided is not shared.
	in := apd.Input{ParticipationID: f.peerP, Operation: "PUBLIC", DisclosureExpiresAt: &f.expiry}
	p, e := b.s.PreviewOwnParticipationDisclosure(b.ctx, b.other, in)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = b.s.ApproveOwnParticipationDisclosure(b.ctx, b.other, p.Preview); e != nil {
		t.Fatal(e)
	}
	old := read(true)
	if old.Candidates[0].ExpiresAt.After(f.expiry) {
		t.Fatal("registration expiry not in candidate bound")
	}
	before := f.snapshot()
	read(true)
	if before != f.snapshot() {
		t.Fatal("introduction produced effect")
	}
	if _, e = b.s.PutOwnPolicy(b.ctx, b.other, agentpolicysettings.Social, policy(1, false)); e != nil {
		t.Fatal(e)
	}
	v, e := b.s.ReadOwnIntroductionSuggestions(b.ctx, b.a, sources[0].ID)
	if e != nil || len(v.Candidates) != 0 {
		t.Fatal("disabled shared policy", e, v)
	}
	if _, e = b.s.PutOwnPolicy(b.ctx, b.other, agentpolicysettings.Social, policy(2, true)); e != nil {
		t.Fatal(e)
	}
	read(true)
	if _, e = b.s.SetSocialDisclosure(b.ctx, b.other.WorkspacePrincipal.ID, socialcontext.Disclosure{}); e != nil {
		t.Fatal(e)
	}
	read(false)
	if _, e = b.s.SetSocialDisclosure(b.ctx, b.other.WorkspacePrincipal.ID, socialcontext.Disclosure{SharedActivities: true}); e != nil {
		t.Fatal(e)
	}
	read(true)
	f.approve(t, f.preview(t, "PRIVATE"))
	read(false)
	f.approve(t, f.preview(t, "PUBLIC"))
	again := read(true)
	if again.Candidates[0].SourceBinding == old.Candidates[0].SourceBinding {
		t.Fatal("disclosure ABA old receipt")
	}
	b.exec(`UPDATE activities SET visibility='private' WHERE id=$1`, f.activity)
	read(false)
	b.exec(`UPDATE activities SET visibility='public' WHERE id=$1`, f.activity)
	read(false) // Source ABA requires both new explicit approvals.
}
