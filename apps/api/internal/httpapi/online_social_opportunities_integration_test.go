package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/activitypublish"
	oso "github.com/birdtie/birdtie/apps/api/internal/onlinesocialopportunity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/birdtie/birdtie/apps/api/internal/socialintent"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestOnlineSocialOpportunityHTTPNativeRegisteredOrdinaryReadAndClosedWire(t *testing.T) {
	b := privateProfileHTTPDBNew(t)
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM native_notification_decisions WHERE recipient_id=ANY($1::uuid[])`, `DELETE FROM inbox_items WHERE recipient_account_id=ANY($1::uuid[])`, `DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), q, b.accountIDs); e != nil {
				t.Error(e)
			}
		}
	})
	makeIntent := func(index int, audience string) string {
		r, e := b.store.CreateSocialIntentDraft(b.ctx, b.accountIDs[index], socialintent.DraftInput{Type: "FIND_COMPANION", Title: "badminton 线上合成交流", Constraints: json.RawMessage(`{"category":"badminton"}`), Audience: audience, Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.ActivateSocialIntent(b.ctx, b.accountIDs[index], r.ID); e != nil {
			t.Fatal(e)
		}
		return r.ID
	}
	own := makeIntent(0, "PRIVATE")
	peer := makeIntent(1, "PUBLIC")
	options := "/v1/me/online-social-opportunities/options"
	path := "/v1/me/online-social-opportunities/" + own
	for _, p := range []string{options, path} {
		w := historicalHTTPCall(b, "GET", p, "", b.tokens[0])
		if w.Code != 200 || w.Header().Get("Cache-Control") != "no-store" {
			t.Fatal("registered source", w.Code, w.Body.String())
		}
		var response struct{ Data oso.View }
		if json.Unmarshal(w.Body.Bytes(), &response) != nil || oso.Validate(response.Data) != nil || response.Data.OwnerID != b.accountIDs[0] {
			t.Fatal("actual typed DTO", w.Body.String())
		}
		if p == path && (len(response.Data.Items) != 1 || response.Data.Items[0].Source.ID != peer) {
			t.Fatal("real original source ref", w.Body.String())
		}
		for _, secret := range []string{"constraints", "creatorAccountId", "token_sha256", "Proof", "Seal", "latitude", "longitude", "AgentTask"} {
			if strings.Contains(w.Body.String(), secret) {
				t.Fatal("private transport", secret)
			}
		}
	}
	for _, token := range []string{"", "bts1_invalid"} {
		if w := historicalHTTPCall(b, "GET", path, "", token); w.Code != 401 {
			t.Fatal("anonymous/authenticated fallback", w.Code)
		}
	}
	if w := historicalHTTPCall(b, "GET", path, "", b.tokens[1]); w.Code != 403 {
		t.Fatal("cross-owner", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"?", "?ownerId=" + b.accountIDs[1], "?latitude=57", "?intentId=" + peer} {
		if w := historicalHTTPCall(b, "GET", path+suffix, "", b.tokens[0]); w.Code != 400 {
			t.Fatal("unknown parameter", suffix, w.Code)
		}
	}
	if w := historicalHTTPCall(b, "GET", path, `{"ownerId":"ignored"}`, b.tokens[0]); w.Code != 400 {
		t.Fatal("GET body", w.Code)
	}
	b.exec(`UPDATE social_intents SET status='CANCELLED' WHERE id=$1`, peer)
	w := historicalHTTPCall(b, "GET", path, "", b.tokens[0])
	if w.Code != 200 || strings.Contains(w.Body.String(), peer) {
		t.Fatal("current withdrawal", w.Code, w.Body.String())
	}
	b.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, b.accountIDs[0])
	if w := historicalHTTPCall(b, "GET", path, "", b.tokens[0]); w.Code != 401 {
		t.Fatal("revoked source", w.Code)
	}
}

// No secrets go in arguments, logs, receipts or public files. The parent has an
// explicitly owned disposable database; each child starts its own real HTTP
// server on a new loopback listener, exits, then a second process reopens IDs.
func TestOnlineSocialOpportunityHTTPNativeTwoProcessRestartAndActivityAction(t *testing.T) {
	b := privateProfileHTTPDBNew(t)
	city := "online005-http-" + strings.ReplaceAll(b.accountIDs[0], "-", "")
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'跨城合成线上来源','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC','local:NOW005','合成维护者',$2)`, city, b.accountIDs[1])
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM social_intents WHERE creator_account_id=ANY($1::uuid[])`, `DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, `DELETE FROM activities WHERE created_by_account_id=ANY($1::uuid[])`, `DELETE FROM cities WHERE maintainer_account_id=ANY($1::uuid[])`} {
			if _, e := b.pool.Exec(context.Background(), q, b.accountIDs); e != nil {
				t.Error("owned source cleanup", e)
			}
		}
	})
	makeIntent := func(index int, audience string) string {
		r, e := b.store.CreateSocialIntentDraft(b.ctx, b.accountIDs[index], socialintent.DraftInput{Type: "FIND_ACTIVITY", Title: "合成线上 badminton", Constraints: json.RawMessage(`{"category":"badminton"}`), Audience: audience, Modality: "ONLINE", ExpiresAt: time.Now().Add(time.Hour)})
		if e != nil {
			t.Fatal(e)
		}
		if _, e = b.store.ActivateSocialIntent(b.ctx, b.accountIDs[index], r.ID); e != nil {
			t.Fatal(e)
		}
		return r.ID
	}
	own, peer := makeIntent(0, "PRIVATE"), makeIntent(1, "PUBLIC")
	start := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	act, e := b.store.CreateSocialDraft(b.ctx, b.accountIDs[1], activitypublish.Input{Organizer: activitypublish.Organizer{Type: "PERSON", ID: b.accountIDs[1]}, CityID: city, Title: "异地合成 online badminton", Summary: "详情字段不在机会DTO", CategoryCode: "badminton", StartsAt: start, EndsAt: start.Add(time.Hour), TimeZone: "Europe/London", Visibility: "public", Modality: "online", PhysicalPlaceStatus: "not_applicable"})
	if e != nil {
		t.Fatal(e)
	}
	act, e = b.store.PublishSocialActivity(b.ctx, b.accountIDs[1], act.ID)
	if e != nil || act.PlaceID != nil {
		t.Fatal("online noPlace", e)
	}
	exe, e := os.Executable()
	if e != nil {
		t.Fatal("test executable")
	}
	ownedConfig, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(ownedConfig.ConnConfig.Database, "birdtie_") || (ownedConfig.ConnConfig.Host != "127.0.0.1" && ownedConfig.ConnConfig.Host != "localhost") {
		t.Fatal("owned loopback fixture boundary")
	}
	for round := 1; round <= 2; round++ {
		cmd := exec.CommandContext(b.ctx, exe, "-test.run=^TestOnlineSocialOpportunityHTTPNativeRestartChild$", "-test.v")
		cmd.Env = append(os.Environ(), "BIRDTIE_ONLINE005_CHILD=owned-native-http", "BIRDTIE_ONLINE005_CHILD_DATABASE="+ownedConfig.ConnConfig.Database, "BIRDTIE_ONLINE005_CHILD_TOKEN="+b.tokens[0], "BIRDTIE_ONLINE005_CHILD_OWNER="+b.accountIDs[0], "BIRDTIE_ONLINE005_CHILD_INTENT="+own, "BIRDTIE_ONLINE005_CHILD_PEER="+peer, "BIRDTIE_ONLINE005_CHILD_ACTIVITY="+act.ID)
		output, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatal("independent owned HTTP process failed", round, string(output))
		}
		t.Logf("process %d exited; %s", round, strings.TrimSpace(string(output)))
	}
}

func TestOnlineSocialOpportunityHTTPNativeRestartChild(t *testing.T) {
	if os.Getenv("BIRDTIE_ONLINE005_CHILD") == "" {
		return
	}
	if os.Getenv("BIRDTIE_ONLINE005_CHILD") != "owned-native-http" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("owned child boundary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil || !strings.HasPrefix(cfg.ConnConfig.Database, "birdtie_") || cfg.ConnConfig.Database != os.Getenv("BIRDTIE_ONLINE005_CHILD_DATABASE") || (cfg.ConnConfig.Host != "127.0.0.1" && cfg.ConnConfig.Host != "localhost") {
		t.Fatal("owned database selector")
	}
	p, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		t.Fatal("owned child pool")
	}
	defer p.Close()
	st := postgres.New(p, false)
	srv := httptest.NewServer(privateProfileHTTPNew(st, st))
	defer srv.Close()
	own, peer, activityID := os.Getenv("BIRDTIE_ONLINE005_CHILD_INTENT"), os.Getenv("BIRDTIE_ONLINE005_CHILD_PEER"), os.Getenv("BIRDTIE_ONLINE005_CHILD_ACTIVITY")
	for _, path := range []string{"/v1/me/online-social-opportunities/" + own, "/v1/activities/" + activityID} {
		req, e := http.NewRequestWithContext(ctx, "GET", srv.URL+path, nil)
		if e != nil {
			t.Fatal("request")
		}
		req.Header.Set("Authorization", "Bearer "+os.Getenv("BIRDTIE_ONLINE005_CHILD_TOKEN"))
		response, e := srv.Client().Do(req)
		if e != nil {
			t.Fatal("owned HTTP network")
		}
		body, e := io.ReadAll(response.Body)
		response.Body.Close()
		if e != nil || response.StatusCode != 200 {
			t.Fatal("original source unavailable", path, response.StatusCode, string(body))
		}
		rid := response.Header.Get("X-Request-ID")
		if !safeRequestID.MatchString(rid) {
			t.Fatal("real request ID absent")
		}
		if strings.Contains(path, "online-social-opportunities") {
			var wire struct{ Data oso.View }
			if json.Unmarshal(body, &wire) != nil || oso.Validate(wire.Data) != nil || wire.Data.OwnerID != os.Getenv("BIRDTIE_ONLINE005_CHILD_OWNER") {
				t.Fatal("owned source DTO")
			}
			found := map[string]bool{}
			for _, item := range wire.Data.Items {
				found[item.Source.ID] = true
			}
			if !found[peer] || !found[activityID] {
				t.Fatal("original persisted online sources lost")
			}
		} else {
			var wire struct {
				Data struct {
					ID string `json:"id"`
				}
			}
			if json.Unmarshal(body, &wire) != nil || wire.Data.ID != activityID {
				t.Fatal("detail did not reopen same native ID")
			}
		}
		t.Logf("LOCAL_SYNTHETIC method=GET path=%s status=200 requestID=%s sameOriginalID=true noLocation=true", path, rid)
	}
}
