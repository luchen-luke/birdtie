package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestMomentContextHTTPIntegration(t *testing.T) {
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("requires disposable migration database")
	}
	v4PrivacyHTTPDatabase(t)
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("MOMENT_CONTEXT owned HTTP database %s", pool.Config().ConnConfig.Database)
	defer pool.Close()
	store := postgres.New(pool, false)
	s := &server{access: store, content: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/me/moments", s.createMomentDraft)
	mux.HandleFunc("GET /v1/me/moments/{momentID}", s.getOwnMoment)
	mux.HandleFunc("PUT /v1/me/moments/{momentID}", s.updateMomentDraft)
	people := []string{
		"b1700000-0000-4000-8000-000000000010",
		"b1700000-0000-4000-8000-000000000011",
		"b1700000-0000-4000-8000-000000000012",
	}
	tokens := make([]string, len(people))
	for i, account := range people {
		value, digest, e := identity.NewToken()
		if e != nil {
			t.Fatal(e)
		}
		tokens[i] = value
		if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at)
            VALUES($1,$2,'test',now()+interval '1 hour',now()+interval '30 minutes')`, account, digest[:]); e != nil {
			t.Fatal(e)
		}
		defer pool.Exec(ctx, `DELETE FROM sessions WHERE token_sha256=$1`, digest[:])
	}
	call := func(method, path string, who int, body string) (int, map[string]any) {
		req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		if who >= 0 {
			req.Header.Set("Authorization", "Bearer "+tokens[who])
		}
		if body != "" {
			req.Header.Set("Content-Type", "application/json")
		}
		rw := httptest.NewRecorder()
		mux.ServeHTTP(rw, req)
		var payload map[string]any
		_ = json.Unmarshal(rw.Body.Bytes(), &payload)
		return rw.Code, payload
	}
	const create = `{"cityId":"aberdeen-gb","placeId":"b1700000-0000-4000-8000-000000000015","activityId":"b1700000-0000-4000-8000-000000000016","communityId":"b1700000-0000-4000-8000-000000000030","organizationId":"b1700000-0000-4000-8000-000000000013","title":"Private context","body":"local test","timePrecision":"unknown","locationPrecision":"place"}`
	if code, _ := call("POST", "/v1/me/moments", -1, create); code != 401 {
		t.Fatalf("anonymous create: %d", code)
	}
	if code, _ := call("POST", "/v1/me/moments", 2, create); code != 403 {
		t.Fatalf("Organization account created personal Moment: %d", code)
	}
	code, payload := call("POST", "/v1/me/moments", 0, create)
	if code != 201 {
		t.Fatalf("create: %d %+v", code, payload)
	}
	record := payload["data"].(map[string]any)
	id := record["id"].(string)
	defer func() {
		for _, q := range []string{
			`DELETE FROM agent_domain_outbox WHERE source_type='MOMENT' AND source_id=$1`,
			`DELETE FROM audit_events WHERE resource_type='moment' AND resource_id=$1`,
			`DELETE FROM moments WHERE id=$1`,
		} {
			if _, err := pool.Exec(ctx, q, id); err != nil {
				t.Errorf("owned HTTP Moment context cleanup: %v", err)
			}
		}
	}()
	if record["visibility"] != "private" || record["status"] != "draft" ||
		record["communityId"] != "b1700000-0000-4000-8000-000000000030" {
		t.Fatalf("private linked response: %+v", record)
	}
	if code, _ := call("GET", "/v1/me/moments/"+id, 1, ""); code != 404 {
		t.Fatalf("other person read: %d", code)
	}
	if code, _ := call("GET", "/v1/me/moments/"+id, 0, ""); code != 200 {
		t.Fatalf("author read: %d", code)
	}
	code, _ = call("PUT", "/v1/me/moments/"+id, 0,
		`{"cityId":"aberdeen-gb","placeId":"b1700000-0000-4000-8000-000000000015","activityId":"11111111-1111-4111-8111-111111111111","title":"Bad link","body":"","timePrecision":"unknown","locationPrecision":"place","revision":1}`)
	if code != 409 {
		t.Fatalf("invalid Activity relation: %d", code)
	}
}
