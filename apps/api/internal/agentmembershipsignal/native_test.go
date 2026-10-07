package agentmembershipsignal

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentruntime"
	"github.com/birdtie/birdtie/apps/api/internal/community"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
	"os"
	"strings"
	"testing"
	"time"
)

type nativeFixture struct {
	ctx                                                              context.Context
	pool                                                             *pgxpool.Pool
	store                                                            *postgres.Store
	service                                                          *Service
	controller                                                       *agentfeature.Controller
	cfg                                                              agentfeature.Config
	owner, other, agent, community, organization, orgAccount, cm, om string
	access                                                           agentprofile.PrivateAccess
}

func enabledConfig(t *testing.T) agentfeature.Config {
	t.Helper()
	c, e := agentfeature.ParseConfig([]byte(`{"schemaVersion":"agent-feature-flags-v1","flags":{"agent_enrichment":true,"agent_memory":false,"agent_attention_policy":false,"agent_social_policy":false,"life_map":false},"pilot":{"memory":"basic","inference":"conservative","autonomousAction":false,"sensitiveInference":false}}`))
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func fixture(t *testing.T) *nativeFixture {
	t.Helper()
	if os.Getenv("BIRDTIE_DATABASE_URL") == "" || os.Getenv("BIRDTIE_DISPOSABLE_DB") != "1" {
		t.Skip("native membership requires explicitly disposable PostgreSQL")
	}
	ctx := context.Background()
	pool, e := pgxpool.New(ctx, os.Getenv("BIRDTIE_DATABASE_URL"))
	if e != nil {
		t.Fatal("fixture pool failed")
	}
	t.Cleanup(pool.Close)
	f := &nativeFixture{ctx: ctx, pool: pool, store: postgres.New(pool, false), cfg: enabledConfig(t), orgAccount: "00000000-0000-0000-0000-000000000000", community: "00000000-0000-0000-0000-000000000000", organization: "00000000-0000-0000-0000-000000000000"}
	t.Cleanup(func() {
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		people := []string{f.owner, f.other}
		accounts := []string{f.owner, f.other, f.orgAccount}
		for _, statement := range []struct {
			sql  string
			args []any
		}{
			{`DELETE FROM account_blocks WHERE blocker_account_id=ANY($1::uuid[]) OR blocked_account_id=ANY($1::uuid[])`, []any{accounts}},
			{`DELETE FROM contexts WHERE community_id=$1`, []any{f.community}},
			{`DELETE FROM audit_events WHERE actor_account_id=ANY($1::uuid[])`, []any{people}},
			{`DELETE FROM admin_audit_events WHERE organization_id=$1`, []any{f.organization}},
			{`DELETE FROM communities WHERE id=$1`, []any{f.community}},
			{`DELETE FROM organizations WHERE id=$1`, []any{f.organization}},
			{`DELETE FROM sessions WHERE account_id=ANY($1::uuid[])`, []any{accounts}},
			{`DELETE FROM agent_profiles WHERE owner_id=ANY($1::uuid[])`, []any{accounts}},
			{`DELETE FROM agents WHERE principal_account_id=ANY($1::uuid[])`, []any{accounts}},
			{`DELETE FROM user_profiles WHERE account_id=ANY($1::uuid[])`, []any{accounts}},
			{`DELETE FROM accounts WHERE id=ANY($1::uuid[])`, []any{accounts}},
		} {
			if _, e := pool.Exec(c, statement.sql, statement.args...); e != nil {
				t.Errorf("owned cleanup: %v", e)
			}
		}
		var n int
		if e := pool.QueryRow(c, `SELECT count(*) FROM accounts WHERE id=ANY($1::uuid[])`, accounts).Scan(&n); e != nil || n != 0 {
			t.Errorf("owned residue %d %v", n, e)
		}
	})
	for _, dest := range []*string{&f.owner, &f.other} {
		if e = pool.QueryRow(ctx, `INSERT INTO accounts(id,account_type) VALUES(gen_random_uuid(),'person') RETURNING id::text`).Scan(dest); e != nil {
			t.Fatal(e)
		}
	}
	if _, e = pool.Exec(ctx, `INSERT INTO agents(agent_type,principal_account_id) VALUES('personal',$1) ON CONFLICT DO NOTHING`, f.owner); e != nil {
		t.Fatal(e)
	}
	if e = pool.QueryRow(ctx, `SELECT id::text FROM agents WHERE principal_account_id=$1 AND agent_type='personal'`, f.owner).Scan(&f.agent); e != nil {
		t.Fatal(e)
	}
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal("credential fixture failed")
	}
	if _, e = pool.Exec(ctx, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.owner, digest[:]); e != nil {
		t.Fatal(e)
	}
	f.access = agentprofile.PrivateAccess{SessionDigest: digest, WorkspacePrincipal: actorref.PrincipalRef{Type: actorref.Person, ID: f.owner}}
	comm, e := f.store.CreateSocialCommunity(ctx, f.other, community.SocialInput{Name: "合成摄影社群", Summary: "仅用于native验证", Visibility: "public", JoinPolicy: "open"})
	if e != nil {
		t.Fatal(e)
	}
	f.community = comm.ID
	membership, e := f.store.JoinSocialCommunity(ctx, f.owner, f.community)
	if e != nil || membership.Status != "active" {
		t.Fatal("real community join", e)
	}
	f.cm = membership.ID
	org, e := f.store.CreateOrganization(ctx, f.other, organization.CreateInput{Name: "合成组织，不是真实CSSA", OrganizationType: "student_society"})
	if e != nil {
		t.Fatal(e)
	}
	f.organization, f.orgAccount = org.ID, org.AccountID
	invited, e := f.store.InviteMember(ctx, f.other, f.organization, f.owner, "member")
	if e != nil {
		t.Fatal(e)
	}
	f.om = invited.ID
	accepted, e := f.store.AcceptInvitation(ctx, f.owner, f.om)
	if e != nil || accepted.Status != "active" {
		t.Fatal("real organization acceptance", e)
	}
	f.controller, e = agentfeature.NewController(f.cfg)
	if e != nil {
		t.Fatal(e)
	}
	f.service, e = NewService(pool, f.controller, false)
	if e != nil {
		t.Fatal(e)
	}
	return f
}
func (f *nativeFixture) request(k Kind) Request {
	id := f.cm
	if k == Organization {
		id = f.om
	}
	return Request{f.access, agentcognitive.AgentReference{AgentID: f.agent, Principal: f.access.WorkspacePrincipal, Role: agentruntime.PersonalAgent}, k, id, time.Now().UTC().Add(10 * time.Minute)}
}
func (f *nativeFixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	if _, e := f.pool.Exec(f.ctx, sql, args...); e != nil {
		t.Fatal(e)
	}
}
func empty(t *testing.T, s Snapshot, e error) {
	t.Helper()
	if e == nil || s.SchemaVersion != "" || s.Evidence != nil || s.Agent.AgentID != "" {
		t.Fatalf("denial released data: %v", e)
	}
	if strings.Contains(e.Error(), "SELECT") || strings.Contains(e.Error(), "合成") {
		t.Fatal("SQL/private label leaked")
	}
}
func ownRows(t *testing.T, f *nativeFixture) string {
	t.Helper()
	var s string
	e := f.pool.QueryRow(f.ctx, `SELECT jsonb_build_object('memory',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM agent_memories m WHERE owner_id=$1),'community',(SELECT to_jsonb(c) FROM communities c WHERE id=$2),'communityMembers',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM community_memberships m WHERE community_id=$2),'organization',(SELECT to_jsonb(o) FROM organizations o WHERE id=$3),'orgMembers',(SELECT jsonb_agg(to_jsonb(m) ORDER BY id) FROM organization_memberships m WHERE organization_id=$3),'profiles',(SELECT jsonb_agg(to_jsonb(a) ORDER BY agent_id) FROM agent_profiles a WHERE owner_id=ANY($4::uuid[])),'accounts',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM accounts a WHERE id=ANY($4::uuid[])),'agents',(SELECT jsonb_agg(to_jsonb(a) ORDER BY id) FROM agents a WHERE principal_account_id=ANY($4::uuid[])))::text`, f.owner, f.community, f.organization, []string{f.owner, f.other, f.orgAccount}).Scan(&s)
	if e != nil {
		t.Fatal(e)
	}
	return s
}
func TestNativeMembershipActualContextAndAllRoles(t *testing.T) {
	for _, k := range []Kind{Community, Organization} {
		roles := []string{"member", "admin", "owner"}
		if k == Organization {
			roles = []string{"member", "moderator", "admin", "owner"}
		}
		for _, role := range roles {
			t.Run(string(k)+role, func(t *testing.T) {
				f := fixture(t)
				var e error
				if k == Community {
					if role == "owner" {
						e = f.store.TransferSocialOwner(f.ctx, f.other, f.community, f.owner)
					} else if role != "member" {
						_, e = f.store.ChangeSocialMemberRole(f.ctx, f.other, f.community, f.cm, role)
					}
				} else if role != "member" {
					_, e = f.store.ChangeMemberRole(f.ctx, f.other, f.organization, f.om, role)
				}
				if e != nil {
					t.Fatal("native role", e)
				}
				var memoryID string
				if e = f.pool.QueryRow(f.ctx, `SELECT gen_random_uuid()::text`).Scan(&memoryID); e != nil {
					t.Fatal(e)
				}
				if _, e = f.store.PutOwnMemory(f.ctx, f.access, memoryID, agentmemory.PutInput{MemoryType: agentmemory.TypePreference, MemoryKey: "explicit.membership-comparison", Summary: "合成长期声明", StructuredValue: json.RawMessage(`{"declared":true}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(time.Hour)}); e != nil {
					t.Fatal(e)
				}
				before := ownRows(t, f)
				out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
				if e != nil || len(out.Evidence) != 1 || out.Evidence[0].Role != role || out.Evidence[0].Kind != k || out.Scope != "PERSONAL_MEMBERSHIP_CONTEXT" || out.Purpose != "HUMAN_SELF_REVIEW" || out.InterestInferred || out.IdentityVerified || out.FriendEstablished || out.MemoryPromotionAllowed || out.ModelAccess != "UNAVAILABLE" {
					t.Fatal("native assembled context", e)
				}
				if k == Organization && (out.Evidence[0].ResourceID != f.organization || out.Evidence[0].PrincipalAccountID != f.orgAccount) {
					t.Fatal("org principal namespace")
				}
				same, e := f.service.RevalidateOwn(f.ctx, f.access, out)
				if e != nil || !same.ExpiresAt.Equal(out.ExpiresAt) {
					t.Fatal("current no-renew", e)
				}
				wire, e := json.Marshal(out)
				if e != nil || strings.Contains(string(wire), "session") || strings.Contains(string(wire), "authority") || strings.Contains(string(wire), "长期声明") {
					t.Fatal("wire leakage")
				}
				if ownRows(t, f) != before {
					t.Fatal("context mutated Memory or native source")
				}
				if _, e = f.service.ReadForCognition(f.ctx, agentcognitive.ReadRequest{}); !errors.Is(e, agentcognitive.ErrUnavailable) {
					t.Fatal("native model permission")
				}
			})
		}
	}
}
func TestNativeMembershipCurrentNegativeGuards(t *testing.T) {
	for _, k := range []Kind{Community, Organization} {
		for _, name := range []string{"sessionRevoked", "sessionExpired", "personInactive", "agentInactive", "metadataAbsent", "metadataChanged", "memberAbsent", "memberInactive", "memberInvited", "resourceInactive", "resourceHiddenOrPrivate", "ownerBlocked", "sourceFuture", "sourceInfinity", "resourceFuture", "crossOwner", "wrongAgent", "crossKind", "flagOff", "orgPrincipal", "resourceDeleted", "requestExpired", "canceled"} {
			t.Run(string(k)+name, func(t *testing.T) {
				f := fixture(t)
				r := f.request(k)
				table, id := "community_memberships", f.cm
				if k == Organization {
					table, id = "organization_memberships", f.om
				}
				ctx := f.ctx
				switch name {
				case "sessionRevoked":
					f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.owner)
				case "sessionExpired":
					f.exec(t, `UPDATE sessions SET expires_at=created_at+interval '1 microsecond',idle_expires_at=created_at+interval '1 microsecond' WHERE account_id=$1`, f.owner)
				case "personInactive":
					f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.owner)
				case "agentInactive":
					f.exec(t, `UPDATE agents SET status='retired' WHERE id=$1`, f.agent)
				case "metadataAbsent":
					f.exec(t, `DELETE FROM agent_profiles WHERE agent_id=$1`, f.agent)
				case "metadataChanged":
					f.exec(t, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, f.agent)
					out, e := f.service.ReadOwnMembershipContext(ctx, r)
					if e != nil {
						t.Fatal(e)
					}
					f.exec(t, `UPDATE agent_profiles SET profile_version=profile_version+1 WHERE agent_id=$1`, f.agent)
					out, e = f.service.RevalidateOwn(ctx, f.access, out)
					empty(t, out, e)
					return
				case "memberAbsent":
					f.exec(t, fmt.Sprintf(`DELETE FROM %s WHERE id=$1`, table), id)
				case "memberInactive":
					if k == Community {
						if e := f.store.LeaveSocialCommunity(f.ctx, f.owner, f.community); e != nil {
							t.Fatal(e)
						}
					} else {
						if e := f.store.RevokeMember(f.ctx, f.other, f.organization, f.om); e != nil {
							t.Fatal(e)
						}
					}
				case "memberInvited":
					f.exec(t, fmt.Sprintf(`UPDATE %s SET status='invited' WHERE id=$1`, table), id)
				case "resourceInactive":
					if k == Community {
						if e := f.store.ArchiveSocialCommunity(f.ctx, f.other, f.community); e != nil {
							t.Fatal(e)
						}
					} else {
						f.exec(t, `UPDATE organizations SET status='closed' WHERE id=$1`, f.organization)
					}
				case "resourceHiddenOrPrivate":
					if k == Community {
						f.exec(t, `UPDATE communities SET visibility='hidden' WHERE id=$1`, f.community)
					} else {
						f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.orgAccount)
					}
				case "ownerBlocked":
					f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.other, f.owner)
				case "sourceFuture":
					f.exec(t, fmt.Sprintf(`UPDATE %s SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, table), id)
				case "sourceInfinity":
					f.exec(t, fmt.Sprintf(`UPDATE %s SET updated_at='infinity' WHERE id=$1`, table), id)
				case "resourceFuture":
					if k == Community {
						f.exec(t, `UPDATE communities SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.community)
					} else {
						f.exec(t, `UPDATE organizations SET updated_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.organization)
					}
				case "crossOwner":
					r.Access.WorkspacePrincipal.ID = f.other
					r.Agent.Principal = r.Access.WorkspacePrincipal
				case "wrongAgent":
					r.Agent.AgentID = f.om
				case "crossKind":
					if k == Community {
						r.Kind = Organization
					} else {
						r.Kind = Community
					}
				case "flagOff":
					if e := f.controller.Disable(agentfeature.Enrichment); e != nil {
						t.Fatal(e)
					}
				case "orgPrincipal":
					r.Access.WorkspacePrincipal = actorref.PrincipalRef{Type: actorref.Organization, ID: f.orgAccount}
					r.Agent.Principal = r.Access.WorkspacePrincipal
					r.Agent.Role = agentruntime.OrganizationAgent
				case "resourceDeleted":
					if k == Community {
						f.exec(t, `DELETE FROM communities WHERE id=$1`, f.community)
					} else {
						f.exec(t, `DELETE FROM organizations WHERE id=$1`, f.organization)
					}
				case "requestExpired":
					r.DeadlineAt = time.Now().UTC().Add(-time.Second)
				case "canceled":
					c, cancel := context.WithCancel(ctx)
					cancel()
					ctx = c
				}
				out, e := f.service.ReadOwnMembershipContext(ctx, r)
				empty(t, out, e)
			})
		}
	}
}
func TestNativeMembershipRevalidationMutationsAndTamper(t *testing.T) {
	for _, k := range []Kind{Community, Organization} {
		for _, name := range []string{"roleSameTimestamp", "sameIDRebuilt", "metadataRebuilt", "accountMutation", "offOn", "otherService", "tamperLabel", "tamperTime", "tamperModel", "anotherSession", "expiredLease"} {
			t.Run(string(k)+name, func(t *testing.T) {
				f := fixture(t)
				r := f.request(k)
				if name == "expiredLease" {
					r.DeadlineAt = time.Now().UTC().Add(250 * time.Millisecond)
				}
				out, e := f.service.ReadOwnMembershipContext(f.ctx, r)
				if e != nil {
					t.Fatal(e)
				}
				service, access := f.service, f.access
				table, resource, id := "community_memberships", "community_id", f.cm
				if k == Organization {
					table, resource, id = "organization_memberships", "organization_id", f.om
				}
				switch name {
				case "roleSameTimestamp":
					f.exec(t, fmt.Sprintf(`UPDATE %s SET role='admin' WHERE id=$1`, table), id)
				case "sameIDRebuilt":
					f.exec(t, fmt.Sprintf(`WITH old AS(DELETE FROM %s WHERE id=$1 RETURNING *) INSERT INTO %s(id,%s,user_account_id,role,status,created_at,updated_at) SELECT id,%s,user_account_id,role,status,created_at,updated_at FROM old`, table, table, resource, resource), id)
				case "metadataRebuilt":
					f.exec(t, `WITH old AS(DELETE FROM agent_profiles WHERE agent_id=$1 RETURNING *) INSERT INTO agent_profiles(agent_id,owner_type,owner_id,profile_version,created_at,updated_at) SELECT agent_id,owner_type,owner_id,profile_version,created_at,updated_at FROM old`, f.agent)
				case "accountMutation":
					f.exec(t, `UPDATE accounts SET status=status WHERE id=$1`, f.owner)
				case "offOn":
					if e = f.controller.Disable(agentfeature.Enrichment); e != nil {
						t.Fatal(e)
					}
					if e = f.controller.Replace(f.controller.Revision(), f.cfg); e != nil {
						t.Fatal(e)
					}
				case "otherService":
					service, e = NewService(f.pool, f.controller, false)
					if e != nil {
						t.Fatal(e)
					}
				case "tamperLabel":
					out.Evidence[0].Label = "伪造"
				case "tamperTime":
					out.ExpiresAt = out.ExpiresAt.Add(time.Hour)
				case "tamperModel":
					out.ModelAccess = "ALLOWED"
				case "anotherSession":
					_, d, e := identity.NewToken()
					if e != nil {
						t.Fatal(e)
					}
					f.exec(t, `INSERT INTO sessions(account_id,token_sha256,authentication_method,expires_at,idle_expires_at) VALUES($1,$2,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, f.owner, d[:])
					access.SessionDigest = d
				case "expiredLease":
					time.Sleep(300 * time.Millisecond)
				}
				out, e = service.RevalidateOwn(f.ctx, access, out)
				empty(t, out, e)
			})
		}
	}
}
func TestNativeMembershipFinalSQLConcurrentRevocation(t *testing.T) {
	for _, k := range []Kind{Community, Organization} {
		for _, name := range []string{"memberRevoked", "roleChanged", "sessionRevoked", "agentRevoked", "blocked", "flagRevoked", "cancelLate", "deadlineLate"} {
			t.Run(string(k)+name, func(t *testing.T) {
				f := fixture(t)
				r := f.request(k)
				ctx, cancel := context.WithCancel(f.ctx)
				defer cancel()
				if name == "deadlineLate" {
					r.DeadlineAt = time.Now().UTC().Add(350 * time.Millisecond)
				}
				reached, release := make(chan struct{}), make(chan struct{})
				f.service.beforeFinal = func() { close(reached); <-release }
				type response struct {
					s Snapshot
					e error
				}
				done := make(chan response, 1)
				go func() { s, e := f.service.ReadOwnMembershipContext(ctx, r); done <- response{s, e} }()
				select {
				case <-reached:
				case v := <-done:
					t.Fatalf("before barrier: %v", v.e)
				case <-time.After(5 * time.Second):
					t.Fatal("barrier timeout")
				}
				switch name {
				case "memberRevoked":
					if k == Community {
						if e := f.store.RemoveSocialMember(f.ctx, f.other, f.community, f.cm); e != nil {
							t.Fatal(e)
						}
					} else {
						if e := f.store.RevokeMember(f.ctx, f.other, f.organization, f.om); e != nil {
							t.Fatal(e)
						}
					}
				case "roleChanged":
					if k == Community {
						if _, e := f.store.ChangeSocialMemberRole(f.ctx, f.other, f.community, f.cm, "admin"); e != nil {
							t.Fatal(e)
						}
					} else {
						if _, e := f.store.ChangeMemberRole(f.ctx, f.other, f.organization, f.om, "admin"); e != nil {
							t.Fatal(e)
						}
					}
				case "sessionRevoked":
					f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE account_id=$1`, f.owner)
				case "agentRevoked":
					f.exec(t, `UPDATE agents SET status='retired' WHERE id=$1`, f.agent)
				case "blocked":
					f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.owner, f.other)
				case "flagRevoked":
					if e := f.controller.Disable(agentfeature.Enrichment); e != nil {
						t.Fatal(e)
					}
				case "cancelLate":
					cancel()
				case "deadlineLate":
					time.Sleep(400 * time.Millisecond)
				}
				close(release)
				select {
				case v := <-done:
					empty(t, v.s, v.e)
				case <-time.After(5 * time.Second):
					t.Fatal("response timeout")
				}
			})
		}
	}
}
func TestNativeMembershipSourceBoundariesAndConnectionTimeZone(t *testing.T) {
	for _, name := range []string{"communityPending", "communityRejected", "communityExpired", "communityVerifyFuture", "communityVerifyInfinity", "communityDraft", "orgPrivateOwn", "communityPrivateOwn", "orgBlockedPrincipal", "orgOwnerInactive", "organizationLabelMutation"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			k := Community
			positive := false
			switch name {
			case "communityPending":
				f.exec(t, `UPDATE community_memberships SET status='pending' WHERE id=$1`, f.cm)
			case "communityRejected":
				f.exec(t, `UPDATE community_memberships SET status='rejected' WHERE id=$1`, f.cm)
			case "communityExpired":
				f.exec(t, `UPDATE communities SET expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, f.community)
			case "communityVerifyFuture":
				f.exec(t, `UPDATE communities SET verified_at=clock_timestamp()+interval '1 day' WHERE id=$1`, f.community)
			case "communityVerifyInfinity":
				f.exec(t, `UPDATE communities SET verified_at='infinity' WHERE id=$1`, f.community)
			case "communityDraft":
				f.exec(t, `UPDATE communities SET publication_status='draft' WHERE id=$1`, f.community)
			case "orgPrivateOwn":
				k = Organization
				positive = true
				f.exec(t, `UPDATE organizations SET visibility='private' WHERE id=$1`, f.organization)
			case "communityPrivateOwn":
				positive = true
				f.exec(t, `UPDATE communities SET visibility='private' WHERE id=$1`, f.community)
			case "orgBlockedPrincipal":
				k = Organization
				f.exec(t, `INSERT INTO account_blocks(blocker_account_id,blocked_account_id) VALUES($1,$2)`, f.owner, f.orgAccount)
			case "orgOwnerInactive":
				k = Organization
				f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, f.other)
			case "organizationLabelMutation":
				k = Organization
				out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
				if e != nil {
					t.Fatal(e)
				}
				f.exec(t, `UPDATE organizations SET name='已变名称' WHERE id=$1`, f.organization)
				out, e = f.service.RevalidateOwn(f.ctx, f.access, out)
				empty(t, out, e)
				return
			}
			out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
			if positive {
				if e != nil || len(out.Evidence) != 1 {
					t.Fatal("private own fact", e)
				}
			} else {
				empty(t, out, e)
			}
		})
	}
	for _, k := range []Kind{Community, Organization} {
		t.Run("timeZone"+string(k), func(t *testing.T) {
			f := fixture(t)
			cfg, e := pgxpool.ParseConfig(os.Getenv("BIRDTIE_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			cfg.MaxConns = 2
			p, e := pgxpool.NewWithConfig(f.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer p.Close()
			a, e := p.Acquire(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			b, e := p.Acquire(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer func() { a.Release() }()
			defer func() { b.Release() }()
			if _, e = a.Exec(f.ctx, `SET TIME ZONE 'Asia/Shanghai'`); e != nil {
				t.Fatal(e)
			}
			if _, e = b.Exec(f.ctx, `SET TIME ZONE 'America/New_York'`); e != nil {
				t.Fatal(e)
			}
			b.Release()
			service, e := NewService(p, f.controller, false)
			if e != nil {
				t.Fatal(e)
			}
			service.beforeFinal = func() {
				b, e = p.Acquire(f.ctx)
				if e != nil {
					t.Fatal(e)
				}
				a.Release()
			}
			out, e := service.ReadOwnMembershipContext(f.ctx, f.request(k))
			if e != nil {
				t.Fatal("different connection final", e)
			}
			if _, e = service.RevalidateOwn(f.ctx, f.access, out); e != nil {
				t.Fatal("timezone canonical", e)
			}
			var zone string
			if e = b.QueryRow(f.ctx, `SHOW TIME ZONE`).Scan(&zone); e != nil || zone != "America/New_York" {
				t.Fatal("global timezone changed", e)
			}
		})
	}
}

func TestNativeMembershipCompositeNativeCases(t *testing.T) {
	for _, name := range []string{"defaultOff", "sameTypedUUID", "boundedSourceLease", "communityRejoin", "organizationReaccept", "communityTwoOwners", "communityOwnerFuture", "organizationOwnerInfinity", "sameContentMutation"} {
		t.Run(name, func(t *testing.T) {
			f := fixture(t)
			k := Community
			switch name {
			case "defaultOff":
				c, e := agentfeature.NewController(agentfeature.DefaultConfig())
				if e != nil {
					t.Fatal(e)
				}
				s, e := NewService(f.pool, c, false)
				if e != nil {
					t.Fatal(e)
				}
				before := ownRows(t, f)
				out, e := s.ReadOwnMembershipContext(f.ctx, f.request(Community))
				empty(t, out, e)
				if !errors.Is(e, ErrUnavailable) || ownRows(t, f) != before {
					t.Fatal("OFF did not brake real source")
				}
				return
			case "sameTypedUUID":
				// This exclusively owned fixture establishes a UUID collision
				// before reading any membership evidence. Native organization
				// membership IDs are now referenced by notification decisions;
				// keep that source/FK intact and adjust the unreferenced fixture
				// community row instead. All typed-domain assertions stay intact.
				f.exec(t, `UPDATE community_memberships SET id=$1 WHERE id=$2`, f.om, f.cm)
				f.cm = f.om
				a, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(Community))
				if e != nil {
					t.Fatal(e)
				}
				b, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(Organization))
				if e != nil {
					t.Fatal(e)
				}
				if a.Evidence[0].MembershipID != b.Evidence[0].MembershipID || a.Evidence[0].Kind == b.Evidence[0].Kind || a.Evidence[0].ResourceID == b.Evidence[0].ResourceID || a.Evidence[0].SourceVersion == b.Evidence[0].SourceVersion {
					t.Fatal("typed UUID domains collapsed")
				}
				b.Evidence[0].Kind = Community
				b, e = f.service.RevalidateOwn(f.ctx, f.access, b)
				empty(t, b, e)
				return
			case "boundedSourceLease":
				f.exec(t, `UPDATE communities SET expires_at=clock_timestamp()+interval '2 seconds' WHERE id=$1`, f.community)
				out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(Community))
				if e != nil || out.ExpiresAt.Sub(out.ObservedAt) > 2*time.Second {
					t.Fatal("source expiry not bounding lease", e)
				}
				return
			case "communityRejoin", "organizationReaccept":
				if name == "organizationReaccept" {
					k = Organization
				}
				old, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
				if e != nil {
					t.Fatal(e)
				}
				if k == Community {
					if e = f.store.LeaveSocialCommunity(f.ctx, f.owner, f.community); e != nil {
						t.Fatal(e)
					}
					if _, e = f.store.JoinSocialCommunity(f.ctx, f.owner, f.community); e != nil {
						t.Fatal(e)
					}
				} else {
					if e = f.store.RevokeMember(f.ctx, f.other, f.organization, f.om); e != nil {
						t.Fatal(e)
					}
					if _, e = f.store.InviteMember(f.ctx, f.other, f.organization, f.owner, "member"); e != nil {
						t.Fatal(e)
					}
					if _, e = f.store.AcceptInvitation(f.ctx, f.owner, f.om); e != nil {
						t.Fatal(e)
					}
				}
				if current, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k)); e != nil || len(current.Evidence) != 1 {
					t.Fatal("current restored membership", e)
				}
				old, e = f.service.RevalidateOwn(f.ctx, f.access, old)
				empty(t, old, e)
				return
			case "communityTwoOwners":
				f.exec(t, `UPDATE community_memberships SET role='owner' WHERE id=$1`, f.cm)
			case "communityOwnerFuture":
				f.exec(t, `UPDATE community_memberships SET updated_at=clock_timestamp()+interval '1 day' WHERE community_id=$1 AND role='owner'`, f.community)
			case "organizationOwnerInfinity":
				k = Organization
				f.exec(t, `UPDATE organization_memberships SET updated_at='infinity' WHERE organization_id=$1 AND role='owner'`, f.organization)
			case "sameContentMutation":
				out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
				if e != nil {
					t.Fatal(e)
				}
				f.exec(t, `UPDATE community_memberships SET role=role,updated_at=updated_at WHERE id=$1`, f.cm)
				out, e = f.service.RevalidateOwn(f.ctx, f.access, out)
				empty(t, out, e)
				return
			}
			out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(k))
			empty(t, out, e)
		})
	}
}

func TestNativeMembershipRapidClockRevalidation(t *testing.T) {
	f := fixture(t)
	for _, kind := range []Kind{Community, Organization} {
		t.Run(string(kind), func(t *testing.T) {
			for i := 0; i < 100; i++ {
				out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(kind))
				if e != nil {
					t.Fatal("rapid read", e)
				}
				host := time.Now().UTC()
				_, e = f.service.RevalidateOwn(f.ctx, f.access, out)
				if e != nil {
					var databaseNow time.Time
					if clockErr := f.pool.QueryRow(f.ctx, `SELECT clock_timestamp()`).Scan(&databaseNow); clockErr != nil {
						t.Fatal("clock diagnostic", clockErr)
					}
					t.Fatalf("rapid current revalidate i=%d err=%v observed=%s hostBefore=%s pgAfter=%s observedMinusHost=%s lease=%s", i, e, out.ObservedAt.Format(time.RFC3339Nano), host.Format(time.RFC3339Nano), databaseNow.Format(time.RFC3339Nano), out.ObservedAt.Sub(host), out.ExpiresAt.Sub(out.ObservedAt))
				}
			}
		})
	}
}

func TestNativeMembershipClockAuthorityNegativeControl(t *testing.T) {
	f := fixture(t)
	out, e := f.service.ReadOwnMembershipContext(f.ctx, f.request(Organization))
	if e != nil {
		t.Fatal(e)
	}
	// This host offset is explicitly a simulated negative control; neither the
	// database nor OS clock is changed, and no fake clock is installed on Service.
	simulatedHost := out.ObservedAt.Add(-time.Millisecond)
	legacyRejected := out.ObservedAt.After(simulatedHost) || !out.ExpiresAt.After(simulatedHost)
	dbNow, e := f.service.native.clock(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if !legacyRejected || snapshotTimeAt(out, dbNow) != nil {
		t.Fatal("two independent clocks negative control")
	}
	t.Log("NEGATIVE CONTROL: simulated host 1ms behind DB observation makes prior host comparison reject a current native snapshot; strict actual DB comparison accepts")
	future := cloneSnapshot(out)
	future.ObservedAt = dbNow.Add(time.Nanosecond)
	if !errors.Is(snapshotTimeAt(future, dbNow), ErrExpired) {
		t.Fatal("future DB observation was relaxed")
	}
	if current, e := f.service.RevalidateOwn(f.ctx, f.access, out); e != nil || !current.ExpiresAt.Equal(out.ExpiresAt) {
		t.Fatal("native strict DB-time revalidate", e)
	}
}
