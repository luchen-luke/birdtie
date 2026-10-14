package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	org "github.com/birdtie/birdtie/apps/api/internal/organization"
	"testing"
	"time"
)

func orgHumanNativePerson(t *testing.T, f *savedPageNativeFixture) (org.HumanAccess, string) {
	t.Helper()
	id := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, id)
	_, digest, e := identity.NewToken()
	if e != nil {
		t.Fatal(e)
	}
	se := f.id(t)
	f.exec(t, `INSERT INTO sessions(id,account_id,token_sha256,authentication_method,expires_at,idle_expires_at)VALUES($1,$2,$3,'test',clock_timestamp()+interval '1 hour',clock_timestamp()+interval '30 minutes')`, se, id, digest[:])
	return org.HumanAccess{AccountID: id, AccountType: "person", SessionDigest: digest}, se
}
func orgHumanNative(t *testing.T) (*savedPageNativeFixture, string, org.HumanAccess) {
	t.Helper()
	f := savedPageNative(t)
	account := f.id(t)
	id := f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'organization')`, account)
	f.exec(t, `INSERT INTO organizations(id,account_id,organization_type,name,visibility,verification_status)VALUES($1,$2,'club','合成组织控制139','public','verified')`, id, account)
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, id, f.actor.ID)
	return f, id, org.HumanAccess{AccountID: f.actor.ID, AccountType: "person", SessionDigest: f.digest}
}
func orgHumanNativeVersion(t *testing.T, f *savedPageNativeFixture, id string) int64 {
	t.Helper()
	var v int64
	if e := f.pool.QueryRow(f.ctx, `SELECT version FROM organization_human_authority WHERE organization_id=$1`, id).Scan(&v); e != nil {
		t.Fatal(e)
	}
	return v
}
func orgHumanNativeWrite(t *testing.T, f *savedPageNativeFixture, a org.HumanAccess, id string, in org.ControlInput) org.ControlReceipt {
	t.Helper()
	p, e := f.store.PreviewOrganizationControl(f.ctx, a, id, in)
	if e != nil {
		t.Fatal("concrete preview", in.Action, e)
	}
	r, e := f.store.ApplyOrganizationControl(f.ctx, a, id, in, p.Approval)
	if e != nil {
		t.Fatal("concrete apply", in.Action, e)
	}
	return r
}
func orgHumanNativeWait(t *testing.T, f *savedPageNativeFixture, id string, a org.HumanAccess, session string, in org.ControlInput) {
	t.Helper()
	p, e := f.store.PreviewOrganizationControl(f.ctx, a, id, in)
	if e != nil {
		t.Fatal(e)
	}
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	if _, e = lock.Exec(f.ctx, `SELECT id FROM organizations WHERE id=$1 FOR UPDATE`, id); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.store.ApplyOrganizationControl(f.ctx, a, id, in, p.Approval); done <- e }()
	waited := false
	end := time.Now().Add(4 * time.Second)
	for time.Now().Before(end) {
		var n int
		e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND wait_event_type='Lock' AND query LIKE '%FROM organizations WHERE id=$1 FOR UPDATE%'`).Scan(&n)
		if e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			waited = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !waited {
		t.Fatal("actual original resource wait absent")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, session)
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, org.ErrControlChanged) {
			t.Fatal("original Session generation ABA committed", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("late writer did not retire")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM organization_human_operations WHERE operation_id=$1`, in.OperationID).Scan(&n); e != nil || n != 0 {
		t.Fatal("retired writer left a receipt", e)
	}
}
func TestOrganizationHumanMembersNativeCASOwnersSessionAndOriginalOperation(t *testing.T) {
	f, id, owner := orgHumanNative(t)
	target, _ := orgHumanNativePerson(t, f)
	other, se := orgHumanNativePerson(t, f)
	roster, e := f.store.ReadMembersControl(f.ctx, owner, id)
	if e != nil || len(roster.Members) != 1 || roster.Members[0].DisplayName != "" {
		t.Fatal("human-only manager / honest no-name fallback", e)
	}
	in := org.ControlInput{Action: "INVITE", OperationID: f.id(t), ExpectedAuthorityVersion: roster.AuthorityVersion, UserAccountID: target.AccountID, Role: "member"}
	r := orgHumanNativeWrite(t, f, owner, id, in)
	replay, e := f.store.ApplyOrganizationControl(f.ctx, owner, id, in, "lost expired token")
	if e != nil || replay.MemberVersion != r.MemberVersion {
		t.Fatal("original operation replay", e)
	}
	var audits int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_audit_events WHERE resource_id=$1 AND action='membership_invite'`, r.MembershipID).Scan(&audits); e != nil || audits != 1 {
		t.Fatal("replay repeated audit/notification action", e)
	}
	accept := org.ControlInput{Action: "ACCEPT", OperationID: f.id(t), ExpectedAuthorityVersion: r.AuthorityVersion, MembershipID: r.MembershipID, ExpectedMemberVersion: r.MemberVersion}
	stalePreview, e := f.store.PreviewOrganizationControl(f.ctx, target, id, accept)
	if e != nil {
		t.Fatal(e)
	}
	revoke := orgHumanNativeWrite(t, f, owner, id, org.ControlInput{Action: "REVOKE", OperationID: f.id(t), ExpectedAuthorityVersion: r.AuthorityVersion, MembershipID: r.MembershipID, ExpectedMemberVersion: r.MemberVersion})
	fresh := orgHumanNativeWrite(t, f, owner, id, org.ControlInput{Action: "INVITE", OperationID: f.id(t), ExpectedAuthorityVersion: revoke.AuthorityVersion, UserAccountID: target.AccountID, Role: "member", ExpectedMemberVersion: revoke.MemberVersion})
	if fresh.MembershipID != r.MembershipID || fresh.MemberVersion <= r.MemberVersion {
		t.Fatal("original stable ID re-invite version absent")
	}
	if _, e = f.store.ApplyOrganizationControl(f.ctx, target, id, accept, stalePreview.Approval); !errors.Is(e, org.ErrControlChanged) {
		t.Fatal("old invited generation accepted", e)
	}
	accepted := orgHumanNativeWrite(t, f, target, id, org.ControlInput{Action: "ACCEPT", OperationID: f.id(t), ExpectedAuthorityVersion: fresh.AuthorityVersion, MembershipID: fresh.MembershipID, ExpectedMemberVersion: fresh.MemberVersion})
	promote := orgHumanNativeWrite(t, f, owner, id, org.ControlInput{Action: "ROLE", OperationID: f.id(t), ExpectedAuthorityVersion: accepted.AuthorityVersion, MembershipID: accepted.MembershipID, ExpectedMemberVersion: accepted.MemberVersion, Role: "owner"})
	f.exec(t, `UPDATE accounts SET status='suspended' WHERE id=$1`, target.AccountID)
	removeInactive := org.ControlInput{Action: "REVOKE", OperationID: f.id(t), ExpectedAuthorityVersion: promote.AuthorityVersion, MembershipID: promote.MembershipID, ExpectedMemberVersion: promote.MemberVersion}
	if _, e = f.store.PreviewOrganizationControl(f.ctx, owner, id, removeInactive); e != nil {
		t.Fatal("inactive nominal owner cannot be removed by current owner", e)
	}
	self := roster.Members[0]
	demote := org.ControlInput{Action: "ROLE", OperationID: f.id(t), ExpectedAuthorityVersion: promote.AuthorityVersion, MembershipID: self.ID, ExpectedMemberVersion: self.Version, Role: "member"}
	if _, e = f.store.PreviewOrganizationControl(f.ctx, owner, id, demote); !errors.Is(e, org.ErrConflict) {
		t.Fatal("inactive nominal owner allowed final actual owner removal", e)
	}
	f.exec(t, `UPDATE accounts SET status='active' WHERE id=$1`, target.AccountID)
	lost := orgHumanNativeWrite(t, f, owner, id, demote)
	original, e := f.store.ReadOrganizationControlOperation(f.ctx, owner, id, lost.OperationID)
	if e != nil || original.Status != "SUCCEEDED" || original.AuthorityVersion != lost.AuthorityVersion {
		t.Fatal("lost manager original receipt hidden", e)
	}
	stoppedOp := f.id(t)
	stopped, e := f.store.StopOrganizationControlOperation(f.ctx, owner, id, stoppedOp, lost.AuthorityVersion)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal("lost manager own operation cannot close", e)
	}
	late := org.ControlInput{Action: "INVITE", OperationID: stoppedOp, ExpectedAuthorityVersion: lost.AuthorityVersion, UserAccountID: other.AccountID, Role: "member"}
	if _, e = f.store.ApplyOrganizationControl(f.ctx, owner, id, late, "old delayed approval"); !errors.Is(e, org.ErrControlChanged) {
		t.Fatal("stopped late writer", e)
	}
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, id, other.AccountID)
	waiting := org.ControlInput{Action: "INVITE", OperationID: f.id(t), ExpectedAuthorityVersion: orgHumanNativeVersion(t, f, id), UserAccountID: f.id(t), Role: "member"}
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'person')`, waiting.UserAccountID)
	orgHumanNativeWait(t, f, id, other, se, waiting)
}

func TestOrganizationHumanApprovalOriginalInputSourceAndDeadline(t *testing.T) {
	var d [32]byte
	d[0] = 19
	a := org.HumanAccess{AccountID: "11111111-1111-4111-8111-111111111111", AccountType: "person", SessionDigest: d}
	at := time.Now()
	material := "actor|org|source-xmin|session-id-generation|exact-body"
	token := orgApproval(a, material, at.UnixNano(), at.Add(time.Second).UnixNano())
	if !orgApprovalValid(a, material, token, at.Add(time.Millisecond)) {
		t.Fatal("specific current approval rejected")
	}
	for name, change := range map[string]string{"source": "source-other", "input": "body-other", "actor": "actor-other"} {
		t.Run(name, func(t *testing.T) {
			if orgApprovalValid(a, material+change, token, at) {
				t.Fatal("changed concrete approval accepted")
			}
		})
	}
	if orgApprovalValid(a, material, token, at.Add(time.Second)) || orgApprovalValid(a, material, token, at.Add(-time.Nanosecond)) {
		t.Fatal("expired/future approval accepted")
	}
}
