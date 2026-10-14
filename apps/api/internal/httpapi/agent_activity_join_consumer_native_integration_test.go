package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	aa "github.com/birdtie/birdtie/apps/api/internal/agentaction"
	"github.com/birdtie/birdtie/apps/api/internal/agentautonomy"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Like the original postgres ownedMigrationDatabase helper, this creates and
// migrates only this call's random child. The parent schema remains unchanged.
// No provider, model call, real account or runtime database is exercised.
func joinConsumerOwnedMigrationDatabase(t *testing.T) {
	t.Helper()
	dsn := os.Getenv("BIRDTIE_DATABASE_URL")
	if dsn == "" {
		t.Skip("original Now/JOIN HTTP requires owned native PostgreSQL")
	}
	if os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Fatal("owned JOIN consumer requires explicitly disposable parent")
	}
	uri, e := url.Parse(dsn)
	if e != nil || (uri.Scheme != "postgres" && uri.Scheme != "postgresql") {
		t.Fatal("owned PostgreSQL URI required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	parent, e := pgxpool.New(ctx, dsn)
	if e != nil {
		cancel()
		t.Fatal("owned parent connection")
	}
	var before string
	if e = parent.QueryRow(ctx, `SELECT coalesce(pg_get_functiondef(to_regprocedure('public.birdtie_guard_memory_reinforcement()')),'')`).Scan(&before); e != nil || before == "" {
		parent.Close()
		cancel()
		t.Fatal("parent guard required")
	}
	cfg, e := pgxpool.ParseConfig(dsn)
	if e != nil {
		parent.Close()
		cancel()
		t.Fatal("owned config")
	}
	cfg.ConnConfig.Database = "postgres"
	admin, e := pgxpool.NewWithConfig(ctx, cfg)
	if e != nil {
		parent.Close()
		cancel()
		t.Fatal("owned administrator")
	}
	var r [12]byte
	if _, e = rand.Read(r[:]); e != nil {
		admin.Close()
		parent.Close()
		cancel()
		t.Fatal("owned random ID")
	}
	name := "birdtie_owned_migration_" + hex.EncodeToString(r[:])
	quoted := pgx.Identifier{name}.Sanitize()
	if _, e = admin.Exec(ctx, "CREATE DATABASE "+quoted); e != nil {
		admin.Close()
		parent.Close()
		cancel()
		t.Fatal("owned child creation")
	}
	var owned *pgxpool.Pool
	t.Cleanup(func() {
		if owned != nil {
			owned.Close()
		}
		cleanup, stop := context.WithTimeout(context.Background(), 30*time.Second)
		defer stop()
		var after string
		if e := parent.QueryRow(cleanup, `SELECT coalesce(pg_get_functiondef(to_regprocedure('public.birdtie_guard_memory_reinforcement()')),'')`).Scan(&after); e != nil || after != before {
			t.Error("parent guard changed")
		}
		if _, e := admin.Exec(cleanup, "DROP DATABASE "+quoted); e != nil {
			t.Error("owned child cleanup")
		}
		admin.Close()
		parent.Close()
		cancel()
	})
	uri.Path = "/" + name
	uri.RawPath = ""
	ownedDSN := uri.String()
	owned, e = pgxpool.New(ctx, ownedDSN)
	if e != nil {
		t.Fatal("owned child connection")
	}
	files, e := filepath.Glob(filepath.Join("..", "..", "migrations", "*.sql"))
	if e != nil {
		t.Fatal(e)
	}
	sort.Strings(files)
	for _, path := range files {
		if strings.HasSuffix(path, ".down.sql") {
			continue
		}
		raw, e := os.ReadFile(path)
		if e != nil {
			t.Fatal(e)
		}
		if _, e = owned.Exec(ctx, string(raw)); e != nil {
			t.Fatal("owned actual migration", filepath.Base(path), e)
		}
		var seeds []string
		if strings.HasPrefix(filepath.Base(path), "029_") {
			seeds = []string{"001_badminton.sql", "002_functional_mvp.sql"}
		}
		if strings.HasPrefix(filepath.Base(path), "033_") {
			seeds = []string{"003_community_social.sql"}
		}
		for _, seed := range seeds {
			raw, e := os.ReadFile(filepath.Join("..", "..", "dev-seeds", seed))
			if e != nil {
				t.Fatal(e)
			}
			if _, e = owned.Exec(ctx, string(raw)); e != nil {
				t.Fatal("owned synthetic seed", seed, e)
			}
		}
	}
	t.Setenv("BIRDTIE_DATABASE_URL", ownedDSN)
	t.Log("independently owned child migrations applied; parent guard preserved")
}

type joinConsumerFixture struct {
	f       *contextBuilderHTTPFixture
	g       *postgres.AgentActivityJoin
	flags   *agentfeature.Controller
	handler http.Handler
}

func joinConsumerNative(t *testing.T) *joinConsumerFixture {
	t.Helper()
	joinConsumerOwnedMigrationDatabase(t)
	f := contextBuilderHTTPNative(t)
	flags := pipelineHTTPFlags(t, true)
	currentReadonlyNativePolicy(t, f.privateProfileHTTPDBFixture, 0, agentautonomy.LevelPrepare, time.Now().UTC().Add(time.Hour))
	g := postgres.NewAgentActivityJoin(f.store, flags, true)
	x := &joinConsumerFixture{f: f, g: g, flags: flags}
	x.handler = joinConsumerHTTPNew(f.store, g)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		tx, e := f.pool.Begin(ctx)
		if e != nil {
			t.Error("owned JOIN cleanup begin")
			return
		}
		defer tx.Rollback(context.Background())
		if _, e = tx.Exec(ctx, `ALTER TABLE agent_activity_join_effects DISABLE TRIGGER USER;`); e != nil {
			t.Error(e)
			return
		}
		if _, e = tx.Exec(ctx, `DELETE FROM agent_activity_join_effects WHERE owner_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Error(e)
			return
		}
		if _, e = tx.Exec(ctx, `ALTER TABLE agent_activity_join_effects ENABLE TRIGGER USER`); e != nil {
			t.Error(e)
			return
		}
		for _, q := range []string{`DELETE FROM agent_action_dispatches WHERE owner_id=ANY($1::uuid[])`, `DELETE FROM agent_action_approvals WHERE owner_id=ANY($1::uuid[]) AND action_kind='ACTIVITY_JOIN'`, `DELETE FROM consent_grants WHERE owner_account_id=ANY($1::uuid[]) AND purpose='OWN_ACTIVITY_JOIN_ACTION'`} {
			if _, e = tx.Exec(ctx, q, f.accountIDs); e != nil {
				t.Error(e)
				return
			}
		}
		if e = tx.Commit(ctx); e != nil {
			t.Error(e)
		}
	})
	return x
}
func joinConsumerHTTPNew(s *postgres.Store, g *postgres.AgentActivityJoin) http.Handler {
	return New(s, s, nil, s, s, s, s, s, s, s, s, nil, false, nil, nil, nil, WithAgentActivityJoin(g))
}
func (x *joinConsumerFixture) query(t *testing.T, task, query string) agentworkspace.Results {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"taskId": task, "query": query})
	return resultHTTPReply(t, x.f.request(t, x.handler, http.MethodPost, "/v1/cities/"+x.f.city+"/agent/tasks", string(body), x.f.tokens[0], 200, nil))
}
func (x *joinConsumerFixture) input(t *testing.T, r agentworkspace.Results, target string) aa.ActivityJoinInput {
	t.Helper()
	if r.Task == nil || r.Task.Status != agentworkspace.TaskCompleted {
		t.Fatal("original HTTP query did not complete")
	}
	last := len(r.Task.Conversation) - 1
	if agentworkspace.ValidateReplyMembership(*r.Task, last) != nil {
		t.Fatal("original last membership absent")
	}
	var op, action string
	if e := x.f.pool.QueryRow(x.f.ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&op, &action); e != nil {
		t.Fatal(e)
	}
	return aa.ActivityJoinInput{TaskID: r.Task.ID, LogicalOperationID: op, ActionID: action, ActivityID: target, ExpectedTaskUpdatedAt: r.Task.UpdatedAt.UTC().Format(time.RFC3339Nano), ReplyTurnDigest: r.Task.Conversation[last].ResultMembership.TurnDigest}
}
func (x *joinConsumerFixture) preview(t *testing.T, p aa.ActivityJoinInput) aa.ActivityJoinPreview {
	t.Helper()
	raw, _ := json.Marshal(p)
	w := x.f.request(t, x.handler, http.MethodPost, "/v1/me/agent-activity-join/previews", string(raw), x.f.tokens[0], 201, nil)
	var r struct {
		Data aa.ActivityJoinPreview `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || aa.ValidateActivityJoinPreview(r.Data, r.Data.Owner) != nil {
		t.Fatal("invalid original native review")
	}
	return r.Data
}
func joinConsumerApprovalPath(id, verb string) string {
	return "/v1/me/agent-activity-join/approvals/" + id + "/" + verb
}
func joinConsumerDigest(v aa.ActivityJoinPreview) string {
	raw, _ := json.Marshal(map[string]string{"bindingDigest": v.BindingDigest})
	return string(raw)
}
func joinConsumerReceipt(t *testing.T, w *httptest.ResponseRecorder) aa.ActivityJoinReceipt {
	t.Helper()
	var r struct {
		Data aa.ActivityJoinReceipt `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || aa.ValidateActivityJoinReceipt(r.Data, r.Data.Owner, r.Data.ApprovalID) != nil {
		t.Fatal("invalid causal receipt")
	}
	return r.Data
}
func joinConsumerCancelled(t *testing.T, w *httptest.ResponseRecorder, dispatch bool) aa.ActivityJoinPreview {
	t.Helper()
	var r struct {
		Data aa.ActivityJoinPreview `json:"data"`
	}
	if json.Unmarshal(w.Body.Bytes(), &r) != nil || r.Data.State != aa.Cancelled || r.Data.Review != nil || r.Data.DispatchCommitted != dispatch || aa.ValidateActivityJoinPreview(r.Data, r.Data.Owner) != nil {
		t.Fatal("revoke fabricated stop proof", dispatch)
	}
	return r.Data
}

func TestAgentActivityJoinConsumerNativeOriginalNowQueryUsesReviewedVersion(t *testing.T) {
	x := joinConsumerNative(t)
	f := x.f
	f.exec(`UPDATE activities SET price_minor=1234,currency='CNY',eligibility='请自备球拍；限成年人',capacity=12 WHERE id=$1`, f.public)
	result := x.query(t, "", "帮我找羽毛球活动")
	if result.Mode != "rules" {
		t.Fatal("original ordinary query unexpectedly used provider")
	}
	resultHTTPFind(t, result, "activity", f.public)
	p := x.input(t, result, f.public)
	v := x.preview(t, p)
	actual, e := f.store.GetActivity(f.ctx, f.public, f.accountIDs[0])
	if e != nil {
		t.Fatal(e)
	}
	if v.DispatchCommitted || v.Review.Title != actual.Title || v.Review.Summary != actual.Summary || v.Review.Description != actual.Description || !v.Review.StartsAt.Equal(actual.StartsAt) || !v.Review.EndsAt.Equal(actual.EndsAt) || v.Review.TimeZone != actual.TimeZone || v.Review.PriceMinor == nil || *v.Review.PriceMinor != 1234 || v.Review.Eligibility == nil || *v.Review.Eligibility != actual.Eligibility || v.Review.Capacity == nil || *v.Review.Capacity != 12 {
		t.Fatal("consumer did not receive exact bounded native facts")
	}
	w := f.request(t, x.handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "confirm"), joinConsumerDigest(v), f.tokens[0], 200, nil)
	var confirmed struct {
		Data aa.ActivityJoinPreview `json:"data"`
	}
	json.Unmarshal(w.Body.Bytes(), &confirmed)
	if confirmed.Data.State != aa.Approved || !reflect.DeepEqual(v.Review, confirmed.Data.Review) || confirmed.Data.BindingDigest != v.BindingDigest {
		t.Fatal("confirm silently changed reviewed version")
	}
	receipt := joinConsumerReceipt(t, f.request(t, x.handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "execute"), joinConsumerDigest(v), f.tokens[0], 200, nil))
	if receipt.Status != aa.Succeeded || receipt.ParticipationID == nil {
		t.Fatal("original domain JOIN did not execute")
	}
	var count, human int
	if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM agent_activity_join_effects WHERE approval_id=$1 AND participation_id=$2),(SELECT count(*) FROM human_participation_operation_receipts WHERE owner_account_id=$3)`, v.ApprovalID, *receipt.ParticipationID, f.accountIDs[0]).Scan(&count, &human); e != nil || count != 1 || human != 0 {
		t.Fatal("original causal effect mixed with Human receipt", e)
	}
	reopened := postgres.New(f.pool, false)
	off := postgres.NewAgentActivityJoin(reopened, x.flags, false)
	handler := joinConsumerHTTPNew(reopened, off)
	recovered := joinConsumerReceipt(t, f.request(t, handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "reconcile"), "", f.tokens[0], 200, nil))
	if !reflect.DeepEqual(receipt, recovered) {
		t.Fatal("new process reconstructed another effect")
	}
	joinConsumerCancelled(t, f.request(t, handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "revoke"), "", f.tokens[0], 200, nil), true)
	after := joinConsumerReceipt(t, f.request(t, handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "reconcile"), "", f.tokens[0], 200, nil))
	if after.Status != aa.Succeeded || after.EffectID == nil || *after.EffectID != *receipt.EffectID {
		t.Fatal("cancelled approval erased earlier effect")
	}
}

