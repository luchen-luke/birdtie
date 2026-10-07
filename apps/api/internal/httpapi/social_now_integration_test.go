package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/opportunity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"os"
	"testing"
	"time"
)

// Offline transport companion for the pre-existing rule-engine contract spy.
// This adapter proves wire compatibility only; real authority is tested below
// against the native Store/session/source rows, never by this fake implementation.
func (s *opportunityReasonsStore) ListHumanOpportunities(ctx context.Context, digest [32]byte, actor identity.Actor) ([]opportunity.Candidate, error) {
	if digest == ([32]byte{}) || actor.AccountType != "person" {
		return nil, identity.ErrUnauthorized
	}
	in, e := s.LoadOpportunityInputs(ctx, actor.ID)
	if e != nil {
		return nil, e
	}
	return opportunity.Generate(time.Now(), in), nil
}

// Offline contract only: it preserves the pre-existing transport spy's actor.
func (a *opportunityReasonsAccess) AuthenticateHumanSocial(ctx context.Context, d [32]byte) (identity.Actor, error) {
	return a.Authenticate(ctx, d)
}
func (a *opportunityReasonsAccess) ValidateHumanSocialResponse(ctx context.Context, d [32]byte, actor identity.Actor) error {
	current, e := a.Authenticate(ctx, d)
	if e != nil {
		return e
	}
	if current.ID != actor.ID || current.AccountType != actor.AccountType {
		return identity.ErrUnauthorized
	}
	return ctx.Err()
}

// A barrier around the actual AccessStore, not an authorization substitute.
type socialNowFinalAccessBarrier struct {
	identity.AccessStore

	entered chan struct{}
	release chan struct{}
}

func (a *socialNowFinalAccessBarrier) AuthenticateHumanSocial(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return a.AccessStore.(interface {
		AuthenticateHumanSocial(context.Context, [32]byte) (identity.Actor, error)
	}).AuthenticateHumanSocial(ctx, digest)
}
func (a *socialNowFinalAccessBarrier) ValidateHumanSocialResponse(ctx context.Context, digest [32]byte, actor identity.Actor) error {
	close(a.entered)
	select {
	case <-a.release:
	case <-ctx.Done():
		return ctx.Err()
	}
	return a.AccessStore.(interface {
		ValidateHumanSocialResponse(context.Context, [32]byte, identity.Actor) error
	}).ValidateHumanSocialResponse(ctx, digest, actor)
}

