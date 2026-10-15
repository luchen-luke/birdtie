package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"strings"
	"testing"
	"time"
)

type share143Fixture struct {
	f                                           *savedPageNativeFixture
	peer, peerSession, place, tie, conversation string
	peerDigest                                  [32]byte
}

func share143Native(t *testing.T, withConversation bool) *share143Fixture {
	t.Helper()
	f := savedPageNative(t)
	other, se := orgHumanNativePerson(t, f)
	g := &share143Fixture{f: f, peer: other.AccountID, peerSession: se, peerDigest: other.SessionDigest}
	f.exec(t, `INSERT INTO user_profiles(account_id,display_name,visibility)VALUES($1,'合成发送者143','public'),($2,'合成接收者143','public')`, f.actor.ID, g.peer)
	_, g.place = f.bookmark(t)
	req := f.id(t)
	g.tie = f.id(t)
	f.exec(t, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at,decided_at)VALUES($1,$2,$3,NULL,'合成已接受好友许可','friend','accepted',clock_timestamp()+interval '1 hour',clock_timestamp())`, req, f.actor.ID, g.peer)
	f.exec(t, `INSERT INTO person_ties(id,person_a_account_id,person_b_account_id,request_id,status)VALUES($1,LEAST($2::uuid,$3::uuid),GREATEST($2::uuid,$3::uuid),$4,'active')`, g.tie, f.actor.ID, g.peer, req)
	if withConversation {
		g.conversation = f.id(t)
		f.exec(t, `INSERT INTO conversations(id,request_id,member_a_account_id,member_b_account_id)VALUES($1,$2,LEAST($3::uuid,$4::uuid),GREATEST($3::uuid,$4::uuid))`, g.conversation, req, f.actor.ID, g.peer)
	}
	return g
}
func (g *share143Fixture) access() connection.EntityShareAccess {
	return connection.EntityShareAccess{Actor: g.f.actor, SessionDigest: g.f.digest}
}
func share143Input(t *testing.T, g *share143Fixture, kind, id string) connection.EntityShareControlInput {
	return connection.EntityShareControlInput{OperationID: g.f.id(t), Entity: connection.EntityShareRef{Type: kind, ID: id}}
}
func share143Approve(t *testing.T, g *share143Fixture, target connection.EntityShareRecipient, in connection.EntityShareControlInput) connection.EntityShareControlInput {
	t.Helper()
	p, e := g.f.store.PreviewEntityShareControl(g.f.ctx, g.access(), target, in)
	if e != nil || p.RecipientAccountID != g.peer || p.Entity != in.Entity || p.SourceVersion == "" {
		t.Fatal("actual preview", p, e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(g.f.ctx); e != nil {
		t.Fatal("actual preview response proof", e)
	}
	in.SourceVersion = p.SourceVersion
	in.Approval = p.Approval
	return in
}
func share143Counts(t *testing.T, g *share143Fixture) (int, int, int, int) {
	t.Helper()
	var cv, msg, notices, history int
	if e := g.f.pool.QueryRow(g.f.ctx, `SELECT(SELECT count(*) FROM conversations WHERE member_a_account_id=$1 OR member_b_account_id=$1),(SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1),(SELECT count(*) FROM native_notification_decisions WHERE actor_id=$1 AND kind='direct_message'),(SELECT count(*) FROM human_entity_share_operations WHERE actor_account_id=$1)`, g.f.actor.ID).Scan(&cv, &msg, &notices, &history); e != nil {
		t.Fatal(e)
	}
	return cv, msg, notices, history
}

func TestEntityShareControlNativeCityMomentAndCurrentWithdrawal(t *testing.T) {
	g := share143Native(t, true)
	f := g.f
	moment := f.id(t)
	f.exec(t, `WITH n AS(SELECT clock_timestamp() at)INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,location_precision,status,visibility,author_confirmed_at,published_at)SELECT $1,$2,'aberdeen-gb',NULL,'合成城市动态分享','正文不进入分享回执','city','published','public',at,at FROM n`, moment, f.actor.ID)
	target := connection.EntityShareRecipient{Type: "CONVERSATION", ID: g.conversation}
	in := share143Approve(t, g, target, share143Input(t, g, "moment", moment))
	receipt, err := f.store.CommitEntityShareControl(f.ctx, g.access(), target, in)
	if err != nil || receipt.Status != "SUCCEEDED" || receipt.Entity == nil || receipt.Entity.ID != moment {
		t.Fatal("city Moment share", receipt, err)
	}
	original, err := f.store.ReadEntityShareControlOperation(f.ctx, g.access(), target, in.OperationID)
	if err != nil || humanWriteHashJSON(original) != humanWriteHashJSON(receipt) {
		t.Fatal("original city share fact", err)
	}
	messages, err := f.store.ListMessages(f.ctx, g.peer, g.conversation)
	if err != nil || len(messages) != 1 || messages[0].Entity == nil || !messages[0].Entity.Available || messages[0].Entity.ID != moment || messages[0].Entity.Title != "合成城市动态分享" {
		t.Fatal("recipient current city card", messages, err)
	}
	f.exec(t, `UPDATE moments SET status='withdrawn',visibility='private',revision=revision+1 WHERE id=$1`, moment)
	messages, err = f.store.ListMessages(f.ctx, g.peer, g.conversation)
	if err != nil || len(messages) != 1 || messages[0].Entity == nil || messages[0].Entity.Available || messages[0].Entity.ID != "" || messages[0].Entity.Title != "" {
		t.Fatal("withdrawn city card retained snapshot", messages, err)
	}
	if _, err = f.store.PreviewEntityShareControl(f.ctx, g.access(), target, share143Input(t, g, "moment", moment)); !errors.Is(err, connection.ErrNotFound) {
		t.Fatal("withdrawn city Moment shared again", err)
	}
	original, err = f.store.ReadEntityShareControlOperation(f.ctx, g.access(), target, in.OperationID)
	if err != nil || humanWriteHashJSON(original) != humanWriteHashJSON(receipt) {
		t.Fatal("withdrawal changed original share fact", err)
	}
	_, messageCount, _, historyCount := share143Counts(t, g)
	if messageCount != 1 || historyCount != 1 {
		t.Fatal("denial created another share", messageCount, historyCount)
	}
}

func TestEntityShareControlNativeSevenKindsAndPreviewNoConversation(t *testing.T) {
	g := share143Native(t, false)
	f := g.f
	target := connection.EntityShareRecipient{Type: "FRIEND_TIE", ID: g.tie}
	orgPrincipal, orgID, bizPrincipal, bizID, moment := f.id(t), f.id(t), f.id(t), f.id(t), f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'organization'),($2,'business')`, orgPrincipal, bizPrincipal)
	f.exec(t, `INSERT INTO organizations(id,account_id,organization_type,name,visibility,verification_status)VALUES($1,$2,'club','合成公开组织143','public','verified')`, orgID, orgPrincipal)
	f.exec(t, `INSERT INTO businesses(id,account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at)VALUES($1,$2,'合成公开商家143','verified','https://example.invalid/isolated143',$3,clock_timestamp())`, bizID, bizPrincipal, f.actor.ID)
	f.exec(t, `WITH n AS(SELECT clock_timestamp() at)INSERT INTO moments(id,author_account_id,city_id,place_id,title,body,location_precision,status,visibility,author_confirmed_at,published_at)SELECT $1,$2,'aberdeen-gb',$3,'合成公开Moment143','不复制此正文','place','published','public',at,at FROM n`, moment, f.actor.ID, g.place)
	sources := []connection.EntityShareRef{{Type: "activity", ID: "b1700000-0000-4000-8000-000000000017"}, {Type: "place", ID: g.place}, {Type: "person", ID: g.peer}, {Type: "community", ID: "b1700000-0000-4000-8000-000000000030"}, {Type: "organization", ID: orgID}, {Type: "business", ID: bizID}, {Type: "moment", ID: moment}}
	for _, ref := range sources {
		t.Run(ref.Type, func(t *testing.T) {
			in := share143Approve(t, g, target, share143Input(t, g, ref.Type, ref.ID))
			cv, msg, _, history := share143Counts(t, g)
			if msg == 0 && (cv != 0 || history != 0) {
				t.Fatal("preview created conversation/history")
			}
			r, e := f.store.CommitEntityShareControl(f.ctx, g.access(), target, in)
			if e != nil || !connection.ValidEntityShareControlReceipt(r, target, in.OperationID) || *r.Entity != ref {
				t.Fatal("exact native share", r, e)
			}
			raw, _ := json.Marshal(r)
			if string(raw) == "" || strings.Contains(string(raw), "不复制此正文") || strings.Contains(string(raw), "title") {
				t.Fatal("receipt copied source")
			}
			again, e := f.store.CommitEntityShareControl(f.ctx, g.access(), target, in)
			if e != nil || humanWriteHashJSON(again) != humanWriteHashJSON(r) {
				t.Fatal("share replay duplicate", e)
			}
			own, e := New(f.pool, false).ReadEntityShareControlOperation(f.ctx, g.access(), target, in.OperationID)
			if e != nil || own.MessageID != r.MessageID {
				t.Fatal("original independent receipt", e)
			}
			g.conversation = r.ConversationID
		})
	}
	cv, msg, notice, history := share143Counts(t, g)
	if cv != 1 || msg != 7 || notice != 7 || history != 7 {
		t.Fatal("canonical once effects", cv, msg, notice, history)
	}
	var starts, shares, agents int
	if e := f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_friend_chat' AND action='start'),(SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_entity_card_share' AND action='share'),(SELECT count(*) FROM agents WHERE principal_account_id=$1)`, f.actor.ID).Scan(&starts, &shares, &agents); e != nil || starts != 1 || shares != 7 || agents != 0 {
		t.Fatal("canonical audits / ordinary no Agent", starts, shares, agents, e)
	}
	// The sender's real organization reader is admitted; this actual recipient
	// has no membership. Sharing must fail even though the title is unchanged.
	f.exec(t, `INSERT INTO organization_memberships(organization_id,user_account_id,role,status)VALUES($1,$2,'owner','active')`, orgID, f.actor.ID)
	f.exec(t, `UPDATE activities SET visibility='organizer_members',organization_id=$2,host_account_id=$3 WHERE id=$1`, sources[0].ID, orgID, orgPrincipal)
	f.exec(t, `UPDATE activity_organizers SET organization_id=$2,person_account_id=NULL,community_id=NULL,business_id=NULL WHERE activity_id=$1`, sources[0].ID, orgID)
	tx, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	title, e := visibleChatEntity(f.ctx, tx, f.actor.ID, "activity", sources[0].ID)
	_ = tx.Rollback(context.Background())
	if e != nil || title == "" {
		t.Fatal("actual sender reader not eligible", e)
	}
	if _, e = f.store.PreviewEntityShareControl(f.ctx, g.access(), target, share143Input(t, g, "activity", sources[0].ID)); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("sender visibility authorized receiver", e)
	}
	_, after, _, _ := share143Counts(t, g)
	if after != 7 {
		t.Fatal("receiver denial wrote message", after)
	}
}
func TestEntityShareControlNativeOriginalSessionSourceAndNamespaceWait(t *testing.T) {
	t.Run("venueDeadlineAfterActualSessionWait", share143VenueDeadlineWait)
	g := share143Native(t, true)
	f := g.f
	target := connection.EntityShareRecipient{Type: "CONVERSATION", ID: g.conversation}
	for _, change := range []string{"SessionABA", "sameTitleSourceABA"} {
		t.Run(change, func(t *testing.T) {
			in := share143Approve(t, g, target, share143Input(t, g, "place", g.place))
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
			var pid int
			if e = outer.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
				t.Fatal(e)
			}
			if _, e = outer.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.actor.ID); e != nil {
				t.Fatal(e)
			}
			done := make(chan error, 1)
			go func() { _, e := f.store.CommitEntityShareControl(ctx, g.access(), target, in); done <- e }()
			hcNativeWaitPID(t, f, pid)
			if change == "SessionABA" {
				f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
				f.exec(t, `UPDATE sessions SET revoked_at=NULL WHERE id=$1`, f.session)
			} else {
				f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, g.place)
				f.exec(t, `UPDATE places SET publication_status='published' WHERE id=$1`, g.place)
			}
			if e = outer.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			select {
			case e = <-done:
				if !errors.Is(e, identity.ErrControlChanged) {
					t.Fatal("original source/frame re-used", change, e)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("writer did not finish")
			}
			_, msg, notice, hist := share143Counts(t, g)
			if msg != 0 || notice != 0 || hist != 0 {
				t.Fatal("retired source leaked effects", msg, notice, hist)
			}
		})
	}
	t.Run("previewTailSourceAfterRealSessionWait", func(t *testing.T) {
		in := share143Input(t, g, "place", g.place)
		p, e := f.store.PreviewEntityShareControl(f.ctx, g.access(), target, in)
		if e != nil {
			t.Fatal(e)
		}
		outer, e := f.pool.Begin(f.ctx)
		if e != nil {
			t.Fatal(e)
		}
		defer outer.Rollback(context.Background())
		var pid int
		if e = outer.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
			t.Fatal(e)
		}
		if _, e = outer.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
			t.Fatal(e)
		}
		done := make(chan error, 1)
		go func() { done <- p.ResponseProof.RevalidateHumanControl(f.ctx) }()
		hcNativeWaitPID(t, f, pid)
		f.exec(t, `UPDATE places SET name=name WHERE id=$1`, g.place)
		if e = outer.Commit(f.ctx); e != nil {
			t.Fatal(e)
		}
		select {
		case e = <-done:
			if !errors.Is(e, identity.ErrControlChanged) {
				t.Fatal("preview old source survived final Session wait", e)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("proof did not finish")
		}
	})
}
func share143BlockedPID(t *testing.T, f *savedPageNativeFixture, blocker int) int {
	t.Helper()
	hcNativeWaitPID(t, f, blocker)
	var pid int
	if e := f.pool.QueryRow(f.ctx, `SELECT pid FROM pg_stat_activity WHERE datname=current_database() AND $1=ANY(pg_blocking_pids(pid)) AND wait_event_type='Lock' ORDER BY pid LIMIT 1`, blocker).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	return pid
}
func share143LegacyStopCompetition(t *testing.T) {
	g := share143Native(t, true)
	f := g.f
	target := connection.EntityShareRecipient{Type: "CONVERSATION", ID: g.conversation}
	for _, first := range []string{"legacyFirst", "stopFirst"} {
		t.Run(first, func(t *testing.T) {
			op := f.id(t)
			legacy := connection.EntityShareInput{OperationID: op}
			legacy.Entity.Type = "place"
			legacy.Entity.ID = g.place
			frame, e := f.store.CaptureHumanControlSession(f.ctx, f.digest, f.actor)
			if e != nil {
				t.Fatal(e)
			}
			initialCtx := identity.WithHumanControlFrame(f.ctx, f.digest, f.actor, frame)
			outer, e := f.pool.Begin(f.ctx)
			if e != nil {
				t.Fatal(e)
			}
			defer outer.Rollback(context.Background())
			var blocker int
			if e = outer.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&blocker); e != nil {
				t.Fatal(e)
			}
			oldDone := make(chan error, 1)
			type stopResult struct {
				v connection.EntityShareControlReceipt
				e error
			}
			stopDone := make(chan stopResult, 1)
			if first == "legacyFirst" {
				if _, e = outer.Exec(f.ctx, `SELECT id FROM accounts WHERE id=$1 FOR NO KEY UPDATE`, f.actor.ID); e != nil {
					t.Fatal(e)
				}
				go func() { _, e := f.store.ShareHumanEntity(f.ctx, g.access(), g.conversation, legacy); oldDone <- e }()
				oldPID := share143BlockedPID(t, f, blocker)
				go func() {
					v, e := f.store.StopEntityShareControlOperation(initialCtx, g.access(), target, op)
					stopDone <- stopResult{v, e}
				}()
				hcNativeWaitPID(t, f, oldPID)
			} else {
				if _, e = outer.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
					t.Fatal(e)
				}
				go func() {
					v, e := f.store.StopEntityShareControlOperation(initialCtx, g.access(), target, op)
					stopDone <- stopResult{v, e}
				}()
				stopPID := share143BlockedPID(t, f, blocker)
				go func() { _, e := f.store.ShareHumanEntity(f.ctx, g.access(), g.conversation, legacy); oldDone <- e }()
				hcNativeWaitPID(t, f, stopPID)
			}
			if e = outer.Commit(f.ctx); e != nil {
				t.Fatal(e)
			}
			var oldErr error
			var result stopResult
			select {
			case oldErr = <-oldDone:
			case <-time.After(5 * time.Second):
				t.Fatal("compatible074 producer/STOP lock cycle")
			}
			select {
			case result = <-stopDone:
			case <-time.After(5 * time.Second):
				t.Fatal("STOP did not complete after actual074 wait")
			}
			var msg, hist int
			if e = f.pool.QueryRow(f.ctx, `SELECT(SELECT count(*) FROM conversation_messages WHERE sender_account_id=$1 AND client_operation_id=$2),(SELECT count(*) FROM human_entity_share_operations WHERE actor_account_id=$1 AND operation_id=$2)`, f.actor.ID, op).Scan(&msg, &hist); e != nil {
				t.Fatal(e)
			}
			if first == "legacyFirst" {
				if oldErr != nil || result.e != nil || result.v.Status != "SUCCEEDED" || msg != 1 || hist != 0 {
					t.Fatal("exact074 effect must precede tiny fact not global STOP", oldErr, result.e, result.v.Status, msg, hist)
				}
			} else {
				if oldErr == nil || result.e != nil || result.v.Status != "STOPPED" || msg != 0 || hist != 1 {
					t.Fatal("STOP-first must fence actual late074 writer", oldErr, result.e, result.v.Status, msg, hist)
				}
			}
		})
	}
	_, msg, notice, hist := share143Counts(t, g)
	if msg != 1 || notice != 1 || hist != 1 {
		t.Fatal("competition duplicate canonical effects", msg, notice, hist)
	}
	var audit int
	if e := f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_entity_card_share' AND action='share'`, f.actor.ID).Scan(&audit); e != nil || audit != 1 {
		t.Fatal("competition audit once", audit, e)
	}
}
func share143VenueDeadlineWait(t *testing.T) {
	g := share143Native(t, true)
	f := g.f
	principal, biz, candidate := f.id(t), f.id(t), f.id(t)
	f.exec(t, `INSERT INTO accounts(id,account_type)VALUES($1,'business')`, principal)
	f.exec(t, `INSERT INTO businesses(id,account_id,name,claim_status,claim_source_url,claim_reviewed_by,claim_reviewed_at)VALUES($1,$2,'合成有期限场地商家143','verified','https://example.invalid/143',$3,clock_timestamp())`, biz, principal, g.peer)
	f.exec(t, `INSERT INTO venue_candidates(id,place_id,city_id,submitted_by,capacity,reservation_support,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at)VALUES($1,$2,'aberdeen-gb',$3,24,'contact','https://example.invalid/143','仅隔离合成场地资料且经不同人员审核',clock_timestamp()+interval '1 hour','approved',$4,clock_timestamp())`, candidate, g.place, f.actor.ID, g.peer)
	f.exec(t, `INSERT INTO venues(place_id,city_id,capacity,reservation_support,source_candidate_id,source_url,reviewed_by,reviewed_at,expires_at)VALUES($1,'aberdeen-gb',24,'contact',$2,'https://example.invalid/143',$3,clock_timestamp(),clock_timestamp()+interval '1 hour')`, g.place, candidate, g.peer)
	f.exec(t, `INSERT INTO business_venue_relations(business_id,place_id,status,evidence_url,submitted_by,reviewed_by,reviewed_at)VALUES($1,$2,'verified','https://example.invalid/143',$3,$4,clock_timestamp())`, biz, g.place, f.actor.ID, g.peer)
	activity := "b1700000-0000-4000-8000-000000000017"
	f.exec(t, `UPDATE activities SET host_account_id=$2,organization_id=NULL,place_id=$3,venue_place_id=$3,modality='in_person',physical_place_status='confirmed',visibility='public' WHERE id=$1`, activity, principal, g.place)
	f.exec(t, `UPDATE activity_organizers SET business_id=$2,person_account_id=NULL,community_id=NULL,organization_id=NULL WHERE activity_id=$1`, activity, biz)
	var until time.Time
	if e := f.pool.QueryRow(f.ctx, `UPDATE venues SET expires_at=clock_timestamp()+interval '6 seconds' WHERE place_id=$1 RETURNING expires_at`, g.place).Scan(&until); e != nil {
		t.Fatal(e)
	}
	target := connection.EntityShareRecipient{Type: "CONVERSATION", ID: g.conversation}
	in := share143Input(t, g, "activity", activity)
	p, e := f.store.PreviewEntityShareControl(f.ctx, g.access(), target, in)
	if e != nil || p.ExpiresAt.After(until) || !p.ExpiresAt.After(time.Now()) {
		t.Fatal("actual venue expiry not in exact preview", p.ExpiresAt, until, e)
	}
	if e = p.ResponseProof.RevalidateHumanControl(f.ctx); e != nil {
		t.Fatal(e)
	}
	in.SourceVersion = p.SourceVersion
	in.Approval = p.Approval
	frame, e := f.store.CaptureHumanControlSession(f.ctx, f.digest, f.actor)
	if e != nil {
		t.Fatal(e)
	}
	initialCtx := identity.WithHumanControlFrame(f.ctx, f.digest, f.actor, frame)
	outer, e := f.pool.Begin(f.ctx)
	if e != nil {
		t.Fatal(e)
	}
	defer outer.Rollback(context.Background())
	var pid int
	if e = outer.QueryRow(f.ctx, `SELECT pg_backend_pid()`).Scan(&pid); e != nil {
		t.Fatal(e)
	}
	if _, e = outer.Exec(f.ctx, `SELECT id FROM sessions WHERE id=$1 FOR UPDATE`, f.session); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { _, e := f.store.CommitEntityShareControl(initialCtx, g.access(), target, in); done <- e }()
	hcNativeWaitPID(t, f, pid)
	time.Sleep(time.Until(p.ExpiresAt) + 75*time.Millisecond)
	if e = outer.Commit(f.ctx); e != nil {
		t.Fatal(e)
	}
	select {
	case e = <-done:
		if !errors.Is(e, identity.ErrControlChanged) {
			t.Fatal("Tx-now renewed expired business venue", e)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("venue expiry writer remained waiting")
	}
	_, msg, notice, hist := share143Counts(t, g)
	if msg != 0 || notice != 0 || hist != 0 {
		t.Fatal("aftereffect venue expiry did not rollback", msg, notice, hist)
	}
	var audit int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM audit_events WHERE actor_account_id=$1 AND purpose='human_entity_card_share' AND action='share'`, f.actor.ID).Scan(&audit); e != nil || audit != 0 {
		t.Fatal("expired venue audit not rolled back", audit, e)
	}
}

func TestEntityShareControlNativeOwnHistoryStoppedLateAndLegacyNamespace(t *testing.T) {
	t.Run("compatible074AndStopActualCompetition", share143LegacyStopCompetition)
	g := share143Native(t, true)
	f := g.f
	target := connection.EntityShareRecipient{Type: "CONVERSATION", ID: g.conversation}
	in := share143Approve(t, g, target, share143Input(t, g, "place", g.place))
	r, e := f.store.CommitEntityShareControl(f.ctx, g.access(), target, in)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE person_ties SET status='removed' WHERE id=$1`, g.tie)
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, g.place)
	history, e := f.store.ReadEntityShareControlOperation(f.ctx, g.access(), target, in.OperationID)
	if e != nil || history.MessageID != r.MessageID {
		t.Fatal("original own fact hidden by source", e)
	}
	stop := f.id(t)
	stopped, e := f.store.StopEntityShareControlOperation(f.ctx, g.access(), target, stop)
	if e != nil || stopped.Status != "STOPPED" {
		t.Fatal("lost relationship close", e)
	}
	late := in
	late.OperationID = stop
	if out, e := f.store.CommitEntityShareControl(f.ctx, g.access(), target, late); e != nil || out.Status != "STOPPED" {
		t.Fatal("late controlled PUT wrote", e)
	}
	legacyLate := connection.EntityShareInput{OperationID: stop}
	legacyLate.Entity.Type = "place"
	legacyLate.Entity.ID = g.place
	if _, e = f.pool.Exec(f.ctx, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,entity_type,entity_id,client_operation_id)VALUES($1,$2,'分享了一张卡片','place',$3,$4)`, g.conversation, f.actor.ID, g.place, stop); e == nil {
		t.Fatal("old in-flight insert crossed tombstone")
	}
	wrong := connection.EntityShareRecipient{Type: "FRIEND_TIE", ID: g.tie}
	if _, e = f.store.ReadEntityShareControlOperation(f.ctx, g.access(), wrong, in.OperationID); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("recipient namespace borrowed", e)
	}
	peerAccess := connection.EntityShareAccess{Actor: identity.Actor{ID: g.peer, AccountType: "person"}, SessionDigest: g.peerDigest}
	if _, e = f.store.ReadEntityShareControlOperation(f.ctx, peerAccess, target, in.OperationID); !errors.Is(e, connection.ErrNotFound) {
		t.Fatal("other actor read receipt", e)
	}
	_, msg, notice, hist := share143Counts(t, g)
	if msg != 1 || notice != 1 || hist != 2 {
		t.Fatal("original / stop effects", msg, notice, hist)
	}
	// Two legitimate old074 namespaces with the same sender+operation remain
	// independently readable; a third namespace may not claim global STOPPED.
	op, cv2, req2 := f.id(t), f.id(t), f.id(t)
	f.exec(t, `INSERT INTO connection_requests(id,sender_account_id,recipient_account_id,city_id,note,scope,state,expires_at,decided_at)VALUES($1,$2,$3,'aberdeen-gb','合成原074会话','conversation','accepted',clock_timestamp()+interval '1 hour',clock_timestamp())`, req2, f.actor.ID, g.peer)
	f.exec(t, `INSERT INTO conversations(id,request_id,member_a_account_id,member_b_account_id)VALUES($1,$2,LEAST($3::uuid,$4::uuid),GREATEST($3::uuid,$4::uuid))`, cv2, req2, f.actor.ID, g.peer)
	for _, cv := range []string{g.conversation, cv2} {
		f.exec(t, `INSERT INTO conversation_messages(conversation_id,sender_account_id,body,entity_type,entity_id,client_operation_id)VALUES($1,$2,'分享了一张卡片','place',$3,$4)`, cv, f.actor.ID, g.place, op)
		own, e := f.store.ReadEntityShareControlOperation(f.ctx, g.access(), connection.EntityShareRecipient{Type: "CONVERSATION", ID: cv}, op)
		if e != nil || own.Status != "SUCCEEDED" || own.ConversationID != cv {
			t.Fatal("exact legacy bridge", own, e)
		}
	}
	third := connection.EntityShareRecipient{Type: "CONVERSATION", ID: f.id(t)}
	if _, e = f.store.StopEntityShareControlOperation(f.ctx, g.access(), third, op); !errors.Is(e, identity.ErrControlChanged) {
		t.Fatal("existing other074 effect became global stop", e)
	}
	var n int
	if e = f.pool.QueryRow(f.ctx, `SELECT count(*) FROM human_entity_share_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.actor.ID, op).Scan(&n); e != nil || n != 0 {
		t.Fatal("bridge wrote ledger or side effects", n, e)
	}
}