type joinConsumerLostWriter struct {
	header http.Header
	status int
}

func (w *joinConsumerLostWriter) Header() http.Header     { return w.header }
func (w *joinConsumerLostWriter) WriteHeader(n int)       { w.status = n }
func (*joinConsumerLostWriter) Write([]byte) (int, error) { return 0, io.ErrClosedPipe }
func TestAgentActivityJoinConsumerNativeLatestSourceABAAndPrecommitStop(t *testing.T) {
	x := joinConsumerNative(t)
	f := x.f
	first := x.query(t, "", "帮我找羽毛球活动")
	p := x.input(t, first, f.public)
	v := x.preview(t, p)
	lost := &joinConsumerLostWriter{header: http.Header{}}
	request := privateProfileHTTPRequest(http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "confirm"), joinConsumerDigest(v), f.tokens[0], "application/json")
	x.handler.ServeHTTP(lost, request)
	if lost.status != 200 {
		t.Fatal("real confirm did not reach lost response")
	}
	f.request(t, x.handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "reconcile"), "", f.tokens[0], 503, nil)
	second := x.query(t, first.Task.ID, "帮我找羽毛球活动")
	if second.Task.ID != first.Task.ID || second.Task.Conversation[len(second.Task.Conversation)-1].ResultMembership.TurnDigest == p.ReplyTurnDigest {
		t.Fatal("original Now follow-up did not make a new round")
	}
	old, _ := json.Marshal(p)
	f.request(t, x.handler, http.MethodPost, "/v1/me/agent-activity-join/previews", string(old), f.tokens[0], 409, nil)
	f.exec(`UPDATE activities SET publication_status='draft' WHERE id=$1`, f.public)
	x.g.Disable()
	x.flags.Disable(agentfeature.Enrichment)
	stopped := joinConsumerCancelled(t, f.request(t, x.handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "revoke"), "", f.tokens[0], 200, nil), false)
	if stopped.ApprovalID != v.ApprovalID || stopped.BindingDigest != v.BindingDigest {
		t.Fatal("stop proof changed original approval")
	}
	f.request(t, x.handler, http.MethodPost, joinConsumerApprovalPath(v.ApprovalID, "execute"), joinConsumerDigest(v), f.tokens[0], 503, nil)
	var effects, dispatch int
	if e := f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM agent_activity_join_effects WHERE approval_id=$1),(SELECT count(*) FROM agent_action_dispatches WHERE approval_id=$1)`, v.ApprovalID).Scan(&effects, &dispatch); e != nil || effects != 0 || dispatch != 0 {
		t.Fatal("late consumer effect after stopped precommit", e)
	}
}

func TestAgentActivityJoinConsumerNativeMissingForeignAndRetiredSourcesDenied(t *testing.T) {
	for _, kind := range []string{"missing-membership", "foreign-member-city", "foreign-task", "latest-place", "unlisted-target", "private-target", "active-task", "task-generation-aba", "review-change"} {
		t.Run(kind, func(t *testing.T) {
			x := joinConsumerNative(t)
			f := x.f
			r := x.query(t, "", "帮我找羽毛球活动")
			p := x.input(t, r, f.public)
			var v aa.ActivityJoinPreview
			if kind == "task-generation-aba" || kind == "review-change" {
				v = x.preview(t, p)
			}
			switch kind {
			case "missing-membership":
				f.exec(`UPDATE agent_tasks SET conversation=jsonb_set(conversation,ARRAY[(jsonb_array_length(conversation)-1)::text,'resultMembership'],'null'::jsonb) WHERE id=$1`, p.TaskID)
			case "foreign-member-city":
				f.exec(`UPDATE agent_tasks SET conversation=jsonb_set(conversation,ARRAY[(jsonb_array_length(conversation)-1)::text,'resultMembership','cityId'],'"foreign-city"'::jsonb) WHERE id=$1`, p.TaskID)
			case "foreign-task":
				raw, _ := json.Marshal(p)
				f.request(t, x.handler, http.MethodPost, "/v1/me/agent-activity-join/previews", string(raw), f.tokens[1], 403, nil)
				return
			case "latest-place":
				x.query(t, r.Task.ID, "找地点")
			case "unlisted-target":
				var id string
				if e := f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&id); e != nil {
					t.Fatal(e)
				}
				p.ActivityID = id
			case "private-target":
				p.ActivityID = f.private
			case "active-task":
				f.exec(`UPDATE agent_tasks SET status='ACTIVE' WHERE id=$1`, p.TaskID)
			case "task-generation-aba":
				f.exec(`UPDATE agent_tasks SET status='FAILED' WHERE id=$1`, p.TaskID)
				f.exec(`UPDATE agent_tasks SET status='COMPLETED' WHERE id=$1`, p.TaskID)
			case "review-change":
				f.exec(`UPDATE activities SET description=description||' 修改报名规则' WHERE id=$1`, f.public)
			}
			path, body := "/v1/me/agent-activity-join/previews", ""
			if v.ApprovalID != "" {
				path = joinConsumerApprovalPath(v.ApprovalID, "confirm")
				body = joinConsumerDigest(v)
			} else {
				raw, _ := json.Marshal(p)
				body = string(raw)
			}
			w := httptest.NewRecorder()
			request := privateProfileHTTPRequest(http.MethodPost, path, body, f.tokens[0], "application/json")
			x.handler.ServeHTTP(w, request)
			if w.Code != 403 && w.Code != 409 {
				t.Fatalf("obsolete/missing native source permitted %s status=%d", kind, w.Code)
			}
			var n int
			if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agent_activity_join_effects WHERE owner_id=$1`, f.accountIDs[0]).Scan(&n); e != nil || n != 0 {
				t.Fatal("denied source changed domain", e)
			}
		})
	}
}
