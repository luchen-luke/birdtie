package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/newpeople"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Separate real database, not a authorization fixture or a runtime schema downgrade.
func v4PrivacyHTTPDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("explicit owned disposable database required; no skipped privacy verification")
	}
	u, e := url.Parse(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		t.Fatal(e)
	}
	cfg.ConnConfig.Database = "postgres"
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	t.Cleanup(cancel)
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal(e)
	}
	var b [12]byte
	if _, e = rand.Read(b[:]); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	name := "birdtie_saf001_http_" + hex.EncodeToString(b[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+quoted); e != nil {
		admin.Close()
		t.Fatal(e)
	}
	var own *pgxpool.Pool
	t.Cleanup(func() {
		if own != nil {
			own.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+quoted); e != nil {
			t.Error("owned database DROP failed", e)
		} else {
			t.Log("SAF001 owned HTTP DB DROP true")
		}
		admin.Close()
	})
	u.Path = "/" + name
	u.RawPath = ""
	child := u.String()
	own, e = pgxpool.New(ctx, child)
	if e != nil {
		t.Fatal(e)
	}
	files, e := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(files)
	count := 0
	for _, f := range files {
		if strings.HasSuffix(f, ".down.sql") {
			continue
		}
		data, e := os.ReadFile(f)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = own.Exec(ctx, string(data)); e != nil {
			t.Fatal(filepath.Base(f), e)
		}
		count++
		seeds := []string{}
		if strings.HasPrefix(filepath.Base(f), "029_") {
			seeds = []string{"001_badminton.sql", "002_functional_mvp.sql"}
		}
		if strings.HasPrefix(filepath.Base(f), "033_") {
			seeds = []string{"003_community_social.sql"}
		}
		for _, seed := range seeds {
			data, e := os.ReadFile(filepath.Join("..", "..", "dev-seeds", seed))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = own.Exec(ctx, string(data)); e != nil {
				t.Fatal(seed, e)
			}
		}
	}
	if count < 83 {
		t.Fatal("current native schema83 required")
	}
	t.Logf("SAF001 HTTP real migrations=%d original seeds=3", count)
	t.Setenv("BIRDTIE_DATABASE_URL", child)
}

type v4PrivacyCandidates struct {
	*postgres.Store
	after func()
	afterCalls int
}

// Exercise the actual registered CurrentMatchPort, not an older fallback.
func (s *v4PrivacyCandidates) ReadOwnCurrentMatch(ctx context.Context, input agenttool.CurrentMatch) (agenttool.CurrentMatchReceipt, error) {
	v, e := s.Store.ReadOwnCurrentMatch(ctx, input)
	if e == nil && s.after != nil {
		s.afterCalls++
		s.after()
	}
	return v, e
}

func (s *v4PrivacyCandidates) FindNewPeople(ctx context.Context, owner, source string) (newpeople.Response, error) {
	v, e := s.Store.FindNewPeople(ctx, owner, source)
	if e == nil && s.after != nil {
		s.after()
	}
	return v, e
}

func (s *v4PrivacyCandidates) ReadHumanNewPeople(ctx context.Context, actor identity.Actor, digest [32]byte, source string) (newpeople.HumanReceipt, error) {
	v, e := s.Store.ReadHumanNewPeople(ctx, actor, digest, source)
	if e == nil && s.after != nil {
		s.after()
	}
	return v, e
}

