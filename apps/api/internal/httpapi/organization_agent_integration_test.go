package httpapi

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
)

// Delegates real native public knowledge. Hook occurs after that actual source
// read to exercise the registered handler's final real-session authentication.
type organizationAgentNativeReplyBarrier struct {
	*postgres.Store
	after func()
}

func TestOrganizationCapabilityRegisteredHTTPNativeFinalWaitExpiry(t *testing.T) {
	for _, phase := range []string{"initial", "final"} {
		t.Run(phase, func(t *testing.T) {
			for _, expiry := range []string{"absolute", "idle"} {
				t.Run(expiry, func(t *testing.T) {
					f := privateProfileHTTPDBNew(t)
					var orgID string
					if e := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&orgID); e != nil {
						t.Fatal(e)
					}
					f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner')`, orgID, f.accountIDs[0])
					f.exec(`UPDATE organizations SET verification_status='verified' WHERE id=$1`, orgID)
					t.Cleanup(func() {
						if _, e := f.pool.Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
							t.Errorf("owned late wait cleanup: %v", e)
						}
					})
					faq, e := f.store.CreateFAQ(f.ctx, f.accountIDs[0], orgID, organization.FAQInput{Question: "如何报名", Answer: "锁等待期满不得释放此合成FAQ", Published: true})
					if e != nil {
						t.Fatal(e)
					}
					token := f.newSession(f.accountIDs[0], false, false)
					digest, e := identity.ParseBearer("Bearer " + token)
					if e != nil {
						t.Fatal(e)
					}
					type proof struct {
						waiting, expired bool
						err              error
					}
					completed := make(chan proof, 1)
					barrier := &organizationAgentNativeReplyBarrier{Store: f.store, after: func() {
						var deadline time.Time
						sql := `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '400 milliseconds',expires_at=stamp.at+interval '1 hour' FROM stamp WHERE token_sha256=$1 RETURNING idle_expires_at`
						if expiry == "absolute" {
							sql = `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() AS at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '400 milliseconds',expires_at=stamp.at+interval '400 milliseconds' FROM stamp WHERE token_sha256=$1 RETURNING expires_at`
						}
						if e := f.pool.QueryRow(f.ctx, sql, digest[:]).Scan(&deadline); e != nil {
							t.Fatal(e)
						}
						lock, e := f.pool.Begin(f.ctx)
						if e != nil {
							t.Fatal(e)
						}
						if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE token_sha256=$1 FOR UPDATE`, digest[:]); e != nil {
							_ = lock.Rollback(context.Background())
							t.Fatal(e)
						}
						go func() {
							ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
							defer cancel()
							defer lock.Rollback(context.Background())
							p := proof{}
							for ctx.Err() == nil {
								var now time.Time
								if e := f.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))),clock_timestamp()`, int(lock.Conn().PgConn().PID())).Scan(&p.waiting, &now); e != nil {
									p.err = e
									break
								}
								p.expired = !now.Before(deadline)
								if p.waiting && p.expired {
									break
								}
								time.Sleep(5 * time.Millisecond)
							}
							if p.err == nil {
								p.err = ctx.Err()
							}
							if e := lock.Commit(context.Background()); p.err == nil {
								p.err = e
							}
							completed <- p
						}()
					}}
					if phase == "initial" {
						barrier.after()
						barrier.after = nil
					}
					w := f.request(t, privateProfileHTTPNew(barrier, f.store), "POST", "/v1/organizations/"+orgID+"/agent/ask", `{"query":"如何报名"}`, token, 401, nil)
					if strings.Contains(w.Body.String(), faq.ID) || strings.Contains(w.Body.String(), faq.Answer) {
						t.Fatal("final expired session released public answer")
					}
					select {
					case p := <-completed:
						if p.err != nil || !p.waiting || !p.expired {
							t.Fatalf("no actual Session lock/PG-expiry proof: %+v", p)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("native guard wait did not finish")
					}
				})
			}
		})
	}
}

func (f *organizationAgentNativeReplyBarrier) AnswerOrganization(ctx context.Context, id, viewer, query string) (organization.AgentAnswer, error) {
	a, e := f.Store.AnswerOrganization(ctx, id, viewer, query)
	if e == nil && f.after != nil {
		f.after()
	}
	return a, e
}

