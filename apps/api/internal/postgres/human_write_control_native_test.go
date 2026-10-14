package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/contextgraph"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/organization"
	"github.com/birdtie/birdtie/apps/api/internal/safety"
	"testing"
	"time"
)

func TestHumanWriteControlApprovalOriginalFrameSourceAndDeadline(t *testing.T) {
	f := humanWriteFrame{a: identity.Actor{ID: "11111111-1111-1111-1111-111111111111", AccountType: "person"}, family: "REPORT", op: "22222222-2222-2222-2222-222222222222", session: identity.HumanControlFrame{ID: "33333333-3333-3333-3333-333333333333", Generation: 1}}
	now := time.Now()
	material := humanWriteMaterial(f, "", "SUBMIT", "body", "source")
	token, e := humanWriteApproval(material, now.UnixNano(), now.Add(30*time.Second).UnixNano())
	if e != nil {
		t.Fatal(e)
	}
	if _, ok := humanWriteApprovalDeadline(material, token, now.Add(time.Second)); !ok {
		t.Fatal("original rejected")
	}
	for n, m := range map[string]string{"newSource": humanWriteMaterial(f, "", "SUBMIT", "body", "source2"), "newBody": humanWriteMaterial(f, "", "SUBMIT", "body2", "source"), "newScope": humanWriteMaterial(f, "other", "SUBMIT", "body", "source")} {
		t.Run(n, func(t *testing.T) {
			if _, ok := humanWriteApprovalDeadline(m, token, now.Add(time.Second)); ok {
				t.Fatal("approval rebound")
			}
		})
	}
	f.session.Generation++
	if _, ok := humanWriteApprovalDeadline(humanWriteMaterial(f, "", "SUBMIT", "body", "source"), token, now.Add(time.Second)); ok {
		t.Fatal("Session ABA rebound")
	}
	if _, ok := humanWriteApprovalDeadline(material, token, now.Add(30*time.Second)); ok {
		t.Fatal("expired approval accepted")
	}
}
func hcNativeFAQ(t *testing.T, f *savedPageNativeFixture, orgID string, in organization.FAQControlInput) identity.HumanControlReceipt {
	t.Helper()
	p, e := f.store.PreviewFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil {
		t.Fatal("FAQ preview", e)
	}
	in.Approval = p.Approval
	r, e := f.store.CommitFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil {
		t.Fatal("FAQ commit", e)
	}
	return r
}
func hcNativeContext(t *testing.T, f *savedPageNativeFixture, in contextgraph.DeclarationControlInput) identity.HumanControlReceipt {
	t.Helper()
	p, e := f.store.PreviewDeclarationControl(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("Context preview", e)
	}
	in.Approval = p.Approval
	r, e := f.store.CommitDeclarationControl(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("Context commit", e)
	}
	return r
}
func hcNativeBlock(t *testing.T, f *savedPageNativeFixture, in identity.BlockControlInput) identity.HumanControlReceipt {
	t.Helper()
	p, e := f.store.PreviewBlockControl(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("Block preview", e)
	}
	in.Approval = p.Approval
	r, e := f.store.CommitBlockControl(f.ctx, f.digest, f.actor, in)
	if e != nil {
		t.Fatal("Block commit", e)
	}
	return r
}
func TestHumanWriteControlNativeFAQContextCASAndOwnHistory(t *testing.T) {
	f, orgID, _ := orgHumanNative(t)
	published := false
	in := organization.FAQControlInput{OperationID: f.id(t), Action: "CREATE", ExpectedVersion: 0, Question: "活动何时开始", Answer: "上午九点", Published: &published}
	p, e := f.store.PreviewFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = p.Approval
	r, e := f.store.CommitFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil || r.AppliedVersion != 1 || r.ResultID == in.OperationID || r.TargetID != r.ResultID {
		t.Fatal("actual FAQ create", r, e)
	}
	again, e := f.store.CommitFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil || humanWriteHashJSON(again) != humanWriteHashJSON(r) {
		t.Fatal("once replay", e)
	}
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_audit_events WHERE resource_id=$1 AND action='faq_create'`, r.ResultID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatal("FAQ audit repeated", n)
	}
	update := organization.FAQControlInput{OperationID: f.id(t), Action: "UPDATE", FAQID: r.ResultID, ExpectedVersion: 1, Question: in.Question, Answer: "上午十点", Published: &published}
	updated := hcNativeFAQ(t, f, orgID, update)
	if updated.AppliedVersion != 2 {
		t.Fatal("specific FAQ revision")
	}
	if _, e = f.store.PreviewFAQControl(f.ctx, f.digest, f.actor, orgID, update); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("stale FAQ CAS", e)
	}
	f.exec(t, `UPDATE organization_memberships SET role='member' WHERE organization_id=$1 AND user_account_id=$2`, orgID, f.actor.ID)
	history, e := f.store.ReadFAQControlOperation(f.ctx, f.digest, f.actor, orgID, in.OperationID)
	if e != nil || history.ResultID != r.ResultID {
		t.Fatal("lost manager own original receipt", e)
	}
	if _, e = f.store.ReadFAQControl(f.ctx, f.digest, f.actor, orgID); !errors.Is(e, organization.ErrForbidden) {
		t.Fatal("lost manager new read", e)
	}
	stopID := f.id(t)
	stopped, e := f.store.StopFAQControlOperation(f.ctx, f.digest, f.actor, orgID, stopID)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal("lost manager unknown close", e)
	}
	late := in
	late.OperationID = stopID
	late.Answer = "迟到写入不允许"
	lateReceipt, e := f.store.CommitFAQControl(f.ctx, f.digest, f.actor, orgID, late)
	if e != nil || lateReceipt.Status != "STOPPED" {
		t.Fatal("stopped late FAQ wrote", e)
	}
	foreign, _ := orgHumanNativePerson(t, f)
	if _, e = f.store.ReadFAQControlOperation(f.ctx, foreign.SessionDigest, identity.Actor{ID: foreign.AccountID, AccountType: "person"}, orgID, in.OperationID); !errors.Is(e, identity.ErrNotFound) {
		t.Fatal("cross actor history", e)
	}
	d := contextgraph.DeclarationControlInput{OperationID: f.id(t), Action: "DECLARE", Type: contextgraph.Institution, SourceKey: "合成个人Context142", Relation: "affiliation"}
	declared := hcNativeContext(t, f, d)
	if declared.AppliedVersion != 1 || declared.TargetID != declared.ResultID {
		t.Fatal("Context actual source", declared)
	}
	duplicate := d
	duplicate.OperationID = f.id(t)
	duplicate.ExpectedVersion = 1
	if _, e = f.store.PreviewDeclarationControl(f.ctx, f.digest, f.actor, duplicate); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("duplicate no fake increment", e)
	}
	removed := hcNativeContext(t, f, contextgraph.DeclarationControlInput{OperationID: f.id(t), Action: "REMOVE", ContextID: declared.ResultID, Relation: "affiliation", ExpectedVersion: 1})
	if removed.AppliedVersion != 2 {
		t.Fatal("Context remove version")
	}
	duplicate.ExpectedVersion = 2
	restored := hcNativeContext(t, f, duplicate)
	if restored.ResultID != declared.ResultID || restored.AppliedVersion != 3 {
		t.Fatal("Context original ID ABA", restored)
	}
	stale := contextgraph.DeclarationControlInput{OperationID: f.id(t), Action: "REMOVE", ContextID: declared.ResultID, Relation: "affiliation", ExpectedVersion: 1}
	if _, e = f.store.PreviewDeclarationControl(f.ctx, f.digest, f.actor, stale); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("Context ABA reused", e)
	}
	if _, e = f.pool.Exec(f.ctx, `DELETE FROM human_write_control_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.actor.ID, in.OperationID); e == nil {
		t.Fatal("immutable history deleted")
	}
	var agentCount int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM agents WHERE principal_account_id=$1`, f.actor.ID).Scan(&agentCount); e != nil {
		t.Fatal(e)
	}
	if agentCount != 0 {
		t.Fatal("ordinary human needed Agent")
	}
}
func TestHumanWriteControlNativeReportGrantBlockOnceAndTombstones(t *testing.T) {
	f := savedPageNative(t)
	peerForReport, _ := orgHumanNativePerson(t, f)
	requestID, conversationID, messageID := f.id(t), f.id(t), f.id(t)
	f.exec(t, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,city_id,note,state,expires_at,decided_at)VALUES($1,$2,$3,'aberdeen-gb','合成既有会话','accepted',clock_timestamp()+interval '1 hour',clock_timestamp())`, requestID, peerForReport.AccountID, f.actor.ID)
	f.exec(t, `INSERT INTO conversations(id,request_id,member_a_account_id,member_b_account_id)VALUES($1,$2,$3,$4)`, conversationID, requestID, peerForReport.AccountID, f.actor.ID)
	f.exec(t, `INSERT INTO conversation_messages(id,conversation_id,sender_account_id,body)VALUES($1,$2,$3,'合成受权消息')`, messageID, conversationID, peerForReport.AccountID)
	subject := safety.ReportControlInput{OperationID: f.id(t), Action: "SUBMIT", ReportInput: safety.ReportInput{TargetType: "message", TargetID: messageID, Reason: "harassment", Details: "合成消息报告不得复用屏蔽变化前的批准"}}
	original, e := f.store.PreviewReportControl(f.ctx, f.digest, f.actor, subject)
	if e != nil {
		t.Fatal("actual message preview", e)
	}
	hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "BLOCK", AccountID: peerForReport.AccountID})
	hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "UNBLOCK", AccountID: peerForReport.AccountID, ExpectedVersion: 1})
	subject.Approval = original.Approval
	if _, e = f.store.CommitReportControl(f.ctx, f.digest, f.actor, subject); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("report original block-pair ABA accepted", e)
	}
	var noReport int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM incident_reports WHERE reporter_account_id=$1`, f.actor.ID).Scan(&noReport); e != nil || noReport != 0 {
		t.Fatal("retired subject used quota", noReport, e)
	}
	hcNativeReportBlockConcurrency(t, f, peerForReport.AccountID, messageID)
	var first identity.HumanControlReceipt
	for i := 0; i < 5; i++ {
		in := safety.ReportControlInput{OperationID: f.id(t), Action: "SUBMIT", ReportInput: safety.ReportInput{TargetType: "general", Reason: "technical", Details: "合成故障说明只用于隔离测试数据库"}}
		p, e := f.store.PreviewReportControl(f.ctx, f.digest, f.actor, in)
		if e != nil {
			t.Fatal("Report preview", e)
		}
		in.Approval = p.Approval
		r, e := f.store.CommitReportControl(f.ctx, f.digest, f.actor, in)
		if e != nil || r.AppliedVersion != 0 || r.ResultID == "" {
			t.Fatal("Report append", r, e)
		}
		replay, e := f.store.CommitReportControl(f.ctx, f.digest, f.actor, in)
		if e != nil || replay.ResultID != r.ResultID {
			t.Fatal("Report original replay", e)
		}
		if i == 0 {
			first = r
		}
	}
	state, e := f.store.ReadReportControl(f.ctx, f.digest, f.actor)
	if e != nil || state.RemainingQuota != 0 || state.CanSubmit {
		t.Fatal("once quota", state, e)
	}
	var n int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM incident_reports WHERE reporter_account_id=$1`, f.actor.ID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 5 {
		t.Fatal("Report duplicate effects", n)
	}
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND resource_id=$2 AND action='submit'`, f.actor.ID, first.ResultID).Scan(&n); e != nil {
		t.Fatal(e)
	}
	if n != 1 {
		t.Fatal("Report audit once", n)
	}
	op := f.id(t)
	_, e = f.store.StopReportControlOperation(f.ctx, f.digest, f.actor, op)
	if e != nil {
		t.Fatal(e)
	}
	late := safety.ReportControlInput{OperationID: op, Action: "SUBMIT", Approval: "old-approval", ReportInput: safety.ReportInput{TargetType: "general", Reason: "technical", Details: "合成停止后迟到的请求不得扣除额度"}}
	if r, e := f.store.CommitReportControl(f.ctx, f.digest, f.actor, late); e != nil || r.Status != "STOPPED" {
		t.Fatal("stopped Report reused quota", e)
	}
	peer, _ := orgHumanNativePerson(t, f)
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成本人授权资料142','private')`, f.actor.ID)
	grant, e := f.store.GrantProfileRead(f.ctx, f.actor.ID, peer.AccountID, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	gi := identity.GrantRevokeControlInput{OperationID: f.id(t), Action: "REVOKE", GrantID: grant.ID, ExpectedVersion: grant.Revision}
	gp, e := f.store.PreviewGrantRevokeControl(f.ctx, f.digest, f.actor, gi)
	if e != nil {
		t.Fatal(e)
	}
	gi.Approval = gp.Approval
	gr, e := f.store.CommitGrantRevokeControl(f.ctx, f.digest, f.actor, gi)
	if e != nil || gr.AppliedVersion != grant.Revision+1 {
		t.Fatal("native revoke", gr, e)
	}
	if rr, e := f.store.CommitGrantRevokeControl(f.ctx, f.digest, f.actor, gi); e != nil || rr.TargetID != gr.TargetID {
		t.Fatal("Grant once", e)
	}
	bi := identity.BlockControlInput{OperationID: f.id(t), Action: "BLOCK", AccountID: peer.AccountID}
	br := hcNativeBlock(t, f, bi)
	if br.AppliedVersion != 1 {
		t.Fatal("first pair", br)
	}
	hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "UNBLOCK", AccountID: peer.AccountID, ExpectedVersion: 1})
	hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "BLOCK", AccountID: peer.AccountID, ExpectedVersion: 2})
	if _, e = f.store.PreviewBlockControl(f.ctx, f.digest, f.actor, bi); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("pair ABA stale", e)
	}
	f.exec(t, `UPDATE person_block_control_epochs SET version=$3 WHERE owner_account_id=$1 AND target_account_id=$2`, f.actor.ID, peer.AccountID, identity.HumanControlMaxVersion)
	terminal := hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "UNBLOCK", AccountID: peer.AccountID, ExpectedVersion: identity.HumanControlMaxVersion})
	if terminal.AppliedVersion != identity.HumanControlMaxVersion {
		t.Fatal("cleanup overflow")
	}
	expiredGrant, e := f.store.GrantProfileRead(f.ctx, f.actor.ID, peer.AccountID, time.Now().Add(time.Hour))
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE consent_grants SET revision=$2,expires_at=clock_timestamp()-interval '1 second' WHERE id=$1`, expiredGrant.ID, identity.HumanControlMaxVersion)
	gi = identity.GrantRevokeControlInput{OperationID: f.id(t), Action: "REVOKE", GrantID: expiredGrant.ID, ExpectedVersion: identity.HumanControlMaxVersion}
	gp, e = f.store.PreviewGrantRevokeControl(f.ctx, f.digest, f.actor, gi)
	if e != nil {
		t.Fatal("expired max grant cleanup preview", e)
	}
	gi.Approval = gp.Approval
	gr, e = f.store.CommitGrantRevokeControl(f.ctx, f.digest, f.actor, gi)
	if e != nil || gr.AppliedVersion != identity.HumanControlMaxVersion {
		t.Fatal("expired max grant cleanup", gr, e)
	}
}
func TestHumanWriteControlNativeRealWaitOriginalSessionABARollback(t *testing.T) {
	f, orgID, _ := orgHumanNative(t)
	published := false
	created := hcNativeFAQ(t, f, orgID, organization.FAQControlInput{OperationID: f.id(t), Action: "CREATE", Question: "活动何时开始", Answer: "上午九点", Published: &published})
	in := organization.FAQControlInput{OperationID: f.id(t), Action: "UPDATE", FAQID: created.ResultID, ExpectedVersion: 1, Question: "活动何时开始", Answer: "这次迟到写不得提交", Published: &published}
	p, e := f.store.PreviewFAQControl(f.ctx, f.digest, f.actor, orgID, in)
	if e != nil {
		t.Fatal(e)
	}
	in.Approval = p.Approval
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = lock.Exec(f.ctx, `SELECT id FROM organization_faqs WHERE id=$1 FOR UPDATE`, created.ResultID); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.store.CommitFAQControl(f.ctx, f.digest, f.actor, orgID, in); done <- e }()
	waited := false
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock'`, pid).Scan(&n); e != nil {
			t.Fatal(e)
		}
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
		t.Fatal("actual FAQ row wait absent")
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("original Session ABA accepted or disguised", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not resume")
	}
	var version int64
	var answer string
	if e := f.pool.QueryRow(f.ctx, `SELECT revision,answer FROM organization_faqs WHERE id=$1`, created.ResultID).Scan(&version, &answer); e != nil {
		t.Fatal(e)
	}
	if version != 1 || answer != "上午九点" {
		t.Fatal("late write effect committed")
	}
	var receipts, audits int
	if e := f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM human_write_control_operations WHERE actor_account_id=$1 AND operation_id=$2),(SELECT count(*) FROM admin_audit_events WHERE resource_id=$3 AND action='faq_update')`, f.actor.ID, in.OperationID, created.ResultID).Scan(&receipts, &audits); e != nil {
		t.Fatal(e)
	}
	if receipts != 0 || audits != 0 {
		t.Fatal("late original Session leaked receipt/audit", receipts, audits)
	}
	history, e := f.store.ReadFAQControlOperation(f.ctx, f.digest, f.actor, orgID, created.OperationID)
	if e != nil || history.Status != "SUCCEEDED" {
		t.Fatal("fresh Session cannot verify original fact", e)
	}
}

func hcNativeWaitPID(t *testing.T, f *savedPageNativeFixture, pid int) {
	t.Helper()
	end := time.Now().Add(5 * time.Second)
	for time.Now().Before(end) {
		var n int
		if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock'`, pid).Scan(&n); e != nil {
			t.Fatal(e)
		}
		if n > 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("actual native wait absent")
}
func TestHumanWriteControlNativeDeadlineAfterEffectSessionWait(t *testing.T) {
	f, orgID, _ := orgHumanNative(t)
	published := false
	created := hcNativeFAQ(t, f, orgID, organization.FAQControlInput{OperationID: f.id(t), Action: "CREATE", Question: "活动何时开始", Answer: "上午九点", Published: &published})
	in := organization.FAQControlInput{OperationID: f.id(t), Action: "UPDATE", FAQID: created.ResultID, ExpectedVersion: 1, Question: "活动何时开始", Answer: "等待过期后不得写入", Published: &published}
	frame, e := f.store.CaptureHumanControlSession(f.ctx, f.digest, f.actor)
	if e != nil {
		t.Fatal(e)
	}
	ctx := identity.WithHumanControlFrame(f.ctx, f.digest, f.actor, frame)
	ctx, sourceTx, e := f.store.humanWriteBegin(ctx, f.digest, f.actor, "FAQ", in.OperationID)
	if e != nil {
		t.Fatal(e)
	}
	_, source, e := faqControlSource(ctx, sourceTx, orgID, in, false)
	if e != nil {
		t.Fatal(e)
	}
	var now time.Time
	if e = sourceTx.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		t.Fatal(e)
	}
	in.Approval, e = humanWriteApproval(humanWriteMaterial(sourceTx, orgID, in.Action, organization.FAQControlDigest(orgID, in), source), now.UnixNano(), now.Add(time.Second).UnixNano())
	if e != nil {
		t.Fatal(e)
	}
	if e = sourceTx.tx.Rollback(context.Background()); e != nil {
		t.Fatal(e)
	}
	lock, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer lock.Rollback(context.Background())
	var pid int
	if e = lock.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = lock.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.store.CommitFAQControl(ctx, f.digest, f.actor, orgID, in); done <- e }()
	hcNativeWaitPID(t, f, pid)
	until := time.Until(now.Add(time.Second + 50*time.Millisecond))
	if until > 0 {
		time.Sleep(until)
	}
	if e = lock.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("expired final wait committed", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("expired writer did not finish")
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM organization_faqs WHERE id=$1 AND revision=1 AND answer='上午九点'`, created.ResultID).Scan(&n); e != nil || n != 1 {
		t.Fatal("expired effect persisted", n, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM human_write_control_operations WHERE operation_id=$1`, in.OperationID).Scan(&n); e != nil || n != 0 {
		t.Fatal("expired operation receipt persisted", n, e)
	}
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM admin_audit_events WHERE resource_id=$1 AND action='faq_update'`, created.ResultID).Scan(&n); e != nil || n != 0 {
		t.Fatal("expired audit persisted", n, e)
	}
}

func hcNativeReportBlockConcurrency(t *testing.T, f *savedPageNativeFixture, peer, message string) {
	t.Helper()
	subject := safety.ReportControlInput{OperationID: f.id(t), Action: "SUBMIT", ReportInput: safety.ReportInput{TargetType: "message", TargetID: message, Reason: "harassment", Details: "并发屏蔽与举报必须维持真实源顺序及零重复效果"}}
	preview, e := f.store.PreviewReportControl(f.ctx, f.digest, f.actor, subject)
	if e != nil {
		t.Fatal(e)
	}
	subject.Approval = preview.Approval
	block := identity.BlockControlInput{OperationID: f.id(t), Action: "BLOCK", AccountID: peer, ExpectedVersion: 2}
	bp, e := f.store.PreviewBlockControl(f.ctx, f.digest, f.actor, block)
	if e != nil {
		t.Fatal(e)
	}
	block.Approval = bp.Approval
	frame, e := f.store.CaptureHumanControlSession(f.ctx, f.digest, f.actor)
	if e != nil {
		t.Fatal(e)
	}
	ctx := identity.WithHumanControlFrame(f.ctx, f.digest, f.actor, frame)
	outer, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer outer.Rollback(context.Background())
	var outerPID int
	if e = outer.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&outerPID); e != nil {
		t.Fatal(e)
	}
	if _, e = outer.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR UPDATE`, peer); e != nil {
		t.Fatal(e)
	}
	blockDone := make(chan error, 1)
	go func() {
		r, e := f.store.CommitBlockControl(ctx, f.digest, f.actor, block)
		if e == nil && (r.Status != "SUCCEEDED" || r.AppliedVersion != 3) {
			e = errors.New("unexpected original block result")
		}
		blockDone <- e
	}()
	hcNativeWaitPID(t, f, outerPID)
	var blockPID int
	if e = f.pool.QueryRow(f.ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' LIMIT 1`, outerPID).Scan(&blockPID); e != nil {
		t.Fatal(e)
	}
	reportDone := make(chan error, 1)
	go func() { _, e := f.store.CommitReportControl(ctx, f.digest, f.actor, subject); reportDone <- e }()
	hcNativeWaitPID(t, f, blockPID)
	check, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = check.Exec(f.ctx, `SET LOCAL lock_timeout='250ms'`); e != nil {
		t.Fatal(e)
	}
	_, e = check.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR SHARE`, f.actor.ID)
	_ = check.Rollback(context.Background())
	if e != nil {
		t.Fatal("report held actor UPDATE while waiting for block pair", e)
	}
	if e = outer.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-blockDone:
		if e != nil {
			t.Fatal("canonical concurrent BLOCK failed", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("block deadlocked")
	}
	select {
	case e = <-reportDone:
		if !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("concurrent source retirement not exact", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("report deadlocked")
	}
	var reports, receipts int
	if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM incident_reports WHERE reporter_account_id=$1),(SELECT count(*) FROM human_write_control_operations WHERE actor_account_id=$1 AND family='REPORT' AND operation_id=$2)`, f.actor.ID, subject.OperationID).Scan(&reports, &receipts); e != nil || reports != 0 || receipts != 0 {
		t.Fatal("concurrent retired report leaked effect/receipt", reports, receipts, e)
	}
	hcNativeBlock(t, f, identity.BlockControlInput{OperationID: f.id(t), Action: "UNBLOCK", AccountID: peer, ExpectedVersion: 3})
}
