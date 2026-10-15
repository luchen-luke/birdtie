package postgres

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentnotification"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/inbox"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

var _ content.MomentInteractionStore = (*Store)(nil)
var _ content.InboxManagementStore = (*Store)(nil)

func (s *Store) beginSocialOperation(ctx context.Context, d [32]byte, a identity.Actor, family, op string) (context.Context, humanWriteFrame, error) {
	ctx, first, e := s.humanWriteBound(ctx, d, a)
	f := humanWriteFrame{d: d, a: a, session: first, family: family, op: op}
	if e != nil {
		return ctx, f, e
	}
	if !identity.HumanControlUUID(op) || (family != "MOMENT_INTERACTION" && family != "INBOX_MANAGEMENT") {
		return ctx, f, identity.ErrControlInvalid
	}
	f.tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ctx, f, identity.ErrControlUnavailable
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e == nil {
		_, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,151))`, a.ID+":"+family+":"+op)
	}
	if e != nil {
		_ = f.tx.Rollback(context.Background())
		return ctx, f, identity.ErrControlUnavailable
	}
	return ctx, f, nil
}

type socialOperationProof struct {
	s          *Store
	d          [32]byte
	a          identity.Actor
	frame      identity.HumanControlFrame
	family, op string
	until      time.Time
	check      func(context.Context, humanWriteFrame) error
}

func (p *socialOperationProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	ctx, f, e := p.s.beginSocialOperation(ctx, p.d, p.a, p.family, p.op)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	return p.s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.until, func() error { return p.check(ctx, f) }))
}
func socialProof(s *Store, f humanWriteFrame, until time.Time, check func(context.Context, humanWriteFrame) error) identity.HumanControlProof {
	return &socialOperationProof{s, f.d, f.a, f.session, f.family, f.op, until, check}
}

func interactionReceiptTx(ctx context.Context, f humanWriteFrame) (content.MomentInteractionReceipt, error) {
	r := content.MomentInteractionReceipt{OperationID: f.op}
	var action, moment, comment *string
	var digest []byte
	var at time.Time
	e := f.tx.QueryRow(ctx, `SELECT status,action,moment_id,comment_id,applied_version,payload_sha256,committed_at FROM moment_interaction_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&r.Status, &action, &moment, &comment, &r.AppliedVersion, &digest, &at)
	if e != nil {
		return r, e
	}
	if r.Status == "STOPPED" {
		return r, nil
	}
	if action == nil || moment == nil || len(digest) != 32 {
		return r, identity.ErrControlUnavailable
	}
	r.Action, r.MomentID, r.PayloadDigest = *action, *moment, hex.EncodeToString(digest)
	if comment != nil {
		r.CommentID = *comment
	}
	at = at.UTC()
	r.CommittedAt = &at
	return r, nil
}
func (s *Store) ReadMomentInteractionOperation(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.MomentInteractionReceipt, error) {
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "MOMENT_INTERACTION", op)
	if e != nil {
		return content.MomentInteractionReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := interactionReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return r, e
			}
			return r, content.ErrNotFound
		}
		_, e = f.tx.Exec(ctx, `INSERT INTO moment_interaction_operations(actor_account_id,operation_id,status)VALUES($1,$2,'STOPPED')`, a.ID, op)
		r = content.MomentInteractionReceipt{OperationID: op, Status: "STOPPED"}
	}
	if e != nil {
		return r, identity.ErrControlUnavailable
	}
	e = s.humanWriteFinish(ctx, f)
	return r, e
}

type interactionSource struct {
	version        string
	until          time.Time
	likeVersion    int64
	liked          bool
	commentVersion int64
	author         string
}

