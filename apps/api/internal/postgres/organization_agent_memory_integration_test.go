package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/birdtie/birdtie/apps/api/internal/agentmemory"
	"github.com/birdtie/birdtie/apps/api/internal/agentorganizationmemory"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/jackc/pgx/v5/pgxpool"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type orgMemoryFixture struct {
	p      *agentPrivateFixture
	org    organization.Organization
	access agentorganizationmemory.Access
}

func orgMemoryNativeSnapshot(t *testing.T, f *orgMemoryFixture) string {
	t.Helper()
	b := f.p.base
	var raw string
	if e := b.pool.QueryRow(b.ctx, `SELECT jsonb_build_object('memories',(SELECT coalesce(jsonb_agg(to_jsonb(m) ORDER BY id),'[]') FROM agent_memories m WHERE owner_id=$1),'audit',(SELECT coalesce(jsonb_agg(to_jsonb(a) ORDER BY id),'[]') FROM admin_audit_events a WHERE organization_id=$2 AND resource_type='organization_memory'))::text`, f.org.AccountID, f.org.ID).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	return raw + seedEffects(t, f.p)
}
func orgMemoryNativeWait(t *testing.T, f *orgMemoryFixture, pid uint32, deadline *time.Time) {
	t.Helper()
	b := f.p.base
	limit := time.Now().Add(4 * time.Second)
	for time.Now().Before(limit) {
		var waiting bool
		var now time.Time
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_stat_activity WHERE $1::integer=ANY(pg_blocking_pids(pid))),clock_timestamp()`, int(pid)).Scan(&waiting, &now); e != nil {
			t.Fatal(e)
		}
		if waiting && (deadline == nil || !now.Before(*deadline)) {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("actual Org Memory lock/PG deadline not observed")
}
func TestOrganizationMemoryNativeSessionExpiryDuringMemoryLock(t *testing.T) {
	for _, operation := range []string{"list", "put", "delete"} {
		for _, kind := range []string{"absolute", "idle"} {
			t.Run(operation+"_"+kind, func(t *testing.T) {
				f := newOrgMemoryFixture(t)
				b := f.p.base
				id := agentMemoryID(t, f.p)
				in := orgMemoryTestInput(agentmemory.OrgPolicy, "wait")
				if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
					t.Fatal(e)
				}
				before := orgMemoryNativeSnapshot(t, f)
				lock, e := b.pool.Begin(b.ctx)
				if e != nil {
					t.Fatal(e)
				}
				defer lock.Rollback(context.Background())
				if _, e = lock.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, id); e != nil {
					t.Fatal(e)
				}
				var deadline time.Time
				sql := `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE sessions SET created_at=stamp.at-interval '1 hour',idle_expires_at=stamp.at+interval '600 milliseconds',expires_at=stamp.at+interval '1 hour' FROM stamp WHERE token_sha256=$1 RETURNING idle_expires_at`
				if kind == "absolute" {
					sql = strings.Replace(sql, "expires_at=stamp.at+interval '1 hour'", "expires_at=stamp.at+interval '600 milliseconds'", 1)
				}
				if e := b.pool.QueryRow(b.ctx, sql, f.access.SessionDigest[:]).Scan(&deadline); e != nil {
					t.Fatal(e)
				}
				done := make(chan error, 1)
				go func() {
					var e error
					switch operation {
					case "list":
						v, x := b.store.ListOrganizationMemories(b.ctx, f.access)
						e = x
						if v != nil && x != nil {
							e = fmt.Errorf("denied list released rows: %w", x)
						}
					case "put":
						in.ExpectedVersion = 1
						in.Summary = "合成等待过期禁止写入"
						_, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
					case "delete":
						_, e = b.store.DeleteOrganizationMemory(b.ctx, f.access, id, 1)
					}
					done <- e
				}()
				orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), &deadline)
				if e := lock.Commit(b.ctx); e != nil {
					t.Fatal(e)
				}
				select {
				case e := <-done:
					if !errors.Is(e, agentmemory.ErrForbidden) {
						t.Fatal("natural session expiry accepted", e)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("expired native request did not finish")
				}
				if before != orgMemoryNativeSnapshot(t, f) {
					t.Fatal("expired wait committed content/revision/audit")
				}
			})
		}
	}
}
func TestOrganizationMemoryNativeLateRoleAndCancellation(t *testing.T) {
	for _, change := range []string{"role", "cancel"} {
		t.Run(change, func(t *testing.T) {
			f := newOrgMemoryFixture(t)
			b := f.p.base
			id := agentMemoryID(t, f.p)
			in := orgMemoryTestInput(agentmemory.OrgVenue, "late")
			if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
				t.Fatal(e)
			}
			before := orgMemoryNativeSnapshot(t, f)
			a := f.access
			if change == "role" {
				inv, e := b.store.InviteMember(b.ctx, b.person.ID, f.org.ID, b.other.ID, "member")
				if e != nil {
					t.Fatal(e)
				}
				if _, e = b.store.AcceptInvitation(b.ctx, b.other.ID, inv.ID); e != nil {
					t.Fatal(e)
				}
				if _, e = b.store.ChangeMemberRole(b.ctx, b.person.ID, f.org.ID, inv.ID, "admin"); e != nil {
					t.Fatal(e)
				}
				a.SessionDigest = f.p.peer.SessionDigest
				a.ActingPersonID = b.other.ID
			}
			lock, e := b.pool.Begin(b.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer lock.Rollback(context.Background())
			if change == "role" {
				_, e = lock.Exec(b.ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, f.org.ID)
			} else {
				_, e = lock.Exec(b.ctx, `SELECT id FROM agent_memories WHERE id=$1 FOR UPDATE`, id)
			}
			if e != nil {
				t.Fatal(e)
			}
			ctx, cancel := context.WithCancel(b.ctx)
			defer cancel()
			done := make(chan error, 1)
			go func() {
				in.ExpectedVersion = 1
				in.Summary = "合成迟到批准不得提交"
				_, e := b.store.PutOrganizationMemory(ctx, a, id, in)
				done <- e
			}()
			orgMemoryNativeWait(t, f, lock.Conn().PgConn().PID(), nil)
			if change == "role" {
				if _, e = lock.Exec(b.ctx, `UPDATE organization_memberships SET role='member' WHERE organization_id=$1 AND user_account_id=$2`, f.org.ID, b.other.ID); e != nil {
					t.Fatal(e)
				}
			} else {
				cancel()
			}
			if e := lock.Commit(b.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e := <-done:
				expected := agentmemory.ErrForbidden
				if change == "cancel" {
					expected = agentmemory.ErrUnavailable
				}
				if !errors.Is(e, expected) {
					t.Fatal("late current authority/cancel", e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("late request stuck")
			}
			if before != orgMemoryNativeSnapshot(t, f) {
				t.Fatal("late denied write changed Memory/audit/Person source")
			}
		})
	}
}
func TestOrganizationMemoryNativeCapacity128AndDeleteReleasesSlot(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	var first string
	for i := 0; i < agentorganizationmemory.MaxRecords; i++ {
		id := agentMemoryID(t, f.p)
		if i == 0 {
			first = id
		}
		if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, orgMemoryTestInput(agentmemory.OrgAnnouncement, fmt.Sprintf("capacity-%d", i))); e != nil {
			t.Fatalf("slot %d: %v", i, e)
		}
	}
	list, e := b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil || len(list) != 128 {
		t.Fatal("128 readable rows", len(list), e)
	}
	before := orgMemoryNativeSnapshot(t, f)
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, agentMemoryID(t, f.p), orgMemoryTestInput(agentmemory.OrgAnnouncement, "capacity-129")); !errors.Is(e, agentorganizationmemory.ErrLimit) {
		t.Fatal("129th declaration accepted", e)
	}
	if before != orgMemoryNativeSnapshot(t, f) {
		t.Fatal("limit denial wrote row/audit")
	}
	if _, e = b.store.DeleteOrganizationMemory(b.ctx, f.access, first, 1); e != nil {
		t.Fatal(e)
	}
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, agentMemoryID(t, f.p), orgMemoryTestInput(agentmemory.OrgAnnouncement, "capacity-after-delete")); e != nil {
		t.Fatal("delete did not release slot", e)
	}
	list, e = b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil || len(list) != 128 {
		t.Fatal("replacement capacity", len(list), e)
	}
	// An elevated SQL writer can bypass the API capacity policy. The reader
	// must reject overflow rather than quietly releasing a truncated ledger.
	extra := agentMemoryID(t, f.p)
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO agent_memories(id,agent_id,owner_type,owner_id,memory_type,memory_key,summary,structured_value,valid_from,valid_until)
	 SELECT $1,agent_id,owner_type,owner_id,memory_type,'org.v1.announcement.sql-overflow','合成SQL容量探针',structured_value,valid_from,valid_until
	 FROM agent_memories WHERE owner_id=$2 AND status='ACTIVE' LIMIT 1`, extra, f.org.AccountID); e != nil {
		t.Fatal(e)
	}
	if list, e := b.store.ListOrganizationMemories(b.ctx, f.access); !errors.Is(e, agentmemory.ErrUnavailable) || list != nil {
		t.Fatal("raw overflow silently truncated", len(list), e)
	}
	if _, e := b.store.DeleteOrganizationMemory(b.ctx, f.access, extra, 1); e != nil {
		t.Fatal("overflow recovery delete failed", e)
	}
	if list, e := b.store.ListOrganizationMemories(b.ctx, f.access); e != nil || len(list) != 128 {
		t.Fatal("overflow recovery not readable", len(list), e)
	}
}
func TestOrganizationMemoryNativeRawSQLShapeAndOldReinforcementBoundary(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	id := agentMemoryID(t, f.p)
	if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, orgMemoryTestInput(agentmemory.OrgFAQ, "raw")); e != nil {
		t.Fatal(e)
	}
	before := orgMemoryNativeSnapshot(t, f)
	changes := []string{`memory_type='PLACE'`, `memory_key='org.v1.unknown.raw'`, `structured_value='{"grant":"not-authority"}'::jsonb`, `structured_value='{"note":null}'::jsonb`, `structured_value='{"tags":[null]}'::jsonb`, `structured_value='{"tags":[""]}'::jsonb`, `source_type='INFERRED',status='PENDING_REVIEW',confidence=0.8`, `confidence=0.7`, `owner_type='PERSON'`, `last_reinforced_at=clock_timestamp()`}
	for i, change := range changes {
		t.Run(fmt.Sprintf("shape_%d", i), func(t *testing.T) {
			if _, e := b.pool.Exec(b.ctx, `UPDATE agent_memories SET version=version+1,`+change+` WHERE id=$1`, id); e == nil {
				t.Fatal("raw invalid mutation accepted", change)
			}
			if before != orgMemoryNativeSnapshot(t, f) {
				t.Fatal("raw rejection not atomic")
			}
		})
	}
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO agent_memory_reinforcement(memory_id,memory_version) VALUES($1,1)`, id); e == nil {
		t.Fatal("old empty reinforcement accepted Organization owner")
	}
	if _, e := b.pool.Exec(b.ctx, `INSERT INTO admin_audit_events(actor_account_id,organization_id,resource_type,resource_id,action) VALUES($1,$2,'faq',$3,'organization_memory_put')`, b.person.ID, f.org.ID, id); e == nil {
		t.Fatal("new action paired with old resource")
	}
	if before != orgMemoryNativeSnapshot(t, f) {
		t.Fatal("guard changed native rows")
	}
}
func TestOrganizationMemoryNativeAuditWaitDeadlineRollsBack(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	id := agentMemoryID(t, f.p)
	in := orgMemoryTestInput(agentmemory.OrgPolicy, "audit-deadline")
	if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
		t.Fatal(e)
	}
	before := orgMemoryNativeSnapshot(t, f)
	var marker int64
	if e := b.pool.QueryRow(b.ctx, `SELECT hashtextextended($1,0)`, id).Scan(&marker); e != nil {
		t.Fatal(e)
	}
	suffix := strings.ReplaceAll(id, "-", "")
	fn := "org_memory_deadline_" + suffix
	trigger := "zz_org_memory_deadline_" + suffix
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()+interval '1500 milliseconds'`).Scan(&in.ValidUntil); e != nil {
		t.Fatal(e)
	}
	b.exec(`CREATE FUNCTION ` + fn + `() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_advisory_xact_lock(` + strconv.FormatInt(marker, 10) + `::bigint); WHILE clock_timestamp()<'` + in.ValidUntil.Format(time.RFC3339Nano) + `'::timestamptz LOOP PERFORM pg_sleep(0.005); END LOOP; RETURN NEW; END $$`)
	t.Cleanup(func() {
		if _, e := b.pool.Exec(context.Background(), `DROP TRIGGER IF EXISTS `+trigger+` ON admin_audit_events;DROP FUNCTION `+fn+`() `); e != nil {
			t.Error("owned Org audit trigger cleanup", e)
		}
	})
	b.exec(`CREATE TRIGGER ` + trigger + ` BEFORE INSERT ON admin_audit_events FOR EACH ROW WHEN(NEW.resource_id='` + id + `'::uuid) EXECUTE FUNCTION ` + fn + `() `)
	in.ExpectedVersion = 1
	in.Summary = "合成audit等待不得提交"
	done := make(chan error, 1)
	go func() { _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); done <- e }()
	entered := false
	limit := time.Now().Add(3 * time.Second)
	for time.Now().Before(limit) {
		if e := b.pool.QueryRow(b.ctx, `SELECT EXISTS(SELECT 1 FROM pg_locks WHERE locktype='advisory' AND granted AND classid=((($1::bigint>>32)&4294967295)::oid) AND objid=(($1::bigint&4294967295)::oid) AND objsubid=1 AND database=(SELECT oid FROM pg_database WHERE datname=current_database()))`, marker).Scan(&entered); e != nil {
			t.Fatal(e)
		}
		if entered {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if !entered {
		t.Fatal("real audit trigger not entered")
	}
	select {
	case e := <-done:
		if !errors.Is(e, agentmemory.ErrInvalid) {
			t.Fatal("final audit deadline accepted", e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("audit deadline operation stuck")
	}
	var now time.Time
	if e := b.pool.QueryRow(b.ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil || now.Before(in.ValidUntil) {
		t.Fatal("PG deadline did not pass", e)
	}
	if before != orgMemoryNativeSnapshot(t, f) {
		t.Fatal("final audit deadline committed Memory or audit")
	}
}

func newOrgMemoryFixture(t *testing.T) *orgMemoryFixture {
	t.Helper()
	p := agentPrivateTestFixture(t)
	b := p.base
	f := &orgMemoryFixture{p: p}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if _, e := b.pool.Exec(ctx, `DELETE FROM admin_audit_events WHERE actor_account_id=ANY($1::uuid[])`, b.accounts); e != nil {
			t.Error("owned admin audit cleanup", e)
		}
	})
	var e error
	f.org, e = b.store.CreateOrganization(b.ctx, b.person.ID, organization.CreateInput{OrganizationType: "club", Name: "合成Org Memory，不是核验CSSA"})
	if e != nil {
		t.Fatal(e)
	}
	b.accounts = append(b.accounts, f.org.AccountID)
	f.access = agentorganizationmemory.Access{SessionDigest: p.owner.SessionDigest, ActingPersonID: b.person.ID, OrganizationID: f.org.ID}
	return f
}
func orgMemoryTestInput(category agentmemory.OrganizationCategory, key string) agentorganizationmemory.PutInput {
	return agentorganizationmemory.PutInput{ExpectedVersion: 0, Category: category, Key: key, Summary: "合成组织管理员人工声明，不是核验事实", StructuredValue: json.RawMessage(`{"note":"独立人工说明","tags":["测试"]}`), Visibility: agentmemory.VisibilityPrivate, ValidUntil: time.Now().UTC().Add(2 * time.Hour).Truncate(time.Microsecond)}
}
func orgMemoryAuditCount(t *testing.T, f *orgMemoryFixture) int {
	t.Helper()
	var count int
	if e := f.p.base.pool.QueryRow(f.p.base.ctx, `SELECT count(*) FROM admin_audit_events WHERE organization_id=$1 AND resource_type='organization_memory'`, f.org.ID).Scan(&count); e != nil {
		t.Fatal(e)
	}
	return count
}
func TestOrganizationMemoryNativeEightKindsRoundtripRestartRetryAndTombstone(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	beforePrivate := seedEffects(t, f.p)
	list, e := b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil || len(list) != 0 {
		t.Fatal(list, e)
	}
	for index, category := range agentmemory.OrganizationCategories() {
		t.Run(string(category), func(t *testing.T) {
			id := agentMemoryID(t, f.p)
			in := orgMemoryTestInput(category, fmt.Sprintf("note-%d", index))
			first, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
			if e != nil || first.Memory.OwnerID != f.org.AccountID || first.Memory.OwnerType != "ORGANIZATION" || first.Category != category || first.Memory.Version != 1 || first.StatementOrigin != agentorganizationmemory.Origin {
				t.Fatal(first, e)
			}
			audit := orgMemoryAuditCount(t, f)
			retry, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
			if e != nil || !reflect.DeepEqual(retry, first) || orgMemoryAuditCount(t, f) != audit {
				t.Fatal("exact retry changed row/audit", e)
			}
			cfg := b.pool.Config().Copy()
			pool, e := pgxpool.NewWithConfig(b.ctx, cfg)
			if e != nil {
				t.Fatal(e)
			}
			defer pool.Close()
			restarted := New(pool, false)
			list, e := restarted.ListOrganizationMemories(b.ctx, f.access)
			if e != nil {
				t.Fatal(e)
			}
			found := false
			for _, v := range list {
				if v.Memory.ID == id {
					found = reflect.DeepEqual(v, first)
				}
			}
			if !found {
				t.Fatal("reconnected native Store lost Org row")
			}
			in.ExpectedVersion = 1
			in.Summary = "合成明确修改"
			second, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in)
			if e != nil || second.Memory.Version != 2 {
				t.Fatal(second, e)
			}
			if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, orgMemoryTestInput(category, fmt.Sprintf("note-%d", index))); !errors.Is(e, agentmemory.ErrConflict) {
				t.Fatal("stale CAS accepted", e)
			}
			deleted, e := b.store.DeleteOrganizationMemory(b.ctx, f.access, id, 2)
			if e != nil || deleted.Memory.Status != agentmemory.StatusDeleted || deleted.Memory.Version != 3 || deleted.Memory.Summary != "" || string(deleted.Memory.StructuredValue) != "{}" {
				t.Fatal(deleted, e)
			}
			audit = orgMemoryAuditCount(t, f)
			again, e := b.store.DeleteOrganizationMemory(b.ctx, f.access, id, 2)
			if e != nil || !reflect.DeepEqual(again, deleted) || orgMemoryAuditCount(t, f) != audit {
				t.Fatal("delete retry not stable", e)
			}
			in.ExpectedVersion = 3
			if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in); !errors.Is(e, agentmemory.ErrConflict) {
				t.Fatal("tombstone restored", e)
			}
		})
	}
	if beforePrivate != seedEffects(t, f.p) {
		t.Fatal("Org writes changed Personal profile/seed/audit")
	}
	list, e = b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil || len(list) != 0 {
		t.Fatal("deleted rows listed", list, e)
	}
	if orgMemoryAuditCount(t, f) != 24 {
		t.Fatal("eight create/update/delete audit events missing or duplicated")
	}
}
func TestOrganizationMemoryNativeRealMembershipRolesAndCrossOwner(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	peer := agentorganizationmemory.Access{SessionDigest: f.p.peer.SessionDigest, ActingPersonID: b.other.ID, OrganizationID: f.org.ID}
	id := agentMemoryID(t, f.p)
	first, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, orgMemoryTestInput(agentmemory.OrgPartner, "declared-partner"))
	if e != nil {
		t.Fatal(e)
	}
	deny := func() {
		t.Helper()
		if v, e := b.store.ListOrganizationMemories(b.ctx, peer); !errors.Is(e, agentmemory.ErrForbidden) || v != nil {
			t.Fatal("non-admin read leaked", v, e)
		}
		if v, e := b.store.PutOrganizationMemory(b.ctx, peer, agentMemoryID(t, f.p), orgMemoryTestInput(agentmemory.OrgFAQ, "deny")); !errors.Is(e, agentmemory.ErrForbidden) || v.Memory.ID != "" {
			t.Fatal("non-admin write", e)
		}
	}
	deny()
	inv, e := b.store.InviteMember(b.ctx, b.person.ID, f.org.ID, b.other.ID, "member")
	if e != nil {
		t.Fatal(e)
	}
	deny()
	if _, e = b.store.AcceptInvitation(b.ctx, b.other.ID, inv.ID); e != nil {
		t.Fatal(e)
	}
	deny()
	if _, e = b.store.ChangeMemberRole(b.ctx, b.person.ID, f.org.ID, inv.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	list, e := b.store.ListOrganizationMemories(b.ctx, peer)
	if e != nil || len(list) != 1 || !reflect.DeepEqual(first, list[0]) {
		t.Fatal("real active admin not allowed", list, e)
	}
	if _, e = b.store.ChangeMemberRole(b.ctx, b.person.ID, f.org.ID, inv.ID, "moderator"); e != nil {
		t.Fatal(e)
	}
	deny()
	if _, e = b.store.ChangeMemberRole(b.ctx, b.person.ID, f.org.ID, inv.ID, "admin"); e != nil {
		t.Fatal(e)
	}
	if e = b.store.RevokeMember(b.ctx, b.person.ID, f.org.ID, inv.ID); e != nil {
		t.Fatal(e)
	}
	deny()
	for _, a := range []agentorganizationmemory.Access{{SessionDigest: f.p.org.SessionDigest, ActingPersonID: b.org.ID, OrganizationID: f.org.ID}, {SessionDigest: f.p.biz.SessionDigest, ActingPersonID: b.business.ID, OrganizationID: f.org.ID}, {SessionDigest: f.p.peer.SessionDigest, ActingPersonID: b.person.ID, OrganizationID: f.org.ID}} {
		if _, e = b.store.ListOrganizationMemories(b.ctx, a); !errors.Is(e, agentmemory.ErrForbidden) {
			t.Fatal("foreign token/actor allowed", e)
		}
	}
	personal := mustPutAgentMemory(t, f.p, agentMemoryID(t, f.p), agentMemoryInput("private-no-org-copy"))
	if _, e = b.store.DeleteOrganizationMemory(b.ctx, f.access, personal.ID, personal.Version); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("Org touched Person row", e)
	}
	second, e := b.store.CreateOrganization(b.ctx, b.person.ID, organization.CreateInput{OrganizationType: "club", Name: "合成第二独立组织"})
	if e != nil {
		t.Fatal(e)
	}
	b.accounts = append(b.accounts, second.AccountID)
	other := f.access
	other.OrganizationID = second.ID
	if _, e = b.store.DeleteOrganizationMemory(b.ctx, other, id, 1); !errors.Is(e, agentmemory.ErrNotFound) {
		t.Fatal("crossOrg delete", e)
	}
	if _, e = b.store.PutOrganizationMemory(b.ctx, other, id, orgMemoryTestInput(agentmemory.OrgPartner, "declared-partner")); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("crossOrg UUID overwrite", e)
	}
	own, e := b.store.ReadOwnMemories(b.ctx, f.p.owner)
	if e != nil || len(own) != 1 || own[0].ID != personal.ID {
		t.Fatal("Personal reader widened", e)
	}
}
func TestOrganizationMemoryNativeEightWayCASAndNamespace(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	id := agentMemoryID(t, f.p)
	in := orgMemoryTestInput(agentmemory.OrgAnnouncement, "cas")
	if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
		t.Fatal(e)
	}
	ready := make(chan struct{})
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-ready
			p := in
			p.ExpectedVersion = 1
			p.Summary = fmt.Sprintf("合成独立版本 %d", i)
			_, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, p)
			results <- e
		}(i)
	}
	close(ready)
	wg.Wait()
	close(results)
	win, conflict := 0, 0
	for e := range results {
		if e == nil {
			win++
		} else if errors.Is(e, agentmemory.ErrConflict) {
			conflict++
		} else {
			t.Fatal(e)
		}
	}
	if win != 1 || conflict != 7 {
		t.Fatal(win, conflict)
	}
	list, e := b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil || len(list) != 1 || list[0].Memory.Version != 2 || orgMemoryAuditCount(t, f) != 2 {
		t.Fatal("CAS native revision/audit", e)
	}
	in.ExpectedVersion = 2
	in.Category = agentmemory.OrgFAQ
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("category rebound", e)
	}
	in.Category = agentmemory.OrgAnnouncement
	in.Key = "different"
	if _, e = b.store.PutOrganizationMemory(b.ctx, f.access, id, in); !errors.Is(e, agentmemory.ErrConflict) {
		t.Fatal("key rebound", e)
	}
}
func TestOrganizationMemoryNativePublicKnowledgeAndPrivateSourcesRemainSeparate(t *testing.T) {
	f := newOrgMemoryFixture(t)
	b := f.p.base
	private := savePrivateCanaries(t, f.p)
	personal := mustPutAgentMemory(t, f.p, agentMemoryID(t, f.p), agentMemoryInput("never-copy-personal"))
	// The fixture now owns a second Org. The old all-owner Memory helper would
	// count this operation's intended new Org row as a Personal side effect.
	var personalBefore string
	if e := b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1 AND owner_type='PERSON'`, personal.ID).Scan(&personalBefore); e != nil {
		t.Fatal(e)
	}
	before := agentMemoryOwnedSourceSnapshot(t, f.p) + purposeOwnedControlRows(t, f.p)
	id := agentMemoryID(t, f.p)
	in := orgMemoryTestInput(agentmemory.OrgFAQ, "internal")
	in.Summary = "只用于人工组织管理的合成内部秘密"
	if _, e := b.store.PutOrganizationMemory(b.ctx, f.access, id, in); e != nil {
		t.Fatal(e)
	}
	list, e := b.store.ListOrganizationMemories(b.ctx, f.access)
	if e != nil {
		t.Fatal(e)
	}
	raw, _ := json.Marshal(list)
	for _, secret := range []string{private.Fields.AgentNotes, personal.Summary} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("copied personal source")
		}
	}
	var personalAfter string
	if e = b.pool.QueryRow(b.ctx, `SELECT to_jsonb(m)::text FROM agent_memories m WHERE id=$1 AND owner_type='PERSON'`, personal.ID).Scan(&personalAfter); e != nil {
		t.Fatal(e)
	}
	if personalBefore != personalAfter || before != agentMemoryOwnedSourceSnapshot(t, f.p)+purposeOwnedControlRows(t, f.p) {
		t.Fatal("Org create altered Personal source/control")
	}
	b.exec(`UPDATE organizations SET visibility='public',verification_status='verified' WHERE id=$1`, f.org.ID)
	answer, e := b.store.AnswerOrganization(b.ctx, f.org.ID, b.person.ID, in.Summary)
	if e != nil || answer.Status != "unknown" || strings.Contains(answer.Answer, in.Summary) {
		t.Fatal("public Org pack used private Memory", answer, e)
	}
}
