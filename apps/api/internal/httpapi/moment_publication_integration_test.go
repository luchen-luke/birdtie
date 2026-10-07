package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type publicationHTTPNativeFixture struct {
	base                        *privateProfileHTTPDBFixture
	city, place, account, token string
}

func publicationHTTPNative(t *testing.T) *publicationHTTPNativeFixture {
	t.Helper()
	b := historicalHTTPFixture(t)
	f := &publicationHTTPNativeFixture{base: b}
	if e := b.pool.QueryRow(b.ctx, `SELECT gen_random_uuid()::text`).Scan(&f.account); e != nil {
		t.Fatal(e)
	}
	f.city = "public-mom-" + strings.ReplaceAll(f.account, "-", "")
	t.Cleanup(func() {
		for _, q := range []string{`DELETE FROM moments WHERE city_id=$1`, `DELETE FROM places WHERE city_id=$1`, `DELETE FROM cities WHERE id=$1`} {
			if _, e := b.pool.Exec(context.Background(), q, f.city); e != nil {
				t.Error("owned public history HTTP cleanup", e)
			}
		}
	})
	b.exec(`INSERT INTO cities(id,name,region,country_code,time_zone,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES($1,'合成公开Moment城市','LOCAL','GB','Europe/London','published','LOCAL_SYNTHETIC_FIXTURE','local:MOM002','合成维护者',$2)`, f.city, b.accountIDs[1])
	if e := b.pool.QueryRow(b.ctx, `INSERT INTO places(id,city_id,name,category_code,summary,publication_status,source_label,source_ref,maintainer_label,maintainer_account_id) VALUES(gen_random_uuid(),$1,'合成公开Moment地点','sports','不能复制到公开历史的描述','published','LOCAL_SYNTHETIC_FIXTURE','local:MOM002','合成维护者',$2) RETURNING id`, f.city, b.accountIDs[1]).Scan(&f.place); e != nil {
		t.Fatal(e)
	}
	b.exec(`INSERT INTO accounts(id,account_type,status) VALUES($1,'person','active')`, f.account)
	b.accountIDs = append(b.accountIDs, f.account)
	b.agentIDs = append(b.agentIDs, "")
	f.token = b.newSession(f.account, false, false)
	b.tokens = append(b.tokens, f.token)
	return f
}
func publicationHTTPNativeDraft(t *testing.T, f *publicationHTTPNativeFixture) content.Moment {
	t.Helper()
	body, _ := json.Marshal(map[string]any{"cityId": f.city, "placeId": f.place, "title": "本人确认的公开合成分享", "body": "只有本人检查后公开的正文", "occurredAt": "1999-01-01T00:00:00Z", "timePrecision": "day", "locationPrecision": "place"})
	return historicalHTTPRecord(t, historicalHTTPCall(f.base, "POST", "/v1/me/moments", string(body), f.token), 201)
}
func publicationHTTPNativePreview(t *testing.T, f *publicationHTTPNativeFixture, id string) content.MomentPublicationPreview {
	t.Helper()
	w := historicalHTTPCall(f.base, "GET", "/v1/me/moments/"+id+"/publication", "", f.token)
	if w.Code != 200 {
		t.Fatal("actual registered preview", w.Code, w.Body.String())
	}
	var response struct {
		Data content.MomentPublicationPreview
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil {
		t.Fatal(e)
	}
	return response.Data
}
func publicationHTTPNativeBody(p content.MomentPublicationPreview) string {
	raw, _ := json.Marshal(content.MomentPublicationInput{Revision: p.Revision, Snapshot: p.Snapshot, ConfirmPublic: true})
	return string(raw)
}
func TestMomentPublicationHTTPNativeRegisteredLifecycle(t *testing.T) {
	f := publicationHTTPNative(t)
	m := publicationHTTPNativeDraft(t, f)
	p := publicationHTTPNativePreview(t, f, m.ID)
	var agents int
	if e := f.base.pool.QueryRow(f.base.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.account).Scan(&agents); e != nil || agents != 0 {
		t.Fatal("ordinary human unexpectedly has Agent", e)
	}
	path := "/v1/me/moments/" + m.ID + "/publication"
	w := historicalHTTPCall(f.base, "POST", path, publicationHTTPNativeBody(p), f.token)
	if w.Code != 200 {
		t.Fatal("actual registered confirmed publication", w.Code, w.Body.String())
	}
	var response struct {
		Data content.MomentPublicationReceipt
	}
	if e := json.Unmarshal(w.Body.Bytes(), &response); e != nil || response.Data.Revision != m.Revision+1 {
		t.Fatal(e)
	}
	history := historicalHTTPCall(f.base, "GET", "/v1/places/"+f.place+"/social-history", "", "")
	if history.Code != 200 || !strings.Contains(history.Body.String(), m.Title) || strings.Contains(history.Body.String(), "occurredAt") || strings.Contains(history.Body.String(), f.account) {
		t.Fatal("registered current public summary", history.Code, history.Body.String())
	}
	if w = historicalHTTPCall(f.base, "POST", path, publicationHTTPNativeBody(p), f.token); w.Code != 409 {
		t.Fatal("repeated old approval", w.Code)
	}
	if w = historicalHTTPCall(f.base, "DELETE", "/v1/me/moments/"+m.ID+fmt.Sprintf("?revision=%d", response.Data.Revision), "", f.token); w.Code != 204 {
		t.Fatal("actual registered withdraw", w.Code, w.Body.String())
	}
	history = historicalHTTPCall(f.base, "GET", "/v1/places/"+f.place+"/social-history", "", "")
	if history.Code != 200 || strings.Contains(history.Body.String(), m.Title) {
		t.Fatal("registered withdrawn still public", history.Code)
	}
}
func TestMomentPublicationHTTPNativeAuthorOnly(t *testing.T) {
	f := publicationHTTPNative(t)
	m := publicationHTTPNativeDraft(t, f)
	for _, tc := range []struct {
		token string
		want  int
	}{{"", 401}, {f.base.tokens[1], 404}, {f.base.tokens[2], 403}, {f.base.tokens[3], 403}} {
		w := historicalHTTPCall(f.base, "GET", "/v1/me/moments/"+m.ID+"/publication", "", tc.token)
		if w.Code != tc.want || strings.Contains(w.Body.String(), m.Body) {
			t.Fatal("native author boundary", w.Code, tc.want)
		}
	}
}
func TestMomentPublicationHTTPNativeLateSession(t *testing.T) {
	for _, mode := range []string{"revoke", "expiry"} {
		t.Run(mode, func(t *testing.T) {
			f := publicationHTTPNative(t)
			m := publicationHTTPNativeDraft(t, f)
			p := publicationHTTPNativePreview(t, f, m.ID)
			digest, _ := identity.ParseBearer("Bearer " + f.token)
			if mode == "expiry" {
				f.base.exec(`UPDATE sessions SET expires_at=clock_timestamp()+interval '1500 milliseconds',idle_expires_at=clock_timestamp()+interval '1400 milliseconds' WHERE token_sha256=$1`, digest[:])
			}
			blocker, e := f.base.pool.Begin(f.base.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer blocker.Rollback(context.Background())
			var pid int
			if e = blocker.QueryRow(f.base.ctx, `SELECT pg_backend_pid() FROM moments WHERE id=$1 FOR UPDATE`, m.ID).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			done := make(chan *httptest.ResponseRecorder, 1)
			go func() {
				done <- historicalHTTPCall(f.base, "POST", "/v1/me/moments/"+m.ID+"/publication", publicationHTTPNativeBody(p), f.token)
			}()
			publicationHTTPWaitSource(t, f, pid, "FROM moments")
			if mode == "revoke" {
				if e = f.base.store.RevokeSession(f.base.ctx, digest); e != nil {
					t.Fatal(e)
				}
			} else {
				publicationHTTPWaitExpiry(t, f, `SELECT expires_at<=clock_timestamp() FROM sessions WHERE token_sha256=$1`, digest[:])
			}
			if e = blocker.Commit(f.base.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case w := <-done:
				if w.Code != 401 {
					t.Fatal("initial auth reused after source wait", w.Code, w.Body.String())
				}
			case <-time.After(5 * time.Second):
				t.Fatal("registered request stuck")
			}
			var status string
			if e = f.base.pool.QueryRow(f.base.ctx, `SELECT status FROM moments WHERE id=$1`, m.ID).Scan(&status); e != nil || status != "draft" {
				t.Fatal("late write changed source", e)
			}
		})
	}
}
func publicationHTTPWaitSource(t *testing.T, f *publicationHTTPNativeFixture, pid int, needle string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var blocked bool
		if e := f.base.pool.QueryRow(f.base.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1=ANY(pg_blocking_pids(pid)) AND query LIKE $2)`, pid, "%"+needle+"%").Scan(&blocked); e != nil {
			t.Fatal(e)
		}
		if blocked {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("registered native wait not observed")
}
func publicationHTTPWaitExpiry(t *testing.T, f *publicationHTTPNativeFixture, q string, param any) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var expired bool
		if e := f.base.pool.QueryRow(f.base.ctx, q, param).Scan(&expired); e != nil {
			t.Fatal(e)
		}
		if expired {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("native PG expiry not reached")
}