func v4PrivacyHTTPPeople(t *testing.T) (*privateProfileHTTPDBFixture, socialintent.Record, socialintent.Record) {
	t.Helper()
	v4PrivacyHTTPDatabase(t)
	f := privateProfileHTTPDBNew(t)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM person_ties WHERE person_a_account_id=ANY($1::uuid[]) OR person_b_account_id=ANY($1::uuid[])`, `DELETE FROM connection_requests WHERE sender_account_id=ANY($1::uuid[]) OR recipient_account_id=ANY($1::uuid[])`, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM person_new_people_consent WHERE account_id=ANY($1::uuid[])`} {
			f.exec(q, f.accountIDs)
		}
	})
	var rows []socialintent.Record
	for _, id := range f.accountIDs[:2] {
		if _, e := f.store.SetNewPeopleConsent(f.ctx, id, true); e != nil {
			t.Fatal(e)
		}
		v, e := f.store.CreateNewPeopleIntent(f.ctx, id, newpeople.DraftInput{Title: "SAF001_PRIVATE_SOURCE_TITLE", Category: "badminton", Modality: "ONLINE", OnlinePlatform: "SAF001_PRIVATE_PLATFORM_57.123456789_-2.987654321", ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		v, e = f.store.ActivateSocialIntent(f.ctx, id, v.ID)
		if e != nil {
			t.Fatal(e)
		}
		rows = append(rows, v)
	}
	return f, rows[0], rows[1]
}
func v4PrivacyHTTPApp(f *privateProfileHTTPDBFixture, c *v4PrivacyCandidates) http.Handler {
	return New(c, f.store, nil, f.store, f.store, f.store, f.store, f.store, f.store, f.store, f.store, nil, false, nil, nil, nil)
}
func v4PrivacyHTTPGet(h http.Handler, path, token string) *httptest.ResponseRecorder {
	r := httptest.NewRequest("GET", path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}
func v4PrivacyWire(t *testing.T, raw string) {
	t.Helper()
	for _, canary := range []string{"SAF001_PRIVATE_SOURCE_TITLE", "SAF001_PRIVATE_PLATFORM", "57.123456789", "-2.987654321", "latitude", "longitude", "areaLabel", "onlinePlatform", "person_contexts", "token_sha256", "privateCityHistory"} {
		if strings.Contains(raw, canary) {
			t.Fatal("private projection leak", canary, raw)
		}
	}
}

func TestV4ProjectionPrivacyHTTPNativeRegisteredMinimumAndExplicitConsent(t *testing.T) {
	f, source, peer := v4PrivacyHTTPPeople(t)
	c := &v4PrivacyCandidates{Store: f.store}
	h := v4PrivacyHTTPApp(f, c)
	path := "/v1/me/new-people/candidates?sourceIntentId=" + source.ID
	w := v4PrivacyHTTPGet(h, path, f.tokens[0])
	if w.Code != 200 || !strings.Contains(w.Body.String(), peer.ID) {
		t.Fatal("real compatible peer missing", w.Code, w.Body.String())
	}
	v4PrivacyWire(t, w.Body.String())
	if _, e := f.store.SetNewPeopleConsent(f.ctx, f.accountIDs[1], false); e != nil {
		t.Fatal(e)
	}
	w = v4PrivacyHTTPGet(h, path, f.tokens[0])
	if w.Code != 200 || strings.Contains(w.Body.String(), peer.ID) {
		t.Fatal("opt-out peer exposed", w.Code, w.Body.String())
	}
	if _, e := f.store.SetNewPeopleConsent(f.ctx, f.accountIDs[1], true); e != nil {
		t.Fatal(e)
	}
	w = v4PrivacyHTTPGet(h, path, f.tokens[1])
	if w.Code != 404 {
		t.Fatal("different owner borrowed source", w.Code, w.Body.String())
	}
	// No GPS input may be silently accepted as location authority or matching data.
	body, _ := json.Marshal(map[string]any{"title": "本人拒绝GPS仍可线上", "category": "badminton", "modality": "ONLINE", "onlinePlatform": "合成", "expiresAt": time.Now().Add(time.Hour), "latitude": 57.123456789, "longitude": -2.987654321})
	r := httptest.NewRequest("POST", "/v1/me/new-people/intents", strings.NewReader(string(body)))
	r.Header.Set("Authorization", "Bearer "+f.tokens[0])
	r.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 400 {
		t.Fatal("arbitrary user coordinates accepted", w.Code, w.Body.String())
	}
}

func TestV4ProjectionPrivacyHTTPNativeCandidateLastBoundary(t *testing.T) {
	for _, kind := range []string{"session_revoke", "peer_opt_out", "forward_block", "reverse_block", "peer_cancel"} {
		t.Run(kind, func(t *testing.T) {
			f, source, peer := v4PrivacyHTTPPeople(t)
			c := &v4PrivacyCandidates{Store: f.store}
			path := "/v1/me/new-people/candidates?sourceIntentId=" + source.ID
			c.after = func() {
				switch kind {
				case "session_revoke":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "peer_opt_out":
					if _, e := f.store.SetNewPeopleConsent(f.ctx, f.accountIDs[1], false); e != nil {
						t.Fatal(e)
					}
				case "forward_block":
					if e := f.store.BlockAccount(f.ctx, f.accountIDs[0], f.accountIDs[1]); e != nil {
						t.Fatal(e)
					}
				case "reverse_block":
					if e := f.store.BlockAccount(f.ctx, f.accountIDs[1], f.accountIDs[0]); e != nil {
						t.Fatal(e)
					}
				case "peer_cancel":
					if _, e := f.store.CancelSocialIntent(f.ctx, f.accountIDs[1], peer.ID); e != nil {
						t.Fatal(e)
					}
				}
			}
			w := v4PrivacyHTTPGet(v4PrivacyHTTPApp(f, c), path, f.tokens[0])
			if c.afterCalls != 1 {
				t.Fatalf("actual current read mutation hook calls=%d", c.afterCalls)
			}
			if w.Code == 200 && strings.Contains(w.Body.String(), peer.ID) {
				t.Fatalf("native current privacy revocation released stale candidate: %s status=%d body=%s", kind, w.Code, w.Body.String())
			}
			expected := 409
			if kind == "session_revoke" {
				// Authentication passed before the hook; the current native read
				// now rejects its original Session with ErrDenied, mapped to 403.
				expected = 403
			}
			if w.Code != expected {
				t.Fatalf("current native denial wrong status %s: got %d want %d body=%s", kind, w.Code, expected, w.Body.String())
			}
			if strings.Contains(w.Body.String(), peer.ID) || strings.Contains(w.Body.String(), f.accountIDs[1]) {
				t.Fatal("stale peer identifier in denied response", w.Body.String())
			}
			v4PrivacyWire(t, w.Body.String())
		})
	}
}