func (s *Store) interactionSource(ctx context.Context, f humanWriteFrame, in content.MomentInteractionInput, expected bool) (interactionSource, error) {
	var out interactionSource
	var city string
	var place *string
	var revision int64
	e := f.tx.QueryRow(ctx, `SELECT author_account_id,coalesce(city_id,''),place_id,revision FROM moments WHERE id=$1 FOR UPDATE`, in.MomentID).Scan(&out.author, &city, &place, &revision)
	if errors.Is(e, pgx.ErrNoRows) {
		return out, content.ErrNotFound
	}
	if e != nil {
		return out, identity.ErrControlUnavailable
	}
	if revision != in.ExpectedMomentRevision {
		return out, identity.ErrControlChanged
	}
	var cityID string
	if city != "" {
		if e = f.tx.QueryRow(ctx, `SELECT id FROM cities WHERE id=$1 AND publication_status='published' AND(expires_at IS NULL OR expires_at>clock_timestamp()) FOR SHARE`, city).Scan(&cityID); e != nil {
			return out, identity.ErrControlChanged
		}
	}
	if place != nil {
		var id string
		if e = f.tx.QueryRow(ctx, `SELECT id FROM places WHERE id=$1 FOR SHARE`, *place).Scan(&id); e != nil {
			return out, identity.ErrControlChanged
		}
	}
	base, e := readPublicMomentContextTx(ctx, f.tx, content.PublicMomentAccess{Actor: f.a, SessionDigest: f.d}, in.MomentID, false, false, s.devPhoneEnabled)
	if e != nil {
		return out, e
	}
	out.until = base.View.ValidUntil
	var likeStamp, principal string
	e = f.tx.QueryRow(ctx, `SELECT a.xmin::text,coalesce(l.version,0),coalesce(l.liked,false),coalesce(l.xmin::text,'') FROM accounts a LEFT JOIN moment_likes l ON l.moment_id=$2 AND l.actor_account_id=a.id WHERE a.id=$1 AND a.account_type='person' AND a.status='active'`, f.a.ID, in.MomentID).Scan(&principal, &out.likeVersion, &out.liked, &likeStamp)
	if e != nil {
		return out, identity.ErrControlChanged
	}
	if expected && out.likeVersion != in.ExpectedLikeVersion {
		return out, identity.ErrControlChanged
	}
	commentStamp := ""
	if in.Action == "DELETE_COMMENT" {
		e = f.tx.QueryRow(ctx, `SELECT version,xmin::text FROM moment_comments WHERE id=$1 AND moment_id=$2 AND author_account_id=$3 AND NOT deleted FOR UPDATE`, in.CommentID, in.MomentID, f.a.ID).Scan(&out.commentVersion, &commentStamp)
		if e != nil {
			return out, content.ErrNotFound
		}
	}
	out.version = content.MomentControlHash([]any{"moment-interaction-source-v1", base.BaseProof, principal, likeStamp, out.likeVersion, out.liked, in.CommentID, commentStamp})
	return out, nil
}
func (s *Store) PreviewMomentInteraction(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentInteractionInput) (content.MomentInteractionPreview, error) {
	in, e := content.NormalizeMomentInteraction(in, false)
	if e != nil {
		return content.MomentInteractionPreview{}, e
	}
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "MOMENT_INTERACTION", in.OperationID)
	if e != nil {
		return content.MomentInteractionPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	src, e := s.interactionSource(ctx, f, in, true)
	if e != nil {
		return content.MomentInteractionPreview{}, e
	}
	p, e := s.humanWritePreview(ctx, f, in.MomentID, in.Action, content.MomentInteractionDigest(in), src.version, &src.until)
	if e != nil {
		return content.MomentInteractionPreview{}, e
	}
	out := content.MomentInteractionPreview{HumanControlPreview: p, Input: in, SourceVersion: src.version}
	out.ResponseProof = socialProof(s, f, p.ExpiresAt, func(ctx context.Context, f humanWriteFrame) error {
		fresh, e := s.interactionSource(ctx, f, in, true)
		if e != nil {
			return e
		}
		if fresh.version != src.version {
			return identity.ErrControlChanged
		}
		return nil
	})
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ExpiresAt, func() error {
		fresh, e := s.interactionSource(ctx, f, in, true)
		if e == nil && fresh.version != src.version {
			e = identity.ErrControlChanged
		}
		return e
	}))
	return out, e
}
func (s *Store) CommitMomentInteraction(ctx context.Context, d [32]byte, a identity.Actor, in content.MomentInteractionInput) (content.MomentInteractionReceipt, error) {
	in, e := content.NormalizeMomentInteraction(in, true)
	if e != nil {
		return content.MomentInteractionReceipt{}, e
	}
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "MOMENT_INTERACTION", in.OperationID)
	if e != nil {
		return content.MomentInteractionReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := content.MomentInteractionDigest(in)
	if old, e := interactionReceiptTx(ctx, f); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return old, identity.ErrControlChanged
		}
		e = s.humanWriteFinish(ctx, f)
		return old, e
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.MomentInteractionReceipt{}, identity.ErrControlUnavailable
	}
	src, e := s.interactionSource(ctx, f, in, true)
	if e != nil {
		return content.MomentInteractionReceipt{}, e
	}
	if src.version != in.SourceVersion {
		return content.MomentInteractionReceipt{}, identity.ErrControlChanged
	}
	until, e := s.humanWriteApprove(ctx, f, in.MomentID, in.Action, digest, src.version, in.Approval, &src.until)
	if e != nil {
		return content.MomentInteractionReceipt{}, e
	}
	// Serialize this person's bounded admission count across different Moments.
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,151))`, a.ID+":MOMENT_INTERACTION_BUDGET"); e != nil {
		return content.MomentInteractionReceipt{}, identity.ErrControlUnavailable
	}
	var recent int
	window, limit := 1, 60
	if in.Action == "COMMENT" {
		window, limit = 5, 20
	}
	if e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_interaction_operations WHERE actor_account_id=$1 AND status='SUCCEEDED' AND committed_at>clock_timestamp()-make_interval(mins=>$2)`, a.ID, window).Scan(&recent); e != nil || recent >= limit {
		return content.MomentInteractionReceipt{}, identity.ErrControlChanged
	}
	applied := int64(1)
	commentID := in.CommentID
	notify := false
	switch in.Action {
	case "LIKE", "UNLIKE":
		want := in.Action == "LIKE"
		applied = src.likeVersion
		if src.likeVersion == 0 || src.liked != want {
			applied++
			_, e = f.tx.Exec(ctx, `INSERT INTO moment_likes(moment_id,actor_account_id,version,liked)VALUES($1,$2,$3,$4) ON CONFLICT(moment_id,actor_account_id) DO UPDATE SET version=EXCLUDED.version,liked=EXCLUDED.liked,updated_at=clock_timestamp()`, in.MomentID, a.ID, applied, want)
			notify = want
		}
	case "COMMENT":
		e = f.tx.QueryRow(ctx, `INSERT INTO moment_comments(id,moment_id,author_account_id,body,version)VALUES(gen_random_uuid(),$1,$2,$3,1)RETURNING id`, in.MomentID, a.ID, in.Body).Scan(&commentID)
		notify = true
	case "DELETE_COMMENT":
		applied = src.commentVersion + 1
		e = f.tx.QueryRow(ctx, `UPDATE moment_comments SET deleted=true,version=version+1,updated_at=clock_timestamp() WHERE id=$1 AND author_account_id=$2 AND NOT deleted RETURNING version`, in.CommentID, a.ID).Scan(&applied)
	}
	if e != nil {
		return content.MomentInteractionReceipt{}, identity.ErrControlUnavailable
	}
	raw, _ := hex.DecodeString(digest)
	var event string
	e = f.tx.QueryRow(ctx, `INSERT INTO moment_interaction_operations(actor_account_id,operation_id,status,action,moment_id,comment_id,applied_version,payload_sha256,notification_source)VALUES($1,$2,'SUCCEEDED',$3,$4,nullif($5,'')::uuid,$6,$7,$8)RETURNING event_id`, a.ID, in.OperationID, in.Action, in.MomentID, commentID, applied, raw, notify).Scan(&event)
	if e != nil {
		return content.MomentInteractionReceipt{}, identity.ErrControlUnavailable
	}
	if notify && src.author != a.ID {
		kind := agentnotification.KindMomentLike
		if in.Action == "COMMENT" {
			kind = agentnotification.KindMomentComment
		}
		if _, e = routeNativeNotification(ctx, f.tx, kind, event, src.author); e != nil {
			return content.MomentInteractionReceipt{}, identity.ErrControlUnavailable
		}
	}
	// Retain a post-effect parent/actor stamp. Deleting a comment intentionally
	// changes its source, so the final gate checks that exact resulting row.
	var post string
	e = f.tx.QueryRow(ctx, `SELECT birdtie_moment_public_binding($1,$2,clock_timestamp())`, in.MomentID, a.ID).Scan(&post)
	if e != nil {
		return content.MomentInteractionReceipt{}, identity.ErrControlChanged
	}
	r, e := interactionReceiptTx(ctx, f)
	if e != nil {
		return r, e
	}
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		var current string
		e := f.tx.QueryRow(ctx, `SELECT birdtie_moment_public_binding($1,$2,clock_timestamp())`, in.MomentID, a.ID).Scan(&current)
		if e != nil || current != post {
			return identity.ErrControlChanged
		}
		return nil
	}))
	return r, e
}

