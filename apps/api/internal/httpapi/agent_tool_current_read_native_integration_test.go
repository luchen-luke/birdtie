package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentpolicysettings"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/intent"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Observation/mutation barriers wrap the actual CurrentSearch/Match ports.
// They neither mint receipts nor substitute SQL, permissions, or authority.
// All principals, sessions and supplied entities are disposable local fixtures.
type currentReadonlyNativeHTTP struct {
	*postgres.Store
	searchReads, searchFinal, legacyReads, legacyFinal int
	matchReads, matchFinal                             int
	beforeSearchRead                                   func()
	beforeMatchRead                                    func()
	beforeSearchFinal                                  func(agenttool.CurrentSearch, *agenttool.CurrentSearchReceipt)
	beforeMatchFinal                                   func(agenttool.CurrentMatch, *agenttool.CurrentMatchReceipt)
}

func (s *currentReadonlyNativeHTTP) ReadOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch) (agenttool.CurrentSearchReceipt, error) {
	s.searchReads++
	if s.beforeSearchRead != nil {
		s.beforeSearchRead()
	}
	return s.Store.ReadOwnCurrentSearch(ctx, q)
}
func (s *currentReadonlyNativeHTTP) RevalidateOwnCurrentSearch(ctx context.Context, q agenttool.CurrentSearch, r agenttool.CurrentSearchReceipt) error {
	s.searchFinal++
	if s.beforeSearchFinal != nil {
		s.beforeSearchFinal(q, &r)
	}
	return s.Store.RevalidateOwnCurrentSearch(ctx, q, r)
}
func (s *currentReadonlyNativeHTTP) ReadAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query) (arp.Receipt, error) {
	s.legacyReads++
	return s.Store.ReadAgentResultProjection(ctx, a, q)
}
func (s *currentReadonlyNativeHTTP) RevalidateAgentResultProjection(ctx context.Context, a arp.Access, q arp.Query, r arp.Receipt) error {
	s.legacyFinal++
	return s.Store.RevalidateAgentResultProjection(ctx, a, q, r)
}
func (s *currentReadonlyNativeHTTP) ReadOwnCurrentMatch(ctx context.Context, q agenttool.CurrentMatch) (agenttool.CurrentMatchReceipt, error) {
	s.matchReads++
	if s.beforeMatchRead != nil {
		s.beforeMatchRead()
	}
	return s.Store.ReadOwnCurrentMatch(ctx, q)
}
func (s *currentReadonlyNativeHTTP) RevalidateOwnCurrentMatch(ctx context.Context, q agenttool.CurrentMatch, r agenttool.CurrentMatchReceipt) error {
	s.matchFinal++
	if s.beforeMatchFinal != nil {
		s.beforeMatchFinal(q, &r)
	}
	return s.Store.RevalidateOwnCurrentMatch(ctx, q, r)
}

func currentReadonlyNativeHandler(f *privateProfileHTTPDBFixture, s *currentReadonlyNativeHTTP) http.Handler {
	return New(s, f.store, nil, f.store, s, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
}

func currentReadonlyNativeSearchFixture(t *testing.T, kind string) (*contextBuilderHTTPFixture, *currentReadonlyNativeHTTP, string, string) {
	t.Helper()
	f := contextBuilderHTTPNative(t)
	id, query := f.place, "找地点"
	if kind == "person" {
		id, query = f.accountIDs[1], "找公开成员"
		var now time.Time
		if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
			t.Fatal(e)
		}
		i, e := f.store.SubmitIntent(f.ctx, id, f.city, intent.Input{Confirmed: true, Topic: "合成本人明确公开交流", AvailableFrom: now.Add(-time.Minute), AvailableUntil: now.Add(time.Hour), TimeZone: "Europe/London", CoarseAreaLabel: "本人公开粗范围", ExpiresAt: now.Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() {
			if _, e := f.pool.Exec(context.Background(), `DELETE FROM intents WHERE id=$1`, i.ID); e != nil {
				t.Error(e)
			}
		})
	}
	s := &currentReadonlyNativeHTTP{Store: f.store}
	f.handler = currentReadonlyNativeHandler(f.privateProfileHTTPDBFixture, s)
	return f, s, id, query
}

func currentReadonlyNativePolicy(t *testing.T, f *privateProfileHTTPDBFixture, version int64, level agentautonomy.Level, until time.Time) agentpolicysettings.Bundle {
	t.Helper()
	digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
	if e != nil {
		t.Fatal(e)
	}
	raw, e := json.Marshal(agentpolicysettings.AutonomySettings{Level: level})
	if e != nil {
		t.Fatal(e)
	}
	b, e := f.store.PutOwnPolicy(f.ctx, agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[0]}, SessionDigest: digest}, agentpolicysettings.Autonomy, agentpolicysettings.PutInput{ExpectedVersion: version, Settings: raw, ExpiresAt: until.UTC().Truncate(time.Microsecond)})
	if e != nil || b.Autonomy.NativeRevision != version+1 {
		t.Fatalf("actual policy writer failed version=%d error=%v", version, e)
	}
	return b
}