func TestOrganizationCapabilityRegisteredHTTPNativePublicAndLateSession(t *testing.T) {
	f := privateProfileHTTPDBNew(t)
	var orgID string
	if e := f.pool.QueryRow(f.ctx, `SELECT id FROM organizations WHERE account_id=$1`, f.accountIDs[2]).Scan(&orgID); e != nil {
		t.Fatal(e)
	}
	f.exec(`INSERT INTO organization_memberships(organization_id,user_account_id,role) VALUES($1,$2,'owner'),($1,$3,'admin')`, orgID, f.accountIDs[0], f.accountIDs[1])
	f.exec(`UPDATE organizations SET verification_status='verified',description='合成HTTP组织公开介绍' WHERE id=$1`, orgID)
	faq, e := f.store.CreateFAQ(f.ctx, f.accountIDs[0], orgID, organization.FAQInput{Question: "如何报名", Answer: "合成HTTP实际发布FAQ答案", Published: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if _, e := f.pool.Exec(context.Background(), `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, f.accountIDs); e != nil {
			t.Errorf("owned organization HTTP audit cleanup: %v", e)
		}
	})
	path := "/v1/organizations/" + orgID + "/agent/ask"
	decode := func(body []byte) organization.AgentAnswer {
		t.Helper()
		var v struct {
			Data organization.AgentAnswer `json:"data"`
		}
		if e := json.Unmarshal(body, &v); e != nil {
			t.Fatal(e)
		}
		return v.Data
	}
	for i, token := range []string{"", f.tokens[0], f.tokens[1]} {
		t.Run("real_public_caller_"+[]string{"guest", "owner", "admin"}[i], func(t *testing.T) {
			w := f.request(t, f.handler, "POST", path, `{"query":"如何报名"}`, token, 200, nil)
			a := decode(w.Body.Bytes())
			if a.Status != "known" || a.Answer != faq.Answer || a.Sources[0].ID != faq.ID || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("registered HTTP failed real published FAQ source")
			}
		})
	}
	f.request(t, f.handler, "POST", path, `{"query":"如何报名"}`, "unknown-org-session", 401, nil)
	if _, e = f.store.UpdateFAQ(f.ctx, f.accountIDs[1], orgID, faq.ID, organization.FAQInput{Question: faq.Question, Answer: "合成当前管理员更新后的FAQ", Published: true}); e != nil {
		t.Fatal(e)
	}
	a := decode(f.request(t, f.handler, "POST", path, `{"query":"如何报名"}`, "", 200, nil).Body.Bytes())
	if a.Answer == faq.Answer || a.Sources[0].ID != faq.ID {
		t.Fatal("old FAQ snapshot reused after native update")
	}
	for _, change := range []string{"revoked", "expired", "idle_expired", "account_suspended"} {
		t.Run("late_"+change, func(t *testing.T) {
			token := f.newSession(f.accountIDs[0], false, false)
			digest, parseErr := identity.ParseBearer("Bearer " + token)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			// The fixture must predate the later expiry. Preserve every real
			// session constraint and authenticate it while it is still valid.
			f.exec(`UPDATE sessions SET created_at=clock_timestamp()-interval '1 hour' WHERE token_sha256=$1`, digest[:])
			barrier := &organizationAgentNativeReplyBarrier{Store: f.store, after: func() {
				sql := map[string]string{"revoked": `UPDATE sessions SET revoked_at=clock_timestamp() WHERE token_sha256=$1`, "expired": `WITH stamp AS MATERIALIZED (SELECT clock_timestamp()-interval '1 second' AS expiry) UPDATE sessions SET idle_expires_at=stamp.expiry,expires_at=stamp.expiry FROM stamp WHERE token_sha256=$1`, "idle_expired": `UPDATE sessions SET idle_expires_at=clock_timestamp()-interval '1 second' WHERE token_sha256=$1`, "account_suspended": `UPDATE accounts SET status='suspended' WHERE id=$2`}[change]
				digest, e := identity.ParseBearer("Bearer " + token)
				if e != nil {
					t.Fatal(e)
				}
				if _, e = f.pool.Exec(f.ctx, sql+` AND $1::bytea IS NOT NULL AND $2::uuid IS NOT NULL`, digest[:], f.accountIDs[0]); e != nil {
					t.Fatal(e)
				}
			}}
			w := f.request(t, privateProfileHTTPNew(barrier, f.store), "POST", path, `{"query":"如何报名"}`, token, 401, nil)
			if strings.Contains(w.Body.String(), "FAQ") || strings.Contains(w.Body.String(), faq.ID) {
				t.Fatal("late native session released grounded answer")
			}
			if change == "account_suspended" {
				f.exec(`UPDATE accounts SET status='active' WHERE id=$1`, f.accountIDs[0])
			}
		})
	}
	f.exec(`UPDATE organizations SET verification_status='unverified' WHERE id=$1`, orgID)
	a = decode(f.request(t, f.handler, "POST", path, `{"query":"如何报名"}`, "", 200, nil).Body.Bytes())
	if a.Status != "unknown" || len(a.Sources) != 0 {
		t.Fatal("revoked verification still answered known")
	}
	t.Log("LOCAL_SYNTHETIC_NATIVE_ONLY: registered public read and native current FAQ; four actual late Session/account invalidations reject401; no production IdP/real organization identity")
}