type interactionCursor struct {
	Moment, Viewer, Binding string
	Window, Before          time.Time
	ID                      string
}

func decodeInteractionCursor(raw, id, viewer, binding string) (*interactionCursor, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 1500 {
		return nil, content.ErrInvalid
	}
	b, e := base64.RawURLEncoding.Strict().DecodeString(raw)
	var c interactionCursor
	if e != nil || json.Unmarshal(b, &c) != nil {
		return nil, content.ErrInvalid
	}
	canonical, _ := json.Marshal(c)
	if !reflect.DeepEqual(b, canonical) || c.Moment != id || c.Viewer != viewer || c.Binding != binding || !identity.HumanControlUUID(c.ID) || c.Before.IsZero() || c.Window.Before(c.Before) {
		return nil, content.ErrInvalid
	}
	return &c, nil
}

const legalInteractionActorSQL = `actor.account_type='person' AND actor.status='active' AND birdtie_moment_public_binding($1,actor.id,clock_timestamp()) IS NOT NULL AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=actor.id)OR(b.blocker_account_id=actor.id AND b.blocked_account_id=$2)))`

func (s *Store) readInteractionPageTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, id, cursor, anchor string) (content.MomentInteractionPage, error) {
	out := content.MomentInteractionPage{SchemaVersion: "moment-interaction-page-v1", MomentID: id, Comments: []content.MomentComment{}}
	base, e := readPublicMomentContextTx(ctx, tx, a, id, false, false, s.devPhoneEnabled)
	if e != nil {
		return out, e
	}
	out.MomentRevision = base.View.Revision
	out.ObservedAt, out.ValidUntil = base.View.ObservedAt, base.View.ValidUntil
	out.SourceVersion = base.BaseProof
	c, e := decodeInteractionCursor(cursor, id, a.Actor.ID, base.BaseProof)
	if e != nil {
		return out, e
	}
	window := out.ObservedAt
	var before time.Time
	var beforeID any
	if c != nil {
		window, before, beforeID = c.Window, c.Before, c.ID
	}
	viewer := momentPhotoViewer(momentPhotoPublicAccess(a))
	e = tx.QueryRow(ctx, `SELECT(SELECT count(*)FROM moment_likes l JOIN accounts actor ON actor.id=l.actor_account_id WHERE l.moment_id=$1 AND l.liked AND `+legalInteractionActorSQL+`),(SELECT count(*) FROM moment_comments comment JOIN accounts actor ON actor.id=comment.author_account_id WHERE comment.moment_id=$1 AND NOT comment.deleted AND `+legalInteractionActorSQL+`),coalesce((SELECT liked FROM moment_likes WHERE moment_id=$1 AND actor_account_id=$2),false),coalesce((SELECT version FROM moment_likes WHERE moment_id=$1 AND actor_account_id=$2),0)`, id, viewer).Scan(&out.LikeCount, &out.CommentCount, &out.Liked, &out.LikeVersion)
	if e != nil {
		return out, content.ErrUnavailable
	}
	if anchor != "" && (!identity.HumanControlUUID(anchor) || cursor != "") {
		return out, content.ErrInvalid
	}
	rows, e := tx.Query(ctx, `SELECT comment.id,comment.body,comment.created_at,`+publicMomentAuthorProfileSQL("actor")+`,comment.author_account_id=$2,jsonb_build_array(comment.xmin::text,actor.xmin::text,`+publicMomentAuthorBindingSQL("actor")+`)::text FROM moment_comments comment JOIN accounts actor ON actor.id=comment.author_account_id WHERE comment.moment_id=$1 AND NOT comment.deleted AND `+legalInteractionActorSQL+` AND comment.created_at<=$3 AND($4::boolean=false OR(comment.created_at,comment.id)<($5,$6::uuid)) AND($7='' OR comment.id=nullif($7,'')::uuid) ORDER BY comment.created_at DESC,comment.id DESC LIMIT 21`, id, viewer, window, c != nil, before, beforeID, anchor)
	if e != nil {
		return out, content.ErrUnavailable
	}
	defer rows.Close()
	proofs := []string{}
	for rows.Next() {
		var item content.MomentComment
		var raw []byte
		var own *bool
		var proof string
		if e = rows.Scan(&item.ID, &item.Body, &item.CreatedAt, &raw, &own, &proof); e != nil {
			return out, content.ErrUnavailable
		}
		proofs = append(proofs, proof)
		item.CreatedAt = item.CreatedAt.UTC()
		item.CanDelete = own != nil && *own
		if len(raw) > 0 {
			if json.Unmarshal(raw, &item.AuthorPublicProfile) != nil {
				return out, content.ErrUnavailable
			}
			if item.AuthorPublicProfile != nil {
				value := item.AuthorPublicProfile.AccountID
				item.AuthorID = &value
			}
		}
		out.Comments = append(out.Comments, item)
	}
	if rows.Err() != nil {
		return out, content.ErrUnavailable
	}
	out.ReadProof = content.MomentControlHash(proofs)
	if anchor != "" && len(out.Comments) != 1 {
		return out, content.ErrNotFound
	}
	if len(out.Comments) > 20 {
		out.Comments = out.Comments[:20]
		last := out.Comments[19]
		b, _ := json.Marshal(interactionCursor{id, a.Actor.ID, base.BaseProof, window, last.CreatedAt, last.ID})
		value := base64.RawURLEncoding.EncodeToString(b)
		out.NextCursor = &value
	}
	return out, nil
}