func currentReadonlyNativeNoEffects(t *testing.T, f *privateProfileHTTPDBFixture) string {
	t.Helper()
	var raw string
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_array(
 (SELECT count(*) FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM conversation_messages WHERE sender_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])),
 (SELECT count(*) FROM saved_items WHERE owner_account_id=ANY($1::uuid[])))::text`, f.accountIDs).Scan(&raw)
	if e != nil {
		t.Fatal(e)
	}
	return raw
}

func currentReadonlyNativeNoBody(t *testing.T, w *httptest.ResponseRecorder, ids ...string) {
	t.Helper()
	if strings.Contains(w.Body.String(), `"data"`) {
		t.Fatal("rejected encoded response retained data", w.Body.String())
	}
	for _, id := range ids {
		if strings.Contains(w.Body.String(), id) {
			t.Fatal("rejected current source leaked original ref", w.Body.String())
		}
	}
}

func TestCurrentReadonlyNativeRegisteredSearchIDsEmptyAndNoEffects(t *testing.T) {
	for _, kind := range []string{"place", "person"} {
		t.Run(kind, func(t *testing.T) {
			f, s, id, query := currentReadonlyNativeSearchFixture(t, kind)
			body, _ := json.Marshal(map[string]string{"query": query})
			path := "/v1/cities/" + f.city + "/agent/tasks"
			r := resultHTTPReply(t, f.request(t, f.handler, "POST", path, string(body), f.tokens[0], 200, nil))
			item := resultHTTPFind(t, r, kind, id)
			if item.Detail == nil || item.Share == nil || *item.Detail != item.Entity || *item.Share != item.Entity || (kind == "person" && item.Anchor != nil) {
				t.Fatal("original source ref or person precision changed", item)
			}
			if s.searchReads != 1 || s.searchFinal != 1 || s.legacyReads != 0 || s.legacyFinal != 0 {
				t.Fatalf("registered POST did not use actual current port: %+v", s)
			}
			before := currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture)
			get := "/v1/me/agent-tasks/" + r.Task.ID
			resultHTTPFind(t, resultHTTPReply(t, f.request(t, f.handler, "GET", get, "", f.tokens[0], 200, nil)), kind, id)
			if s.searchReads != 2 || s.searchFinal != 2 || before != currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture) {
				t.Fatal("restore bypassed current port or had domain effects")
			}
			f.request(t, f.handler, "GET", get, "", f.tokens[1], 404, nil)
			f.request(t, f.handler, "GET", get, "", "", 401, nil)
			unknown := f.request(t, f.handler, "GET", "/v1/me/agent-tasks/"+f.accountIDs[2], "", f.tokens[0], 404, nil)
			currentReadonlyNativeNoBody(t, unknown, id)
			if s.searchReads != 2 || s.searchFinal != 2 {
				t.Fatal("foreign/anonymous task borrowed owner tool read")
			}
			if kind == "place" {
				f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, id)
			} else {
				f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, id)
			}
			w := f.request(t, f.handler, "GET", get, "", f.tokens[0], 200, nil)
			empty := resultHTTPReply(t, w)
			if empty.ResultSet.Status != "empty" || len(empty.ResultSet.Items) != 0 || strings.Contains(w.Body.String(), id) {
				t.Fatal("hidden source did not produce genuinely current empty", w.Body.String())
			}
			if s.searchReads != 3 || s.searchFinal != 3 || before != currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture) {
				t.Fatal("empty result bypassed current port or caused effects")
			}
		})
	}
}

func TestCurrentReadonlyNativeSearchFinalCurrentPolicySourceAndSeal(t *testing.T) {
	for _, kind := range []string{"place", "person"} {
		modes := []string{"sourceHide", "sourceABA", "taskABA", "policyABA", "sessionRevoke", "normalIdleRenewal", "compoundSealTamper", "sourceSealTamper", "actorSuspend", "agentRetire"}
		if kind == "person" {
			modes = append(modes, "forwardBlock", "reverseBlock")
		}
		for _, mode := range modes {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f, s, id, query := currentReadonlyNativeSearchFixture(t, kind)
				// The original Task transition legitimately routes one task notice.
				// Capture AFTER that transition, BEFORE the registered READ port.
				// This isolates tool side effects without hiding a read-created notice.
				beforeEffects := ""
				s.beforeSearchRead = func() { beforeEffects = currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture) }
				expires := time.Now().UTC().Add(time.Hour)
				if mode == "policyABA" {
					currentReadonlyNativePolicy(t, f.privateProfileHTTPDBFixture, 0, agentautonomy.LevelAssist, expires)
				}
				s.beforeSearchFinal = func(q agenttool.CurrentSearch, r *agenttool.CurrentSearchReceipt) {
					if !r.Valid(q) || len(r.Source.Items) != 1 || r.Source.Items[0].Entity.ID != id {
						t.Fatal("barrier did not receive real current native source")
					}
					switch mode {
					case "sourceHide":
						if kind == "place" {
							f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, id)
						} else {
							f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, id)
						}
					case "sourceABA":
						if kind == "place" {
							f.exec(`UPDATE places SET publication_status='hidden' WHERE id=$1`, id)
							f.exec(`UPDATE places SET publication_status='published' WHERE id=$1`, id)
						} else {
							f.exec(`UPDATE user_profiles SET visibility='private' WHERE account_id=$1`, id)
							f.exec(`UPDATE user_profiles SET visibility='public' WHERE account_id=$1`, id)
						}
					case "taskABA":
						f.exec(`UPDATE agent_tasks SET updated_at=updated_at WHERE id=$1`, q.Access.TaskID)
					case "policyABA":
						currentReadonlyNativePolicy(t, f.privateProfileHTTPDBFixture, 1, agentautonomy.LevelObserve, expires)
						currentReadonlyNativePolicy(t, f.privateProfileHTTPDBFixture, 2, agentautonomy.LevelAssist, expires)
					case "sessionRevoke":
						f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, q.Access.SessionDigest[:])
					case "normalIdleRenewal":
						f.exec(`UPDATE sessions SET idle_expires_at=least(expires_at,clock_timestamp()+interval '90 minutes') WHERE token_sha256=$1`, q.Access.SessionDigest[:])
					case "compoundSealTamper":
						r.Seal = strings.Repeat("0", 64)
					case "sourceSealTamper":
						r.Source.Seal = strings.Repeat("0", 64)
					case "actorSuspend":
						f.exec(`UPDATE accounts SET status='suspended' WHERE id=$1`, f.accountIDs[0])
					case "agentRetire":
						f.exec(`UPDATE agents SET status='retired' WHERE id=$1`, f.agentIDs[0])
					case "forwardBlock":
						if e := f.store.BlockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); e != nil {
							t.Fatal(e)
						}
					case "reverseBlock":
						if e := f.store.BlockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
							t.Fatal(e)
						}
					}
				}
				want := 409
				if mode == "sessionRevoke" || mode == "compoundSealTamper" || mode == "sourceSealTamper" || mode == "actorSuspend" || mode == "agentRetire" {
					want = 403
				}
				if mode == "normalIdleRenewal" {
					want = 200
				}
				body, _ := json.Marshal(map[string]string{"query": query})
				w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", string(body), f.tokens[0], want, nil)
				if s.searchReads != 1 || s.searchFinal != 1 || s.legacyReads+s.legacyFinal != 0 {
					t.Fatal("final barrier did not reach actual current search exactly once")
				}
				if want == 200 {
					resultHTTPFind(t, resultHTTPReply(t, w), kind, id)
				} else {
					currentReadonlyNativeNoBody(t, w, id)
				}
				if after := currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture); after != beforeEffects {
					t.Logf("domain counts before=%s after=%s", beforeEffects, after)
					t.Fatal("search mutated a user action domain")
				}
			})
		}
	}
}

func currentReadonlyNativeMatchFixture(t *testing.T) (*privateProfileHTTPDBFixture, *currentReadonlyNativeHTTP, string, string) {
	t.Helper()
	f := privateProfileHTTPDBNew(t)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`} {
			if _, e := f.pool.Exec(context.Background(), q, f.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	ids := []string{}
	for _, owner := range f.accountIDs[:2] {
		if _, e := f.store.SetNewPeopleConsent(f.ctx, owner, true); e != nil {
			t.Fatal(e)
		}
		i, e := f.store.CreateNewPeopleIntent(f.ctx, owner, newpeople.DraftInput{Title: "039_PRIVATE_MATCH_TITLE_CANARY", Category: "badminton", Modality: "ONLINE", OnlinePlatform: "039_PRIVATE_PLATFORM_CANARY", ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = f.store.ActivateSocialIntent(f.ctx, owner, i.ID); e != nil {
			t.Fatal(e)
		}
		ids = append(ids, i.ID)
	}
	s := &currentReadonlyNativeHTTP{Store: f.store}
	f.handler = currentReadonlyNativeHandler(f, s)
	return f, s, ids[0], ids[1]
}

func TestCurrentReadonlyNativeMatchCurrentPolicyOptInAndNoEffects(t *testing.T) {
	for _, mode := range []string{"positiveThenEmpty", "policyABA", "peerOptInABA", "blockABA", "sessionRefreshRetiresProof", "compoundSealTamper", "sourceSealTamper", "sessionRevoke", "sessionABA"} {
		t.Run(mode, func(t *testing.T) {
			f, s, source, peer := currentReadonlyNativeMatchFixture(t)
			expires := time.Now().UTC().Add(time.Hour)
			if mode == "policyABA" {
				currentReadonlyNativePolicy(t, f, 0, agentautonomy.LevelAssist, expires)
			}
			if mode != "positiveThenEmpty" {
				s.beforeMatchFinal = func(q agenttool.CurrentMatch, r *agenttool.CurrentMatchReceipt) {
					if !r.Valid(q) || len(r.Source.Response.Candidates) != 1 || r.Source.Response.Candidates[0].CandidateIntentID != peer {
						t.Fatal("final match did not receive actual current native candidate")
					}
					switch mode {
					case "policyABA":
						currentReadonlyNativePolicy(t, f, 1, agentautonomy.LevelObserve, expires)
						currentReadonlyNativePolicy(t, f, 2, agentautonomy.LevelAssist, expires)
					case "peerOptInABA":
						for _, enabled := range []bool{false, true} {
							if _, e := f.store.SetNewPeopleConsent(f.ctx, f.accountIDs[1], enabled); e != nil {
								t.Fatal(e)
							}
						}
					case "blockABA":
						if e := f.store.BlockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); e != nil {
							t.Fatal(e)
						}
						if e := f.store.UnblockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); e != nil {
							t.Fatal(e)
						}
					case "sessionRefreshRetiresProof":
						// Existing HumanNewPeople conservatively binds session xmin.
						// This records the current 409 limitation; accepting renewal
						// without another native authority marker loses revoke/restore ABA.
						f.exec(`UPDATE sessions SET idle_expires_at=least(expires_at,clock_timestamp()+interval '90 minutes') WHERE token_sha256=$1`, q.SessionDigest[:])
					case "compoundSealTamper":
						r.Seal = strings.Repeat("0", 64)
					case "sourceSealTamper":
						r.Source.Seal = strings.Repeat("0", 64)
					case "sessionRevoke":
						if e := f.store.RevokeSession(f.ctx, q.SessionDigest); e != nil {
							t.Fatal(e)
						}
					case "sessionABA":
						// Privileged diagnostic only: no production restore API exists.
						f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, q.SessionDigest[:])
						f.exec(`UPDATE sessions SET revoked_at=NULL WHERE token_sha256=$1`, q.SessionDigest[:])
					}
				}
			}
			want := 409
			if mode == "positiveThenEmpty" {
				want = 200
			}
			if mode == "compoundSealTamper" || mode == "sourceSealTamper" || mode == "sessionRevoke" {
				want = 403
			}
			path := "/v1/me/new-people/candidates?sourceIntentId=" + source
			w := f.request(t, f.handler, "GET", path, "", f.tokens[0], want, nil)
			if s.matchReads != 1 || s.matchFinal != 1 {
				t.Fatal("registered match bypassed actual current port")
			}
			if want == 200 {
				if !strings.Contains(w.Body.String(), peer) {
					t.Fatal("current explicitly eligible peer missing")
				}
			} else {
				currentReadonlyNativeNoBody(t, w, peer)
			}
			for _, canary := range []string{"039_PRIVATE_MATCH_TITLE_CANARY", "039_PRIVATE_PLATFORM_CANARY", "latitude", "longitude", "SessionDigest", "Seal"} {
				if strings.Contains(w.Body.String(), canary) {
					t.Fatal("matching source/control leaked", canary)
				}
			}
			if mode == "positiveThenEmpty" {
				if _, e := f.store.SetNewPeopleConsent(f.ctx, f.accountIDs[1], false); e != nil {
					t.Fatal(e)
				}
				empty := f.request(t, f.handler, "GET", path, "", f.tokens[0], 200, nil)
				var wire struct{ Data newpeople.Response }
				if json.Unmarshal(empty.Body.Bytes(), &wire) != nil || wire.Data.Candidates == nil || len(wire.Data.Candidates) != 0 || strings.Contains(empty.Body.String(), peer) {
					t.Fatal("actual peer opt-out did not yield true empty", empty.Body.String())
				}
				f.request(t, f.handler, "GET", path, "", f.tokens[1], 404, nil)
				f.request(t, f.handler, "GET", path, "", "", 401, nil)
			}
			if currentReadonlyNativeNoEffects(t, f) != "[0, 0, 0, 0, 0]" {
				t.Fatal("read-only matching created invitation/Tie/message/save")
			}
			t.Logf("LOCAL_SYNTHETIC_ONLY native match=%s source=%s original_peer=%s calls=%d/%d", mode, source, peer, s.matchReads, s.matchFinal)
		})
	}
}

