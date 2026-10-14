package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/birdtie/birdtie/apps/api/internal/socialnow"
	"github.com/jackc/pgx/v5"
)

var _ inbox.HumanReviewResultStore = (*Store)(nil)

// $1 is the original notification, $2 its current human recipient. Neither a
// notification's cached target nor a caller-provided candidate ID is authority.
var inboxReviewResultSQL = `WITH clock AS MATERIALIZED(SELECT clock_timestamp() at), candidates AS MATERIALIZED(
 SELECT i.id,i.resource_type kind,ac.status,ac.city_id,ac.resolved_activity_id target,
 concat_ws(':',i.xmin::text,d.xmin::text,ac.xmin::text) source
 FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 JOIN activity_candidates ac ON ac.id=i.resource_id AND ac.submitted_by=$2
 WHERE i.id=$1 AND i.recipient_account_id=$2 AND i.resource_type='activity_candidate'
 AND ac.status IN('published','rejected') AND ` + inboxVisibility + `
 AND(i.routing_decision_id IS NULL OR(d.kind='activity_review' AND d.source_id=ac.id AND d.activity_candidate_id=ac.id))
 UNION ALL
 SELECT i.id,i.resource_type,pc.status,pc.city_id,pc.resolved_place_id,
 concat_ws(':',i.xmin::text,d.xmin::text,pc.xmin::text)
 FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id
 JOIN place_candidates pc ON pc.id=i.resource_id AND pc.submitted_by=$2
 WHERE i.id=$1 AND i.recipient_account_id=$2 AND i.resource_type='place_candidate'
 AND pc.status IN('published','linked_duplicate','rejected') AND ` + inboxVisibility + `
 AND(i.routing_decision_id IS NULL OR(d.kind='place_review' AND d.source_id=pc.id AND d.place_candidate_id=pc.id))
), projected AS(
 SELECT r.*,target.id target_id,target.stamp target_stamp FROM candidates r
 LEFT JOIN LATERAL(
 SELECT p.id,concat_ws(':',p.xmin::text,c.xmin::text,` + retainedPlaceReadBindingSQL("p") + `) stamp
 FROM places p JOIN cities c ON c.id=p.city_id CROSS JOIN clock
 WHERE r.kind='place_candidate' AND p.id=r.target AND p.city_id=r.city_id
 AND p.publication_status='published' AND c.publication_status='published'
 AND(p.expires_at IS NULL OR p.expires_at>clock.at) AND(c.expires_at IS NULL OR c.expires_at>clock.at)
 AND ` + currentRetainedPlaceVisibilitySQL("p", "clock.at") + `
 UNION ALL
 SELECT a.id,concat_ws(':',a.xmin::text,c.xmin::text,ao.xmin::text)` + publishedActivityFrom + `
 AND r.kind='activity_candidate' AND a.id=r.target AND a.city_id=r.city_id
 AND(a.expires_at IS NULL OR a.expires_at>clock_timestamp()) AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp())
 ) target ON true
)
SELECT id,kind,status,target_id,concat_ws(':',source,target_id::text,target_stamp) FROM projected`

func inboxReviewRead(ctx context.Context, tx pgx.Tx, owner, id string) (inbox.ReviewResult, error) {
	v := inbox.ReviewResult{SchemaVersion: inbox.ReviewResultSchema}
	var source string
	e := tx.QueryRow(ctx, inboxReviewResultSQL, id, owner).Scan(&v.ItemID, &v.Kind, &v.Status, &v.TargetID, &source)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, inbox.ErrNotFound
	}
	if e != nil {
		return v, socialnow.ErrUnavailable
	}
	v.TargetAvailable = v.TargetID != nil
	h := sha256.Sum256([]byte(source))
	v.SourceStamp = hex.EncodeToString(h[:])
	if !v.Valid(id) {
		return inbox.ReviewResult{}, socialnow.ErrUnavailable
	}
	return v, nil
}

func (s *Store) ReadHumanInboxReviewResult(ctx context.Context, d [32]byte, a identity.Actor, id string) (inbox.ReviewResult, error) {
	v := inbox.ReviewResult{}
	if !inbox.ValidPageID(id) {
		return v, inbox.ErrNotFound
	}
	ctx, first, e := s.humanWriteBound(ctx, d, a)
	if e != nil {
		return v, e
	}
	tx, e := s.beginHumanInboxTx(ctx, d, a)
	if e != nil {
		return v, e
	}
	defer tx.Rollback(context.Background())
	var locked string
	e = tx.QueryRow(ctx, `SELECT id FROM inbox_items WHERE id=$1 AND recipient_account_id=$2 FOR SHARE`, id, a.ID).Scan(&locked)
	if errors.Is(e, pgx.ErrNoRows) {
		return v, inbox.ErrNotFound
	}
	if e != nil {
		return v, socialnow.ErrUnavailable
	}
	e = s.humanWriteFinish(ctx, humanWriteFrame{tx: tx, d: d, a: a, session: first}, func() error {
		var err error
		v, err = inboxReviewRead(ctx, tx, a.ID, id)
		return err
	})
	if e != nil {
		return inbox.ReviewResult{}, e
	}
	if ctx.Err() != nil {
		return inbox.ReviewResult{}, socialnow.ErrUnavailable
	}
	v.Session = first
	return v, nil
}

func (s *Store) ValidateHumanInboxReviewResultResponse(ctx context.Context, d [32]byte, a identity.Actor, old inbox.ReviewResult) error {
	if !old.Valid(old.ItemID) || old.SourceStamp == "" || !identity.HumanControlUUID(old.Session.ID) || old.Session.Generation < 1 {
		return socialnow.ErrUnavailable
	}
	ctx = identity.WithHumanControlFrame(ctx, d, a, old.Session)
	next, e := s.ReadHumanInboxReviewResult(ctx, d, a, old.ItemID)
	if e != nil {
		return e
	}
	if next.SourceStamp != old.SourceStamp || next.Session != old.Session {
		return inbox.ErrSourceChanged
	}
	return nil
}