type interactionReadProof struct {
	s                  *Store
	a                  content.PublicMomentAccess
	id, cursor, anchor string
	frame              identity.HumanControlFrame
	page               content.MomentInteractionPage
}

func (p *interactionReadProof) RevalidateHumanControl(ctx context.Context) error {
	if p.a.Actor.ID != "" {
		ctx = identity.WithHumanControlFrame(ctx, p.a.SessionDigest, p.a.Actor, p.frame)
	}
	ctx, tx, frame, e := p.s.beginPublicMomentPhoto(ctx, momentPhotoPublicAccess(p.a), p.id)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	return p.s.finishPublicMomentPhoto(ctx, tx, momentPhotoPublicAccess(p.a), frame, func() error {
		fresh, e := p.s.readInteractionPageTx(ctx, tx, p.a, p.id, p.cursor, p.anchor)
		if e != nil {
			return e
		}
		old := p.page
		old.ResponseProof = nil
		fresh.ObservedAt, fresh.ValidUntil = old.ObservedAt, old.ValidUntil
		if !old.ValidUntil.After(time.Now().UTC()) || !reflect.DeepEqual(old, fresh) {
			return identity.ErrControlChanged
		}
		return nil
	})
}
func (s *Store) ReadMomentInteractions(ctx context.Context, a content.PublicMomentAccess, id, cursor, anchor string) (content.MomentInteractionPage, error) {
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, momentPhotoPublicAccess(a), id)
	if e != nil {
		return content.MomentInteractionPage{}, e
	}
	defer tx.Rollback(context.Background())
	out, e := s.readInteractionPageTx(ctx, tx, a, id, cursor, anchor)
	if e != nil {
		return out, e
	}
	proof := &interactionReadProof{s, a, id, cursor, anchor, frame, out}
	out.ResponseProof = proof
	e = s.finishPublicMomentPhoto(ctx, tx, momentPhotoPublicAccess(a), frame, func() error {
		fresh, e := s.readInteractionPageTx(ctx, tx, a, id, cursor, anchor)
		if e != nil {
			return e
		}
		initial := out
		initial.ResponseProof = nil
		fresh.ObservedAt, fresh.ValidUntil = initial.ObservedAt, initial.ValidUntil
		if !reflect.DeepEqual(initial, fresh) {
			return identity.ErrControlChanged
		}
		return nil
	})
	return out, e
}