// Tag only the request Store; unrelated native waiters cannot satisfy its barrier.
func currentReadonlyNativeRequestPool(t *testing.T, f *privateProfileHTTPDBFixture, s *currentReadonlyNativeHTTP) string {
	t.Helper()
	c := f.pool.Config().Copy()
	app := "birdtie_read039_" + f.accountIDs[0]
	c.ConnConfig.RuntimeParams["application_name"] = app
	p, e := pgxpool.NewWithConfig(f.ctx, c)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(p.Close)
	s.Store = postgres.New(p, false)
	return app
}

// Arm after the first native receipt. Observe the exact SHARE-table wait and
// DB-clock expiry, then release it. Failure cleanup unlocks and joins first.
func currentReadonlyNativeExpiryBarrier(t *testing.T, f *privateProfileHTTPDBFixture, app string, until *time.Time) (func(), func()) {
	t.Helper()
	ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
	var lock pgx.Tx
	done := make(chan error, 1)
	started, consumed := false, false
	t.Cleanup(func() {
		cancel()
		cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		if lock != nil {
			_ = lock.Rollback(cleanupCtx)
		}
		if started && !consumed {
			select {
			case <-done:
			case <-cleanupCtx.Done():
				t.Error("native expiry observer did not terminate before fixture cleanup")
			}
		}
	})
	arm := func() {
		if started || until.IsZero() {
			t.Fatal("invalid native expiry barrier")
		}
		var e error
		lock, e = f.pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = lock.Exec(ctx, `LOCK TABLE agent_policy_settings IN ROW EXCLUSIVE MODE`); e != nil {
			t.Fatal(e)
		}
		started = true
		go func() {
			var result error
			defer func() {
				cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if e := lock.Rollback(cleanupCtx); e != nil && result == nil {
					result = e
				}
				done <- result
			}()
			pid := 0
			for {
				var statement string
				e := f.pool.QueryRow(ctx, `SELECT a.pid,a.query FROM pg_stat_activity a
 WHERE a.datname=current_database() AND a.application_name=$1
 AND $2::integer=ANY(pg_blocking_pids(a.pid))
 AND a.query LIKE '%LOCK TABLE agent_policy_settings IN SHARE MODE%'
 AND EXISTS(SELECT 1 FROM pg_locks l WHERE l.pid=a.pid
 AND l.relation='agent_policy_settings'::regclass AND l.mode='ShareLock' AND NOT l.granted)`, app, int(lock.Conn().PgConn().PID())).Scan(&pid, &statement)
				if e == nil {
					t.Logf("LOCAL_SYNTHETIC actual final native wait requestPID=%d lockerPID=%d SQL=%q", pid, lock.Conn().PgConn().PID(), statement)
					break
				}
				if e != pgx.ErrNoRows {
					result = fmt.Errorf("exact request wait not observed: %w", e)
					return
				}
				select {
				case <-ctx.Done():
					result = ctx.Err()
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
			for {
				var now time.Time
				if e := f.pool.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
					result = e
					return
				}
				if now.After(*until) {
					t.Logf("LOCAL_SYNTHETIC DB clock expired while exact request blocked: requestPID=%d deadline=%s observed=%s", pid, until.UTC().Format(time.RFC3339Nano), now.UTC().Format(time.RFC3339Nano))
					return
				}
				select {
				case <-ctx.Done():
					result = ctx.Err()
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
		}()
	}
	finish := func() {
		select {
		case e := <-done:
			consumed = true
			if e != nil {
				t.Fatal(e)
			}
		case <-ctx.Done():
			t.Fatal("native expiry barrier failed", ctx.Err())
		}
	}
	return arm, finish
}

func currentReadonlyNativeShortDeadline(t *testing.T, f *privateProfileHTTPDBFixture, mode, source string) time.Time {
	t.Helper()
	var until time.Time
	if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()+interval '2 seconds'`).Scan(&until); e != nil {
		t.Fatal(e)
	}
	switch mode {
	case "policyExpiry":
		currentReadonlyNativePolicy(t, f, 0, agentautonomy.LevelAssist, until)
	case "sessionExpiry":
		digest, e := identity.ParseBearer("Bearer " + f.tokens[0])
		if e != nil {
			t.Fatal(e)
		}
		f.exec(`UPDATE sessions SET idle_expires_at=$2 WHERE token_sha256=$1`, digest[:], until)
	case "placeExpiry":
		f.exec(`UPDATE places SET expires_at=$2 WHERE id=$1`, source, until)
	case "personExpiry":
		f.exec(`UPDATE intents SET expires_at=$2 WHERE owner_account_id=$1`, source, until)
	case "peerExpiry":
		f.exec(`UPDATE social_intents SET expires_at=$2 WHERE id=$1`, source, until)
	default:
		t.Fatal("unknown native deadline")
	}
	return until
}

func TestCurrentReadonlyNativeSearchActualWaitRejectsExpiredOutput(t *testing.T) {
	for _, kind := range []string{"place", "person"} {
		for _, mode := range []string{"policyExpiry", "sessionExpiry", kind + "Expiry"} {
			t.Run(kind+"/"+mode, func(t *testing.T) {
				f, s, id, query := currentReadonlyNativeSearchFixture(t, kind)
				app := currentReadonlyNativeRequestPool(t, f.privateProfileHTTPDBFixture, s)
				var until time.Time
				before := ""
				s.beforeSearchRead = func() {
					until = currentReadonlyNativeShortDeadline(t, f.privateProfileHTTPDBFixture, mode, id)
					before = currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture)
				}
				arm, finish := currentReadonlyNativeExpiryBarrier(t, f.privateProfileHTTPDBFixture, app, &until)
				s.beforeSearchFinal = func(q agenttool.CurrentSearch, r *agenttool.CurrentSearchReceipt) {
					if !r.Valid(q) || len(r.Source.Items) != 1 || r.Source.Items[0].Entity.ID != id {
						t.Fatal("no real native source before wait")
					}
					arm()
				}
				want := 409
				if mode == "policyExpiry" {
					want = 403
				}
				body, _ := json.Marshal(map[string]string{"query": query})
				w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", string(body), f.tokens[0], want, nil)
				finish()
				currentReadonlyNativeNoBody(t, w, id)
				if s.searchReads != 1 || s.searchFinal != 1 || s.legacyReads+s.legacyFinal != 0 || before != currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture) {
					t.Fatal("expiry bypassed current read fence or created read effect")
				}
			})
		}
	}
}

func TestCurrentReadonlyNativeMatchActualWaitRejectsExpiredOutput(t *testing.T) {
	for _, mode := range []string{"policyExpiry", "sessionExpiry", "peerExpiry"} {
		t.Run(mode, func(t *testing.T) {
			f, s, source, peer := currentReadonlyNativeMatchFixture(t)
			app := currentReadonlyNativeRequestPool(t, f, s)
			var until time.Time
			s.beforeMatchRead = func() { until = currentReadonlyNativeShortDeadline(t, f, mode, peer) }
			arm, finish := currentReadonlyNativeExpiryBarrier(t, f, app, &until)
			s.beforeMatchFinal = func(q agenttool.CurrentMatch, r *agenttool.CurrentMatchReceipt) {
				if !r.Valid(q) || len(r.Source.Response.Candidates) != 1 || r.Source.Response.Candidates[0].CandidateIntentID != peer {
					t.Fatal("no real eligible native peer before wait")
				}
				arm()
			}
			want := 409
			if mode == "policyExpiry" {
				want = 403
			}
			w := f.request(t, f.handler, "GET", "/v1/me/new-people/candidates?sourceIntentId="+source, "", f.tokens[0], want, nil)
			finish()
			currentReadonlyNativeNoBody(t, w, peer)
			if s.matchReads != 1 || s.matchFinal != 1 || currentReadonlyNativeNoEffects(t, f) != "[0, 0, 0, 0, 0]" {
				t.Fatal("expiry bypassed current match fence or created action")
			}
		})
	}
}

// Observe only the specified connection's actual SQL behind the specified
// locker. The predicate never accepts a different fixture or package waiter.
func currentReadonlyNativeWait(t *testing.T, f *privateProfileHTTPDBFixture, ctx context.Context, app, sqlLike string, blocker int) (int, error) {
	t.Helper()
	for {
		var pid int
		var statement string
		e := f.pool.QueryRow(ctx, `SELECT a.pid,a.query FROM pg_stat_activity a WHERE a.datname=current_database()
 AND a.application_name=$1 AND $2::integer=ANY(pg_blocking_pids(a.pid)) AND a.query LIKE $3`, app, blocker, sqlLike).Scan(&pid, &statement)
		if e == nil {
			t.Logf("LOCAL_SYNTHETIC actual SQL serialization requestPID=%d blockerPID=%d SQL=%q", pid, blocker, statement)
			return pid, nil
		}
		if e != pgx.ErrNoRows {
			return 0, e
		}
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
}

func TestCurrentReadonlyNativeOriginalPolicyWriterSerialization(t *testing.T) {
	for _, ownerIndex := range []int{0, 1} {
		t.Run(fmt.Sprintf("writerOwner%d", ownerIndex), func(t *testing.T) {
			f, s, id, query := currentReadonlyNativeSearchFixture(t, "place")
			readerApp := currentReadonlyNativeRequestPool(t, f.privateProfileHTTPDBFixture, s)
			ctx, cancel := context.WithTimeout(f.ctx, 12*time.Second)
			c := f.pool.Config().Copy()
			writerApp := "birdtie_read039_writer_" + f.accountIDs[ownerIndex]
			c.ConnConfig.RuntimeParams["application_name"] = writerApp
			writerPool, e := pgxpool.NewWithConfig(ctx, c)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(writerPool.Close)
			digest, e := identity.ParseBearer("Bearer " + f.tokens[ownerIndex])
			if e != nil {
				t.Fatal(e)
			}
			access := agentprofile.PrivateAccess{WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.accountIDs[ownerIndex]}, SessionDigest: digest}
			settings, e := json.Marshal(agentpolicysettings.AutonomySettings{Level: agentautonomy.LevelAssist})
			if e != nil {
				t.Fatal(e)
			}
			input := agentpolicysettings.PutInput{ExpectedVersion: 0, Settings: settings, ExpiresAt: time.Now().UTC().Add(time.Hour).Truncate(time.Microsecond)}
			writer := postgres.New(writerPool, false)
			var lock pgx.Tx
			done := make(chan error, 1)
			started, consumed := false, false
			t.Cleanup(func() {
				cancel()
				cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
				defer stop()
				if lock != nil {
					_ = lock.Rollback(cleanupCtx)
				}
				if started && !consumed {
					select {
					case <-done:
					case <-cleanupCtx.Done():
						t.Error("native writer observer cleanup timed out")
					}
				}
			})
			before := ""
			finish := func() {
				select {
				case e = <-done:
					consumed = true
					if e != nil {
						t.Fatal(e)
					}
				case <-ctx.Done():
					t.Fatal("native reader/writer did not serialize", ctx.Err())
				}
			}
			s.beforeSearchFinal = func(agenttool.CurrentSearch, *agenttool.CurrentSearchReceipt) {
				// Force the ORIGINAL writer's successful commit before final read.
				// Without this, 200 before a later commit is legitimately current.
				finish()
			}
			s.beforeSearchRead = func() {
				before = currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture)
				lock, e = f.pool.Begin(ctx)
				if e != nil {
					t.Fatal(e)
				}
				var taskID string
				if e = lock.QueryRow(ctx, `SELECT id FROM agent_tasks WHERE owner_account_id=$1 FOR UPDATE`, f.accountIDs[0]).Scan(&taskID); e != nil {
					t.Fatal(e)
				}
				started = true
				go func() {
					var result error
					defer func() {
						cleanupCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
						defer stop()
						_ = lock.Rollback(cleanupCtx)
						done <- result
					}()
					readerPID, err := currentReadonlyNativeWait(t, f.privateProfileHTTPDBFixture, ctx, readerApp, "%FROM agent_tasks%FOR SHARE%", int(lock.Conn().PgConn().PID()))
					if err != nil {
						result = err
						return
					}
					writerDone := make(chan error, 1)
					go func() {
						b, err := writer.PutOwnPolicy(ctx, access, agentpolicysettings.Autonomy, input)
						if err == nil && (b.Autonomy.NativeRevision != 1 || b.OwnerID != f.accountIDs[ownerIndex]) {
							err = fmt.Errorf("wrong original writer version/owner")
						}
						writerDone <- err
					}()
					writerSQL := "%pg_advisory_xact_lock%human-agent-policy%"
					if ownerIndex == 1 {
						writerSQL = "%INSERT INTO agent_policy_settings%"
					}
					_, result = currentReadonlyNativeWait(t, f.privateProfileHTTPDBFixture, ctx, writerApp, writerSQL, readerPID)
					if result == nil && ownerIndex == 1 {
						var exact bool
						result = f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks l JOIN pg_stat_activity a ON a.pid=l.pid WHERE a.application_name=$1 AND l.relation='agent_policy_settings'::regclass AND l.mode='RowExclusiveLock' AND NOT l.granted)`, writerApp).Scan(&exact)
						if result == nil && !exact {
							result = fmt.Errorf("cross-owner table write wait not native RowExclusiveLock")
						}
					}
					// Release the source Task lock; the actual read Tx commits first,
					// then the original writer proceeds. No forced process termination.
					if err = lock.Rollback(ctx); err != nil && result == nil {
						result = err
					}
					select {
					case err = <-writerDone:
						if err != nil && result == nil {
							result = err
						}
					case <-ctx.Done():
						if result == nil {
							result = ctx.Err()
						}
					}
				}()
			}
			want := 200
			if ownerIndex == 0 {
				want = 409
			}
			body, _ := json.Marshal(map[string]string{"query": query})
			w := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", string(body), f.tokens[0], want, nil)
			if !consumed {
				finish()
			}
			if want == 200 {
				resultHTTPFind(t, resultHTTPReply(t, w), "place", id)
			} else {
				currentReadonlyNativeNoBody(t, w, id)
			}
			if s.searchReads != 1 || s.searchFinal != 1 || before != currentReadonlyNativeNoEffects(t, f.privateProfileHTTPDBFixture) {
				t.Fatal("concurrent native read skipped fence or created effect")
			}
		})
	}
}

