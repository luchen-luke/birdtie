package postgres

import (
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"strings"
	"testing"
)

func TestInboxReviewResultNativeOwnerTargetRetirementAndReadOnly(t *testing.T) {
	f := savedPageNative(t)
	_, place := f.bookmark(t)
	permission := mc144RetainedPlace(t, f, place, 0)
	var candidate, reviewer string
	if e := f.pool.QueryRow(f.ctx, `SELECT candidate_id,reviewed_by FROM place_source_permissions WHERE id=$1`, permission).Scan(&candidate, &reviewer); e != nil {
		t.Fatal(e)
	}
	item := f.id(t)
	f.exec(t, `UPDATE place_candidates SET review_note='PRIVATE reviewer note' WHERE id=$1`, candidate)
	f.exec(t, `INSERT INTO inbox_items(id,recipient_account_id,category,title,detail,resource_type,resource_id)VALUES($1,$2,'updates','审核结果','PRIVATE cached summary','place_candidate',$3)`, item, f.actor.ID, candidate)
	v, e := f.store.ReadHumanInboxReviewResult(f.ctx, f.digest, f.actor, item)
	if e != nil || !v.Valid(item) || v.TargetID == nil || *v.TargetID != place {
		t.Fatal("actual published place result", v, e)
	}
	wire, _ := json.Marshal(v)
	if strings.Contains(string(wire), "PRIVATE") || strings.Contains(string(wire), "SourceStamp") || strings.Contains(string(wire), "authorization") {
		t.Fatal("private source released", string(wire))
	}
	if e = f.store.ValidateHumanInboxReviewResultResponse(f.ctx, f.digest, f.actor, v); e != nil {
		t.Fatal("same final source", e)
	}
	var unread bool
	if e = f.pool.QueryRow(f.ctx, `SELECT read_at IS NULL FROM inbox_items WHERE id=$1`, item).Scan(&unread); e != nil || !unread {
		t.Fatal("GET marked original notification", e)
	}
	f.exec(t, `UPDATE places SET publication_status='hidden' WHERE id=$1`, place)
	if e = f.store.ValidateHumanInboxReviewResultResponse(f.ctx, f.digest, f.actor, v); !errors.Is(e, inbox.ErrSourceChanged) {
		t.Fatal("old formal target remained approved", e)
	}
	next, e := f.store.ReadHumanInboxReviewResult(f.ctx, f.digest, f.actor, item)
	if e != nil || next.Status != "published" || next.TargetID != nil || next.TargetAvailable {
		t.Fatal("retain fact, retire target", next, e)
	}
	rejected, notice := f.id(t), f.id(t)
	f.exec(t, `INSERT INTO activity_candidates(id,city_id,submitted_by,title,host_label,starts_at,ends_at,time_zone,source_label,source_url,rights_note,expires_at,status,reviewed_by,reviewed_at)
 VALUES($1,'aberdeen-gb',$2,'PRIVATE title','PRIVATE host',clock_timestamp(),clock_timestamp()+interval '1 hour','Europe/London','PRIVATE','https://private.invalid','PRIVATE',clock_timestamp()+interval '1 day','rejected',$3,clock_timestamp())`, rejected, f.actor.ID, reviewer)
	f.exec(t, `INSERT INTO inbox_items(id,recipient_account_id,category,title,detail,resource_type,resource_id)VALUES($1,$2,'updates','审核结果','PRIVATE cached summary','activity_candidate',$3)`, notice, f.actor.ID, rejected)
	denied, e := f.store.ReadHumanInboxReviewResult(f.ctx, f.digest, f.actor, notice)
	if e != nil || denied.Status != "rejected" || denied.TargetID != nil {
		t.Fatal("activity rejected fact", denied, e)
	}
	f.exec(t, `UPDATE activity_candidates SET submitted_by=$2 WHERE id=$1`, rejected, reviewer)
	if _, e = f.store.ReadHumanInboxReviewResult(f.ctx, f.digest, f.actor, notice); !errors.Is(e, inbox.ErrNotFound) {
		t.Fatal("foreign submitted material", e)
	}
	f.exec(t, `UPDATE activity_candidates SET submitted_by=$2 WHERE id=$1`, rejected, f.actor.ID)
	denied, e = f.store.ReadHumanInboxReviewResult(f.ctx, f.digest, f.actor, notice)
	if e != nil {
		t.Fatal(e)
	}
	f.exec(t, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id=$1`, f.session)
	if e = f.store.ValidateHumanInboxReviewResultResponse(f.ctx, f.digest, f.actor, denied); e == nil {
		t.Fatal("revoked session reused result")
	}
}