func inboxManagementReceiptTx(ctx context.Context, f humanWriteFrame) (content.InboxManagementReceipt, error) {
	r := content.InboxManagementReceipt{OperationID: f.op}
	var action *string
	var digest []byte
	var at time.Time
	var source *string
	e := f.tx.QueryRow(ctx, `SELECT status,action,affected_count,payload_sha256,committed_at,cutoff,source_version FROM inbox_management_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&r.Status, &action, &r.AffectedCount, &digest, &at, &r.Cutoff, &source)
	if e != nil {
		return r, e
	}
	if r.Status == "SUCCEEDED" {
		if action == nil || len(digest) != 32 || r.Cutoff == nil || source == nil {
			return r, identity.ErrControlUnavailable
		}
		r.Action, r.PayloadDigest = *action, hex.EncodeToString(digest)
		at = at.UTC()
		r.CommittedAt = &at
		r.SourceVersion = *source
	}
	return r, nil
}
func (s *Store) ReadInboxManagementOperation(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.InboxManagementReceipt, error) {
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "INBOX_MANAGEMENT", op)
	if e != nil {
		return content.InboxManagementReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := inboxManagementReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.humanWriteFinish(ctx, f); e != nil {
				return r, e
			}
			return r, content.ErrNotFound
		}
		_, e = f.tx.Exec(ctx, `INSERT INTO inbox_management_operations(actor_account_id,operation_id,status)VALUES($1,$2,'STOPPED')`, a.ID, op)
		r = content.InboxManagementReceipt{OperationID: op, Status: "STOPPED"}
	}
	if e != nil {
		return r, identity.ErrControlUnavailable
	}
	e = s.humanWriteFinish(ctx, f)
	return r, e
}
func inboxManagementMembers(ctx context.Context, f humanWriteFrame, in content.InboxManagementInput, cutoff time.Time) ([]string, string, error) {
	var principal string
	if e := f.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, f.a.ID).Scan(&principal); e != nil {
		return nil, "", identity.ErrControlChanged
	}
	rows, e := f.tx.Query(ctx, `SELECT i.id,i.xmin::text FROM inbox_items i LEFT JOIN native_notification_decisions d ON d.id=i.routing_decision_id WHERE i.recipient_account_id=$1 AND i.resource_type<>'conversation_message' AND(i.origin='NATIVE' OR $5='DELETE_ITEM') AND `+inboxVisibility+` AND i.created_at<=$2 AND($3='' OR i.category=$3) AND(NOT $4::boolean OR i.read_at IS NULL) AND($5<>'DELETE_ITEM' OR i.id=nullif($6,'')::uuid) AND($5<>'CLEAR_READ' OR(i.read_at IS NOT NULL AND i.read_at<=$2)) AND($5<>'MARK_ALL_READ' OR i.read_at IS NULL) ORDER BY i.id FOR UPDATE OF i`, f.a.ID, cutoff, in.Category, in.UnreadOnly, in.Action, in.ItemID)
	if e != nil {
		return nil, "", identity.ErrControlUnavailable
	}
	defer rows.Close()
	ids := []string{}
	stamps := []any{}
	for rows.Next() {
		var id, stamp string
		if e = rows.Scan(&id, &stamp); e != nil {
			return nil, "", e
		}
		ids = append(ids, id)
		stamps = append(stamps, []any{id, stamp})
	}
	if e = rows.Err(); e != nil {
		return nil, "", e
	}
	if in.Action == "DELETE_ITEM" && len(ids) != 1 {
		return nil, "", content.ErrNotFound
	}
	return ids, content.MomentControlHash([]any{f.a.ID, principal, in.Category, in.UnreadOnly, in.Action, in.ItemID, cutoff.UTC(), stamps}), nil
}
func (s *Store) PreviewInboxManagement(ctx context.Context, d [32]byte, a identity.Actor, in content.InboxManagementInput) (content.InboxManagementPreview, error) {
	raw, _ := json.Marshal(in)
	in, normalizeErr := content.DecodeInboxManagement(raw, false)
	if normalizeErr != nil {
		return content.InboxManagementPreview{}, normalizeErr
	}
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "INBOX_MANAGEMENT", in.OperationID)
	if e != nil {
		return content.InboxManagementPreview{}, e
	}
	defer f.tx.Rollback(context.Background())
	var cutoff time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&cutoff); e != nil {
		return content.InboxManagementPreview{}, e
	}
	cutoff = cutoff.UTC()
	ids, source, e := inboxManagementMembers(ctx, f, in, cutoff)
	if e != nil {
		return content.InboxManagementPreview{}, e
	}
	in.Cutoff = &cutoff
	in.SourceVersion = source
	p, e := s.humanWritePreview(ctx, f, "", in.Action, content.InboxManagementDigest(in), source, nil)
	if e != nil {
		return content.InboxManagementPreview{}, e
	}
	out := content.InboxManagementPreview{HumanControlPreview: p, Input: in, Cutoff: cutoff, Count: len(ids), SourceVersion: source}
	out.ResponseProof = socialProof(s, f, p.ExpiresAt, func(ctx context.Context, f humanWriteFrame) error {
		_, fresh, e := inboxManagementMembers(ctx, f, in, cutoff)
		if e == nil && fresh != source {
			e = identity.ErrControlChanged
		}
		return e
	})
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, p.ExpiresAt, func() error {
		_, fresh, e := inboxManagementMembers(ctx, f, in, cutoff)
		if e == nil && fresh != source {
			e = identity.ErrControlChanged
		}
		return e
	}))
	return out, e
}
func (s *Store) CommitInboxManagement(ctx context.Context, d [32]byte, a identity.Actor, in content.InboxManagementInput) (content.InboxManagementReceipt, error) {
	rawInput, _ := json.Marshal(in)
	in, normalizeErr := content.DecodeInboxManagement(rawInput, true)
	if normalizeErr != nil {
		return content.InboxManagementReceipt{}, normalizeErr
	}
	ctx, f, e := s.beginSocialOperation(ctx, d, a, "INBOX_MANAGEMENT", in.OperationID)
	if e != nil {
		return content.InboxManagementReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := content.InboxManagementDigest(in)
	if old, e := inboxManagementReceiptTx(ctx, f); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return old, identity.ErrControlChanged
		}
		e = s.humanWriteFinish(ctx, f)
		return old, e
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.InboxManagementReceipt{}, identity.ErrControlUnavailable
	}
	if in.Cutoff == nil {
		return content.InboxManagementReceipt{}, identity.ErrControlInvalid
	}
	ids, source, e := inboxManagementMembers(ctx, f, in, *in.Cutoff)
	if e != nil {
		return content.InboxManagementReceipt{}, e
	}
	if source != in.SourceVersion {
		return content.InboxManagementReceipt{}, identity.ErrControlChanged
	}
	until, e := s.humanWriteApprove(ctx, f, "", in.Action, digest, source, in.Approval, nil)
	if e != nil {
		return content.InboxManagementReceipt{}, e
	}
	if in.Action == "MARK_ALL_READ" {
		_, e = f.tx.Exec(ctx, `UPDATE inbox_items SET read_at=coalesce(read_at,clock_timestamp()) WHERE recipient_account_id=$1 AND id=ANY($2::uuid[])`, a.ID, ids)
	} else {
		_, e = f.tx.Exec(ctx, `DELETE FROM inbox_items WHERE recipient_account_id=$1 AND id=ANY($2::uuid[])`, a.ID, ids)
	}
	if e != nil {
		return content.InboxManagementReceipt{}, identity.ErrControlUnavailable
	}
	raw, _ := hex.DecodeString(digest)
	_, e = f.tx.Exec(ctx, `INSERT INTO inbox_management_operations(actor_account_id,operation_id,status,action,affected_count,payload_sha256,cutoff,source_version)VALUES($1,$2,'SUCCEEDED',$3,$4,$5,$6,$7)`, a.ID, in.OperationID, in.Action, len(ids), raw, *in.Cutoff, in.SourceVersion)
	if e != nil {
		return content.InboxManagementReceipt{}, identity.ErrControlUnavailable
	}
	r, e := inboxManagementReceiptTx(ctx, f)
	if e != nil {
		return r, e
	}
	e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		// This response is the immutable persisted operation fact. Current unread
		// counts belong to ListHumanInboxPage and may change after this cutoff.
		return nil
	}))
	return r, e
}

func (s *Store) GetInboxMomentTarget(ctx context.Context, digest [32]byte, a identity.Actor, item string) (map[string]any, error) {
	ctx, original, e := s.humanWriteBound(ctx, digest, a)
	if e != nil {
		return nil, e
	}
	tx, e := s.beginHumanInboxTx(ctx, digest, a)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	if e = s.lockHumanMomentSession(ctx, tx, digest, a.ID); e != nil {
		return nil, e
	}
	current, _, e := s.humanWriteSession(ctx, tx, digest, a, false)
	if e != nil || current != original {
		return nil, identity.ErrControlChanged
	}
	read := func() (string, *string, error) {
		var mid string
		var cid *string
		e := tx.QueryRow(ctx, `SELECT op.moment_id,op.comment_id FROM inbox_items i JOIN native_notification_decisions d ON d.id=i.routing_decision_id JOIN moment_interaction_operations op ON op.event_id=d.moment_interaction_event_id WHERE i.id=$1 AND i.recipient_account_id=$2 AND i.resource_type IN('moment_like','moment_comment') AND i.resource_id=op.event_id AND `+inboxVisibility, item, a.ID).Scan(&mid, &cid)
		return mid, cid, e
	}
	mid, cid, e := read()
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, inbox.ErrNotFound
	}
	if e != nil {
		return nil, content.ErrUnavailable
	}
	if e = checkHumanMomentSession(ctx, tx, digest, a.ID, s.devPhoneEnabled); e != nil {
		return nil, e
	}
	fresh, comment, e := read()
	if e != nil || fresh != mid || !reflect.DeepEqual(comment, cid) {
		return nil, identity.ErrControlChanged
	}
	if e = tx.Commit(ctx); e != nil {
		return nil, e
	}
	return map[string]any{"momentId": mid, "commentId": cid}, nil
}