func TestSocialNowHTTPLateSessionCurrentGate(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires owned disposable DB")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	_, tag, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	app := fmt.Sprintf("social-now-%x", tag[:8])
	cfg.ConnConfig.RuntimeParams["application_name"] = app
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	city, places, cleanupPlaces := ownedSocialPlacesFixture(t, ctx, pool, 1)
	defer cleanupPlaces()
	ids := make([]string, 2)
	var activity string
	defer func() {
		if activity != "" {
			ownedSocialFixtureCleanup(t, ctx, pool, `DELETE FROM activities WHERE id=$1`, activity)
		}
		for _, sql := range []string{
			`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`,
			`DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`,
			`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`,
			`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`,
			`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`,
			`DELETE FROM accounts WHERE id=ANY($1::uuid[])`,
		} {
			ownedSocialFixtureCleanup(t, ctx, pool, sql, ids)
		}
	}()
	for i := range ids {
		if e := pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id`).Scan(&ids[i]); e != nil {
			t.Fatal(e)
		}
		if _, e := pool.Exec(ctx, `INSERT INTO user_profiles(account_id,display_name,visibility) VALUES($1,'Owned synthetic Social Now person','public')`, ids[i]); e != nil {
			t.Fatal(e)
		}
	}
	store := postgres.New(pool, false)
	req, err := store.CreateFriendRequest(ctx, ids[0], ids[1], "Owned friend connection")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.DecideRequest(ctx, ids[1], req.ID, "accept"); err != nil {
		t.Fatal(err)
	}
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	draft, err := store.CreateSocialDraft(ctx, ids[1], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: ids[1]}, CityID: city, PlaceID: places[0], Title: "Owned Social Now activity", Summary: "Synthetic current source", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "UTC", Visibility: "public", Modality: "in_person", PhysicalPlaceStatus: "confirmed"})
	if err != nil {
		t.Fatal(err)
	}
	activity = draft.ID
	if _, err = store.PublishSocialActivity(ctx, ids[1], activity); err != nil {
		t.Fatal(err)
	}
	constraints, err := json.Marshal(map[string]string{"placeId": places[0]})
	if err != nil {
		t.Fatal(err)
	}
	own, err := store.CreateSocialIntentDraft(ctx, ids[0], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "Owned activity intent", Constraints: constraints, Audience: "PRIVATE", Modality: "IN_PERSON", ExpiresAt: time.Now().Add(48 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ActivateSocialIntent(ctx, ids[0], own.ID); err != nil {
		t.Fatal(err)
	}
	shared, err := store.CreateSocialIntentDraft(ctx, ids[1], socialintent.DraftInput{Type: "FIND_COMPANION", Title: "Owned visible intent", Constraints: json.RawMessage(`{}`), Audience: "FRIENDS", Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.ActivateSocialIntent(ctx, ids[1], shared.ID); err != nil {
		t.Fatal(err)
	}
	mux := New(store, store, store, store, store, store, store, store, store, store, store, store, false, nil, pool, nil)
	for _, test := range []struct{ name, path, table string }{
		{"ties", "/v1/me/ties", "person_ties"}, {"visibleIntents", "/v1/social-intents", "social_intents"}, {"visibleIntentDetail", "/v1/social-intents/" + shared.ID, "social_intents"}, {"opportunities", "/v1/me/opportunities", "activities"},
	} {
		t.Run(test.name, func(t *testing.T) {
			token, digest, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, ids[0], digest[:]); e != nil {
				t.Fatal(e)
			}
			call := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", test.path, nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			if w := call(); w.Code != 200 {
				t.Fatalf("initial current source status=%d body=%s", w.Code, w.Body)
			}
			lock, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(ctx)
			if _, e = lock.Exec(ctx, `LOCK TABLE `+test.table+` IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- call() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
				if e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("actual owned source barrier not reached")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if _, e = pool.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, digest[:]); e != nil {
				t.Fatal(e)
			}
			if e = lock.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code == 200 {
					t.Fatalf("session revoked during actual native source wait still returns payload: status=%d body=%s", w.Code, w.Body)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native read did not complete")
			}
		})
	}
	for _, change := range []string{"cancelIntent", "removeTie"} {
		t.Run(change, func(t *testing.T) {
			token, digest, e := identity.NewToken()
			if e != nil {
				t.Fatal(e)
			}
			if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, ids[0], digest[:]); e != nil {
				t.Fatal(e)
			}
			call := func() *httptest.ResponseRecorder {
				r := httptest.NewRequest("GET", "/v1/me/opportunities", nil)
				r.Header.Set("Authorization", "Bearer "+token)
				w := httptest.NewRecorder()
				mux.ServeHTTP(w, r)
				return w
			}
			if w := call(); w.Code != 200 {
				t.Fatalf("current source precondition: %d %s", w.Code, w.Body)
			}
			lock, e := pool.Begin(ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(ctx)
			if _, e = lock.Exec(ctx, `LOCK TABLE activities IN ACCESS EXCLUSIVE MODE`); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() { done <- call() }()
			deadline := time.Now().Add(5 * time.Second)
			for {
				var waiting bool
				e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
				if e != nil {
					t.Fatal(e)
				}
				if waiting {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("actual owned activity source barrier not reached")
				}
				time.Sleep(10 * time.Millisecond)
			}
			if change == "cancelIntent" {
				if _, e = store.CancelSocialIntent(ctx, ids[0], own.ID); e != nil {
					t.Fatal(e)
				}
				// Restore only this owned fixture for the independent next case;
				// this is not a user API or approval to reactivate a cancelled intent.
				defer ownedSocialFixtureCleanup(t, ctx, pool, `UPDATE social_intents SET status='ACTIVE' WHERE id=$1`, own.ID)
			} else {
				ties, e := store.ListTies(ctx, ids[0])
				if e != nil || len(ties) != 1 {
					t.Fatalf("current own tie: %+v %v", ties, e)
				}
				if e = store.RemoveTie(ctx, ids[0], ties[0].ID); e != nil {
					t.Fatal(e)
				}
			}
			if e = lock.Commit(ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 200 {
					return
				} // A denied current-source read is also fail closed.
				var envelope struct {
					Data []struct {
						IntentID    string   `json:"intentId"`
						ReasonCodes []string `json:"reasonCodes"`
					}
				}
				if e = json.Unmarshal(w.Body.Bytes(), &envelope); e != nil {
					t.Fatal(e)
				}
				for _, item := range envelope.Data {
					if change == "cancelIntent" && item.IntentID == own.ID {
						t.Fatalf("cancelled actual intent still yields opportunity: %s", w.Body)
					}
					for _, code := range item.ReasonCodes {
						if change == "removeTie" && code == "TIE_ORGANIZER" {
							t.Fatalf("removed actual Tie still ranks as friend: %s", w.Body)
						}
					}
				}
			case <-time.After(5 * time.Second):
				t.Fatal("native source read did not complete")
			}
		})
	}
	t.Run("realAccountRowWaitAndNaturalSessionExpiry", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS n) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',n+interval '2 seconds',n+interval '2 seconds' FROM clock`, ids[0], digest[:]); e != nil {
			t.Fatal(e)
		}
		lock, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(ctx)
		var id string
		if e = lock.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, ids[0]).Scan(&id); e != nil {
			t.Fatal(e)
		}
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			r := httptest.NewRequest("GET", "/v1/me/ties", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			done <- w
		}()
		deadline := time.Now().Add(4 * time.Second)
		for {
			var waiting bool
			e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
			if e != nil {
				t.Fatal(e)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("real Account row wait not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var expired bool
			e = pool.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired)
			if e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("native DB clock expiry not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = lock.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case w := <-done:
			if w.Code != 401 {
				t.Fatalf("expired during real Account row wait: %d %s", w.Code, w.Body)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("read after row wait did not complete")
		}
	})
	t.Run("realInitialAccountWaitAndIdleExpiry", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS n) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',n+interval '1 hour',n+interval '2 seconds' FROM clock`, ids[0], digest[:]); e != nil {
			t.Fatal(e)
		}
		lock, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(ctx)
		var id string
		if e = lock.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, ids[0]).Scan(&id); e != nil {
			t.Fatal(e)
		}
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			r := httptest.NewRequest("GET", "/v1/me/ties", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			mux.ServeHTTP(w, r)
			done <- w
		}()
		deadline := time.Now().Add(4 * time.Second)
		for {
			var waiting bool
			e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)))`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
			if e != nil {
				t.Fatal(e)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("real Account row wait not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var expired bool
			e = pool.QueryRow(ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired)
			if e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("native DB clock expiry not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = lock.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case w := <-done:
			if w.Code != 401 {
				t.Fatalf("expired during real Account row wait: %d %s", w.Code, w.Body)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("read after row wait did not complete")
		}
	})
	t.Run("materializedJSONThenFinalAuthenticationRowWaitExpiry", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS n) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',n+interval '2 seconds',n+interval '2 seconds' FROM clock`, ids[0], digest[:]); e != nil {
			t.Fatal(e)
		}
		barrier := &socialNowFinalAccessBarrier{AccessStore: store, entered: make(chan struct{}), release: make(chan struct{})}
		finalMux := New(store, barrier, store, store, store, store, store, store, store, store, store, store, false, nil, pool, nil)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			r := httptest.NewRequest("GET", "/v1/me/ties", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			finalMux.ServeHTTP(w, r)
			done <- w
		}()
		select {
		case <-barrier.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("post-JSON real final authentication not reached")
		}
		lock, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(ctx)
		var session string
		if e = lock.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]).Scan(&session); e != nil {
			t.Fatal(e)
		}
		close(barrier.release)
		deadline := time.Now().Add(4 * time.Second)
		for {
			var waiting bool
			e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT id FROM sessions%' AND query LIKE '%FOR SHARE%')`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
			if e != nil {
				t.Fatal(e)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("post-JSON actual Session UPDATE row wait not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var expired bool
			e = pool.QueryRow(ctx, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired)
			if e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("DB clock natural final expiry not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = lock.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case w := <-done:
			if w.Code != 401 {
				t.Fatalf("materialized payload escaped final row-wait expiry: %d %s", w.Code, w.Body)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("post-JSON final read did not complete")
		}
	})
	t.Run("materializedJSONThenFinalAuthenticationIdleRowWaitExpiry", func(t *testing.T) {
		token, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		if _, e = pool.Exec(ctx, `WITH clock AS MATERIALIZED(SELECT clock_timestamp() AS n) INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) SELECT $1,$2,'test',n+interval '1 hour',n+interval '2 seconds' FROM clock`, ids[0], digest[:]); e != nil {
			t.Fatal(e)
		}
		barrier := &socialNowFinalAccessBarrier{AccessStore: store, entered: make(chan struct{}), release: make(chan struct{})}
		finalMux := New(store, barrier, store, store, store, store, store, store, store, store, store, store, false, nil, pool, nil)
		done := make(chan *httptest.ResponseRecorder, 1)
		go func() {
			r := httptest.NewRequest("GET", "/v1/me/ties", nil)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			finalMux.ServeHTTP(w, r)
			done <- w
		}()
		select {
		case <-barrier.entered:
		case <-time.After(5 * time.Second):
			t.Fatal("post-JSON real final authentication not reached")
		}
		// First authenticated request legitimately refreshed idle; shorten the
		// owned source only after payload materialization, before actual wait.
		if _, e = pool.Exec(ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '2 seconds' WHERE token_sha256=$1`, digest[:]); e != nil {
			t.Fatal(e)
		}
		lock, e := pool.Begin(ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer lock.Rollback(ctx)
		var session string
		if e = lock.QueryRow(ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]).Scan(&session); e != nil {
			t.Fatal(e)
		}
		close(barrier.release)
		deadline := time.Now().Add(4 * time.Second)
		for {
			var waiting bool
			e = pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE datname=current_database() AND application_name=$1 AND $2::integer=ANY(pg_blocking_pids(pid)) AND query LIKE '%SELECT id FROM sessions%' AND query LIKE '%FOR SHARE%')`, app, int(lock.Conn().PgConn().PID())).Scan(&waiting)
			if e != nil {
				t.Fatal(e)
			}
			if waiting {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("post-JSON actual Session UPDATE row wait not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		for {
			var expired bool
			e = pool.QueryRow(ctx, `SELECT idle_expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:]).Scan(&expired)
			if e != nil {
				t.Fatal(e)
			}
			if expired {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("DB clock natural final expiry not reached")
			}
			time.Sleep(10 * time.Millisecond)
		}
		if e = lock.Commit(ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case w := <-done:
			if w.Code != 401 {
				t.Fatalf("materialized payload escaped final row-wait expiry: %d %s", w.Code, w.Body)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("post-JSON final read did not complete")
		}
	})
}
