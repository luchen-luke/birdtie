package httpapi

import (
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/organizationannouncement"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type announcementHTTPAfter struct {
	*postgres.Store
	afterManaged, afterPublic func()
}

func (s *announcementHTTPAfter) ReadOrganizationAnnouncement(ctx context.Context, a agentorganizationmemory.Access, id string) (organizationannouncement.Record, error) {
	r, e := s.Store.ReadOrganizationAnnouncement(ctx, a, id)
	if e == nil && s.afterManaged != nil {
		hook := s.afterManaged
		s.afterManaged = nil
		hook()
	}
	return r, e
}
func (s *announcementHTTPAfter) ReadPublicOrganizationAnnouncement(ctx context.Context, org, id string, a organizationannouncement.PublicAccess) (organizationannouncement.PublicRecord, error) {
	r, e := s.Store.ReadPublicOrganizationAnnouncement(ctx, org, id, a)
	if e == nil && s.afterPublic != nil {
		hook := s.afterPublic
		s.afterPublic = nil
		hook()
	}
	return r, e
}

func TestOrganizationAnnouncementRegisteredHTTPNativeLateResponse(t *testing.T) {
	for _, name := range []string{"managed_role", "managed_session", "managed_withdraw", "public_withdraw", "public_context", "public_session"} {
		t.Run(name, func(t *testing.T) {
			f, org := orgMemoryHTTPFixture(t)
			f.exec(`UPDATE organizations SET visibility='public',verification_status='verified',updated_at=clock_timestamp() WHERE id=$1`, org)
			id := orgMemoryHTTPID(t, f)
			detail := "/v1/me/organizations/" + org + "/announcements/" + id
			body := announcementHTTPBody(t, organizationannouncement.DraftInput{Title: "合成迟到回复", Body: "私密许可上下文不应迟到释放", ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)})
			f.request(t, f.handler, "PUT", detail, body, f.tokens[0], 200, nil)
			w := f.request(t, f.handler, "POST", detail+"/publication-preview", `{"expectedRevision":1}`, f.tokens[0], 200, nil)
			var p struct {
				Data organizationannouncement.Preview `json:"data"`
			}
			json.Unmarshal(w.Body.Bytes(), &p)
			f.request(t, f.handler, "POST", detail+"/publish", announcementHTTPBody(t, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.Data.ID}), f.tokens[0], 200, nil)
			hook := func() {
				switch name {
				case "managed_role":
					f.exec(`UPDATE organization_memberships SET role='member',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[0])
				case "managed_session", "public_session":
					f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[0])
				case "managed_withdraw", "public_withdraw":
					f.request(t, f.handler, "POST", detail+"/withdraw", `{"expectedRevision":2}`, f.tokens[0], 200, nil)
				case "public_context":
					f.exec(`UPDATE organizations SET name=name||' changed',updated_at=clock_timestamp() WHERE id=$1`, org)
				}
			}
			store := &announcementHTTPAfter{Store: f.store}
			path := detail
			want := 403
			if strings.HasPrefix(name, "public_") {
				store.afterPublic = hook
				path = "/v1/organizations/" + org + "/announcements/" + id
				want = 404
				if name == "public_session" {
					want = 401
				}
			} else {
				store.afterManaged = hook
				if name == "managed_withdraw" {
					want = 409
				}
			}
			w = f.request(t, privateProfileHTTPNew(store, f.store), "GET", path, "", f.tokens[0], want, nil)
			if strings.Contains(w.Body.String(), "私密许可上下文") {
				t.Fatal("late read released invalid body")
			}
		})
	}
}

// Root's real registered handler authenticates twice. This adapter only holds
// the *final payload reader's* connection after the first payload SELECT; the
// second Authenticate runs on the independent native access pool. The final
// Store SELECT must therefore handle changes occurring after authentication.
type announcementHTTPPoolWait struct {
	*postgres.Store
	pool         *pgxpool.Pool
	held         *pgxpool.Conn
	calls        int
	finalStarted chan struct{}
}

func (s *announcementHTTPPoolWait) ReadPublicOrganizationAnnouncement(ctx context.Context, org, id string, a organizationannouncement.PublicAccess) (organizationannouncement.PublicRecord, error) {
	s.calls++
	if s.calls == 2 {
		close(s.finalStarted)
	}
	r, e := s.Store.ReadPublicOrganizationAnnouncement(ctx, org, id, a)
	if e == nil && s.calls == 1 {
		s.held, e = s.pool.Acquire(ctx)
	}
	return r, e
}
func TestOrganizationAnnouncementRegisteredHTTPNativeFinalPoolWaitSession(t *testing.T) {
	for _, name := range []string{"revoke", "absolute_expiry", "idle_expiry", "session_aba", "account_aba", "healthy"} {
		t.Run(name, func(t *testing.T) {
			f, org := orgMemoryHTTPFixture(t)
			f.exec(`UPDATE organizations SET visibility='public',verification_status='verified',updated_at=clock_timestamp() WHERE id=$1`, org)
			id := orgMemoryHTTPID(t, f)
			detail := "/v1/me/organizations/" + org + "/announcements/" + id
			input := organizationannouncement.DraftInput{Title: "合成最终等待边界", Body: "会话撤销后不得释放这一段内容", ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
			f.request(t, f.handler, "PUT", detail, announcementHTTPBody(t, input), f.tokens[0], 200, nil)
			w := f.request(t, f.handler, "POST", detail+"/publication-preview", `{"expectedRevision":1}`, f.tokens[0], 200, nil)
			var p struct {
				Data organizationannouncement.Preview `json:"data"`
			}
			json.Unmarshal(w.Body.Bytes(), &p)
			f.request(t, f.handler, "POST", detail+"/publish", announcementHTTPBody(t, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.Data.ID}), f.tokens[0], 200, nil)
			var deadline time.Time
			if name == "absolute_expiry" {
				if e := f.pool.QueryRow(f.ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',expires_at=stamp.at+interval '900 milliseconds',idle_expires_at=stamp.at+interval '900 milliseconds' FROM stamp WHERE account_id=$1 RETURNING expires_at`, f.accountIDs[1]).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			cfg := f.pool.Config()
			cfg.MaxConns = 1
			cfg.MinConns = 0
			cfg.ConnConfig.RuntimeParams["timezone"] = "Pacific/Honolulu"
			pool, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			wait := &announcementHTTPPoolWait{Store: postgres.New(pool, false), pool: pool, finalStarted: make(chan struct{})}
			defer func() {
				if wait.held != nil {
					wait.held.Release()
				}
			}()
			handler := privateProfileHTTPNew(wait, f.store)
			rec := httptest.NewRecorder()
			req := httptest.NewRequest("GET", "/v1/organizations/"+org+"/announcements/"+id, nil)
			req.Header.Set("Authorization", "Bearer "+f.tokens[1])
			done := make(chan struct{})
			go func() { handler.ServeHTTP(rec, req); close(done) }()
			select {
			case <-wait.finalStarted:
			case <-time.After(5 * time.Second):
				t.Fatal("final SELECT not reached after actual authentication")
			}
			if wait.held == nil || pool.Stat().AcquiredConns() != 1 {
				t.Fatal("reader did not own the only available connection")
			}
			select {
			case <-done:
				t.Fatal("payload escaped while final native reader pool held")
			case <-time.After(50 * time.Millisecond):
			}
			switch name {
			case "revoke":
				f.exec(`UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.accountIDs[1])
			case "session_aba":
				f.exec(`UPDATE sessions SET id=gen_random_uuid(),created_at=created_at+interval '1 microsecond' WHERE account_id=$1`, f.accountIDs[1])
			case "account_aba":
				f.exec(`UPDATE accounts SET updated_at=updated_at+interval '1 microsecond' WHERE id=$1`, f.accountIDs[1])
			case "idle_expiry":
				if e := f.pool.QueryRow(f.ctx, `UPDATE sessions SET idle_expires_at=clock_timestamp()+interval '100 milliseconds' WHERE account_id=$1 RETURNING idle_expires_at`, f.accountIDs[1]).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
			}
			if !deadline.IsZero() {
				for {
					var now time.Time
					if e := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
						t.Fatal(e)
					}
					if !now.Before(deadline) {
						break
					}
					time.Sleep(5 * time.Millisecond)
				}
			}
			wait.held.Release()
			wait.held = nil
			select {
			case <-done:
			case <-time.After(5 * time.Second):
				t.Fatal("final reader never finished")
			}
			want := 404
			if name == "healthy" {
				want = 200
			}
			if rec.Code != want {
				t.Fatal("post-auth native pool boundary", name, rec.Code, rec.Body.String())
			}
			if want != 200 && strings.Contains(rec.Body.String(), input.Body) {
				t.Fatal("post-auth revoked payload leak")
			}
			t.Log("LOCAL_DISPOSABLE_SYNTHETIC_ONLY final payload pool max1 held after second native Authenticate; session/current-clock boundary enforced", name, rec.Code)
		})
	}
}

func announcementHTTPBody(t *testing.T, value any) string {
	t.Helper()
	raw, e := json.Marshal(value)
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestOrganizationAnnouncementRegisteredHTTPNativeLifecycleAndAuth(t *testing.T) {
	f, org := orgMemoryHTTPFixture(t)
	f.exec(`UPDATE organizations SET visibility='public',verification_status='verified',updated_at=clock_timestamp() WHERE id=$1`, org)
	id := orgMemoryHTTPID(t, f)
	base := "/v1/me/organizations/" + org + "/announcements"
	detail := base + "/" + id
	public := "/v1/organizations/" + org + "/announcements/" + id
	body := announcementHTTPBody(t, organizationannouncement.DraftInput{Title: "合成公告", Body: "仅测试组织管理员发布，不是真实通知或 CSSA 授权。", ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)})
	for _, token := range []string{"", "unknown", f.newSession(f.accountIDs[0], true, false), f.newSession(f.accountIDs[0], false, true)} {
		f.request(t, f.handler, "PUT", detail, body, token, 401, nil)
	}
	for _, token := range []string{f.tokens[2], f.tokens[3]} {
		f.request(t, f.handler, "PUT", detail, body, token, 403, nil)
	}
	f.request(t, f.handler, "PUT", detail, body, f.tokens[0], 200, nil)
	f.request(t, f.handler, "GET", base, "", f.tokens[1], 200, nil)
	f.request(t, f.handler, "GET", detail, "", f.tokens[1], 200, nil)
	f.request(t, f.handler, "GET", public, "", "", 404, nil)
	preview := f.request(t, f.handler, "POST", detail+"/publication-preview", `{"expectedRevision":1}`, f.tokens[0], 200, nil)
	var p struct {
		Data organizationannouncement.Preview `json:"data"`
	}
	if e := json.Unmarshal(preview.Body.Bytes(), &p); e != nil || p.Data.ID == "" || p.Data.Announcement.Revision != 1 {
		t.Fatal(e)
	}
	publish := announcementHTTPBody(t, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.Data.ID})
	f.request(t, f.handler, "POST", detail+"/publish", publish, f.tokens[1], 409, nil)
	f.request(t, f.handler, "POST", detail+"/publish", publish, f.tokens[0], 200, nil)
	f.request(t, f.handler, "POST", detail+"/publish", publish, f.tokens[0], 200, nil)
	w := f.request(t, f.handler, "GET", public, "", "", 200, nil)
	if w.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("public cached")
	}
	for _, key := range []string{"createdBy", "updatedBy", "organizationAccountId", "publicationContext", "session", "previewId", "audit"} {
		if strings.Contains(w.Body.String(), `"`+key+`"`) {
			t.Fatal("private field public", key)
		}
	}
	f.request(t, f.handler, "GET", public, "", "invalid", 401, nil)
	f.request(t, f.handler, "GET", public+"?confirmed=true", "", "", 400, nil)
	f.request(t, f.handler, "POST", detail+"/withdraw", `{"expectedRevision":2}`, f.tokens[1], 200, nil)
	f.request(t, f.handler, "GET", public, "", "", 404, nil)
	f.request(t, f.handler, "PUT", detail, strings.Replace(body, `"expectedRevision":0`, `"expectedRevision":3`, 1), f.tokens[0], 409, nil)
}
func TestOrganizationAnnouncementRegisteredHTTPNativeClosedInputAndRoleChange(t *testing.T) {
	f, org := orgMemoryHTTPFixture(t)
	f.exec(`UPDATE organizations SET visibility='public',verification_status='verified',updated_at=clock_timestamp() WHERE id=$1`, org)
	id := orgMemoryHTTPID(t, f)
	path := "/v1/me/organizations/" + org + "/announcements/" + id
	input := organizationannouncement.DraftInput{Title: "合成预览", Body: "合成声明", ValidUntil: time.Now().Add(time.Hour).UTC().Truncate(time.Microsecond)}
	body := announcementHTTPBody(t, input)
	f.request(t, f.handler, "PUT", path, body, f.tokens[0], 200, nil)
	for _, bad := range []string{`{"expectedRevision":1,"confirmed":true}`, `{"expectedRevision":1,"expectedRevision":1}`, `{"expectedRevision":1,"modelPermission":true}`, `{"expectedRevision":1,"organizationId":"` + org + `"}`, `{"expectedRevision":null}`} {
		f.request(t, f.handler, "POST", path+"/publication-preview", bad, f.tokens[0], 400, nil)
	}
	w := f.request(t, f.handler, "POST", path+"/publication-preview", `{"expectedRevision":1}`, f.tokens[1], 200, nil)
	var p struct {
		Data organizationannouncement.Preview `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &p)
	f.exec(`UPDATE organization_memberships SET role='member',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	publish := announcementHTTPBody(t, organizationannouncement.PublishInput{ExpectedRevision: 1, PreviewID: p.Data.ID})
	f.request(t, f.handler, "POST", path+"/publish", publish, f.tokens[1], 403, nil)
	f.exec(`UPDATE organization_memberships SET role='admin',updated_at=clock_timestamp() WHERE organization_id=$1 AND user_account_id=$2`, org, f.accountIDs[1])
	f.request(t, f.handler, "POST", path+"/publish", publish, f.tokens[1], 409, nil)
	f.request(t, f.handler, "GET", path+"?workspace=other", "", f.tokens[0], 400, nil)
	f.request(t, f.handler, "GET", path, "{}", f.tokens[0], 400, nil)
}