func TestCurrentReadonlyNativeActivityReuseLatestAttendanceAndChanges(t *testing.T) {
	f := contextBuilderHTTPNative(t)
	s := &currentReadonlyNativeHTTP{Store: f.store}
	f.handler = currentReadonlyNativeHandler(f.privateProfileHTTPDBFixture, s)
	f.exec(`UPDATE activities SET capacity=2 WHERE id=$1`, f.public)
	t.Cleanup(func() {
		if _, e := f.pool.Exec(context.Background(), `DELETE FROM activity_participations WHERE participant_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
		}
	})
	read := func(path, method, body string, wantCount, wantCapacity int, wantTitle string) time.Time {
		t.Helper()
		w := f.request(t, f.handler, method, path, body, f.tokens[0], 200, nil)
		r := resultHTTPReply(t, w)
		item := resultHTTPFind(t, r, "activity", f.public)
		for _, a := range r.Activities {
			if a.ID != f.public {
				continue
			}
			if a.Capacity == nil || *a.Capacity != wantCapacity || a.ParticipantCount == nil || *a.ParticipantCount != wantCount || a.Title != wantTitle || item.Title != wantTitle {
				t.Fatalf("current original Activity values diverged: capacity=%v count=%v title=%q typed=%q", a.Capacity, a.ParticipantCount, a.Title, item.Title)
			}
			return a.StartsAt
		}
		t.Fatal("original Activity DTO missing")
		return time.Time{}
	}
	post := f.request(t, f.handler, "POST", "/v1/cities/"+f.city+"/agent/tasks", `{"query":"帮我找羽毛球活动"}`, f.tokens[0], 200, nil)
	initial := resultHTTPReply(t, post)
	get := "/v1/me/agent-tasks/" + initial.Task.ID
	oldStart := read(get, "GET", "", 0, 2, "合成羽毛球 public")
	if joined, created, e := f.store.JoinActivity(f.ctx, f.accountIDs[0], f.public); e != nil || !created || joined.ActivityID != f.public || joined.Status != "going" {
		t.Fatal("original explicit local RSVP writer failed", e)
	}
	// This privileged fixture edit provides real changed DB facts; it is not
	// evidence of organizer UI or production publishing authorization.
	f.exec(`UPDATE activities SET title='039原活动改期且已满',capacity=1,starts_at=starts_at+interval '30 minutes',ends_at=ends_at+interval '30 minutes',updated_at=clock_timestamp() WHERE id=$1`, f.public)
	newStart := read(get, "GET", "", 1, 1, "039原活动改期且已满")
	if !newStart.Equal(oldStart.Add(30 * time.Minute)) {
		t.Fatal("old schedule survived current read")
	}
	if _, e := f.store.CancelParticipation(f.ctx, f.accountIDs[0], f.public); e != nil {
		t.Fatal(e)
	}
	read(get, "GET", "", 0, 1, "039原活动改期且已满")
	unknown := f.request(t, f.handler, "GET", "/v1/activities/"+f.accountIDs[2], "", f.tokens[0], 404, nil)
	currentReadonlyNativeNoBody(t, unknown, f.public)
	f.exec(`UPDATE activity_invitations SET status='revoked' WHERE activity_id=$1 AND invitee_account_id=$2`, f.private, f.accountIDs[0])
	f.exec(`UPDATE activities SET cancelled_at=clock_timestamp(),updated_at=clock_timestamp() WHERE id=$1`, f.public)
	empty := f.request(t, f.handler, "GET", get, "", f.tokens[0], 200, nil)
	r := resultHTTPReply(t, empty)
	if r.ResultSet.Status != "empty" || len(r.Activities)+len(r.ResultSet.Items) != 0 || strings.Contains(empty.Body.String(), f.public) || strings.Contains(empty.Body.String(), f.private) {
		t.Fatal("cancelled/invitation-revoked originals leaked or became fictional capacity", empty.Body.String())
	}
	if s.searchReads+s.searchFinal != 0 || s.legacyReads != 5 || s.legacyFinal != 5 {
		t.Fatal("human Activity invitation ACL was replaced by Place/Person port")
	}
}
