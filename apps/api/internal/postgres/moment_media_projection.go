package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
	"reflect"
	"time"
)

type momentMediaParent struct {
	owner, binding, viewerBinding string
	revision                      int64
	deadline, observed            time.Time
}

func momentMediaParentTx(ctx context.Context, tx pgx.Tx, a mm.Access, moment string) (momentMediaParent, error) {
	var p momentMediaParent
	viewer := momentPhotoViewer(a)
	e := tx.QueryRow(ctx, `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
 SELECT m.author_account_id,m.revision,jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,`+retainedPlaceReadBindingSQL("p")+`)::text,
 least(cl.at+interval '60 seconds',c.expires_at,CASE WHEN p.id IS NOT NULL THEN `+currentRetainedPlaceDeadlineSQL("p", "cl.at")+` END),cl.at
 FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN cities c ON c.id=m.city_id
 LEFT JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id CROSS JOIN cl
 WHERE m.id=$1 AND m.status='published' AND m.visibility='public' AND m.revision>=2
 AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
 AND a.account_type='person' AND a.status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>cl.at)
 AND((m.place_id IS NULL AND m.location_precision='city') OR(m.location_precision='place' AND p.id IS NOT NULL AND p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND `+currentRetainedPlaceVisibilitySQL("p", "cl.at")+`))
 AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=a.id)OR(b.blocker_account_id=a.id AND b.blocked_account_id=$2)))`, moment, viewer).Scan(&p.owner, &p.revision, &p.binding, &p.deadline, &p.observed)
	if errors.Is(e, pgx.ErrNoRows) {
		return p, mm.ErrNotFound
	}
	if e != nil {
		return p, mm.ErrUnavailable
	}
	p.binding = mm.Digest(p.binding)
	e = tx.QueryRow(ctx, `SELECT jsonb_build_array((SELECT xmin::text FROM accounts WHERE id=$1),(SELECT coalesce(jsonb_agg(jsonb_build_array(owner_account_id,target_account_id,version,xmin::text)ORDER BY owner_account_id,target_account_id),'[]'::jsonb)FROM person_block_control_epochs WHERE $1::uuid IS NOT NULL AND((owner_account_id=$1 AND target_account_id=$2)OR(owner_account_id=$2 AND target_account_id=$1))))::text`, viewer, p.owner).Scan(&p.viewerBinding)
	if e != nil {
		return p, mm.ErrUnavailable
	}
	return p, nil
}
func mediaAttachment(moment string, revision, version int64, position int, material mm.Stage, rights mm.Rights, until time.Time) mm.Attachment {
	a := mm.Attachment{MediaID: material.MediaID, MediaVersion: material.Version, Position: position, Width: material.Width, Height: material.Height, DisplayBytes: material.DisplayBytes, DisplaySHA: material.DisplaySHA, ThumbnailBytes: material.ThumbnailBytes, ThumbnailSHA: material.ThumbnailSHA, DisplayPath: mm.Path(moment, revision, version, material.MediaID, material.Version, "display"), ThumbnailPath: mm.Path(moment, revision, version, material.MediaID, material.Version, "thumbnail"), RightsDigest: rights.Digest(), Attribution: rights.Attribution, ValidationKind: mm.ValidationKind, ValidationStatus: "VALIDATED", AntivirusScan: "NOT_RUN", PrivacyRisk: "UNKNOWN", ExpiresAt: until}
	if material.Kind == "VIDEO" && material.Video != nil {
		c := *material.Video
		c.Path = mm.Path(moment, revision, version, material.MediaID, material.Version, "video")
		a.SchemaVersion, a.Kind, a.Purpose, a.Video, a.ValidationKind = "public-moment-media-attachment-v2", "VIDEO", mm.VideoPurpose, &c, mm.VideoValidation
	}
	return a
}
func (s *Store) mediaSetTx(ctx context.Context, tx pgx.Tx, a mm.Access, moment string, own bool) (mm.Set, error) {
	p, e := momentMediaParentTx(ctx, tx, a, moment)
	if e != nil {
		return mm.Set{}, e
	}
	if own && p.owner != a.AccountID {
		return mm.Set{}, mm.ErrForbidden
	}
	v := mm.Set{SchemaVersion: "public-moment-media-set-v1", MomentID: moment, MomentRevision: p.revision, Status: "EMPTY", Attachments: []mm.Attachment{}, ObservedAt: p.observed, ValidUntil: p.deadline, CanReplace: own}
	var approved int64
	var source, stamp string
	e = tx.QueryRow(ctx, `SELECT version,approved_moment_revision,status,source_binding,xmin::text FROM public_moment_media_sets WHERE moment_id=$1`, moment).Scan(&v.SetVersion, &approved, &v.Status, &source, &stamp)
	if errors.Is(e, pgx.ErrNoRows) {
		v.SourceVersion = mm.Digest([]any{p.binding, p.viewerBinding, "none"})
		return v, nil
	}
	if e != nil {
		return v, mm.ErrUnavailable
	}
	v.CanDelete = own && v.Status == "READY"
	proof := []any{p.binding, p.viewerBinding, stamp, v.SetVersion, v.Status}
	if v.Status == "READY" && approved == p.revision && source == p.binding {
		rows, e := tx.Query(ctx, `SELECT metadata,rights,retention_until,display,thumbnail,xmin::text FROM public_moment_media_attachments WHERE moment_id=$1 AND set_version=$2 ORDER BY position`, moment, v.SetVersion)
		if e != nil {
			return v, mm.ErrUnavailable
		}
		defer rows.Close()
		for rows.Next() {
			var meta, rightsRaw, display, thumbnail []byte
			var until time.Time
			var rowStamp string
			if e = rows.Scan(&meta, &rightsRaw, &until, &display, &thumbnail, &rowStamp); e != nil {
				return v, mm.ErrUnavailable
			}
			var attachment mm.Attachment
			var rights mm.Rights
			if json.Unmarshal(meta, &attachment) != nil || json.Unmarshal(rightsRaw, &rights) != nil {
				return v, mm.ErrUnavailable
			}
			if display == nil || thumbnail == nil || !rights.Valid(p.observed) || !until.After(p.observed) || rights.Digest() != attachment.RightsDigest || mm.DigestBytes(display) != attachment.DisplaySHA || mm.DigestBytes(thumbnail) != attachment.ThumbnailSHA {
				v.Status = "DELETED"
				v.Attachments = []mm.Attachment{}
				break
			}
			if until.Before(v.ValidUntil) {
				v.ValidUntil = until
			}
			v.Attachments = append(v.Attachments, attachment)
			proof = append(proof, []any{rowStamp, attachment.MediaID, attachment.MediaVersion, attachment.RightsDigest, attachment.DisplaySHA, attachment.ThumbnailSHA})
		}
		if rows.Err() != nil {
			return v, mm.ErrUnavailable
		}
		rows.Close()
		for _, attachment := range v.Attachments {
			if attachment.Kind == "VIDEO" {
				if len(v.Attachments) != 1 {
					return v, mm.ErrChanged
				}
				binding, e := publicVideoBindingTx(ctx, tx, attachment, moment, v.SetVersion)
				if e != nil {
					return v, e
				}
				v.SchemaVersion = "public-moment-media-set-v2"
				proof = append(proof, []any{"VIDEO", attachment.Video, binding})
			}
		}
		if len(v.Attachments) < 1 {
			v.Status = "DELETED"
		}
	} else if v.Status == "READY" {
		v.Status = "DELETED"
	}
	for i := range v.Attachments {
		v.Attachments[i].ExpiresAt = v.ValidUntil
	}
	v.SourceVersion = mm.Digest(proof)
	if !v.Valid(moment, p.observed) {
		return v, mm.ErrUnavailable
	}
	return v, nil
}
func compareMediaSets(old, next mm.Set) error {
	if old.SourceVersion != next.SourceVersion || old.Status != next.Status || !old.ValidUntil.After(next.ObservedAt) || next.ValidUntil.Before(old.ValidUntil) {
		return mm.ErrChanged
	}
	for i := range next.Attachments {
		next.Attachments[i].ExpiresAt = old.ValidUntil
	}
	next.ObservedAt, next.ValidUntil = old.ObservedAt, old.ValidUntil
	old.ResponseProof, next.ResponseProof = nil, nil
	old.OriginalFrame, next.OriginalFrame = identity.HumanControlFrame{}, identity.HumanControlFrame{}
	if !reflect.DeepEqual(old, next) {
		return mm.ErrChanged
	}
	return nil
}

type mediaSetProof struct {
	s     *Store
	a     mm.Access
	value mm.Set
}

func (p *mediaSetProof) RevalidateHumanControl(ctx context.Context) error {
	_, e := p.s.FinalizePublicMomentMedia(ctx, p.a, p.value)
	return e
}
func (s *Store) ReadPublicMomentMedia(ctx context.Context, a mm.Access, moment string) (mm.Set, error) {
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, a, moment)
	if e != nil {
		return mm.Set{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := s.mediaSetTx(ctx, tx, a, moment, false)
	if e != nil {
		return v, e
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, a, frame, func() error {
		next, e := s.mediaSetTx(ctx, tx, a, moment, false)
		if e != nil {
			return e
		}
		return compareMediaSets(v, next)
	}); e != nil {
		return v, e
	}
	v.OriginalFrame = frame
	return v, nil
}
func (s *Store) FinalizePublicMomentMedia(ctx context.Context, a mm.Access, old mm.Set) (mm.Set, error) {
	if !old.Valid(old.MomentID, time.Now().UTC()) {
		return old, mm.ErrChanged
	}
	if a.AccountID != "" {
		if first, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, mediaActor(a)); present && (!valid || first != old.OriginalFrame) {
			return old, mm.ErrChanged
		}
		if !mm.ID(old.OriginalFrame.ID) || old.OriginalFrame.Generation < 1 {
			return old, mm.ErrChanged
		}
		ctx = identity.WithHumanControlFrame(ctx, a.SessionDigest, mediaActor(a), old.OriginalFrame)
	} else if old.OriginalFrame != (identity.HumanControlFrame{}) {
		return old, mm.ErrChanged
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, a, old.MomentID)
	if e != nil {
		return old, e
	}
	defer tx.Rollback(context.Background())
	if e = s.finishPublicMomentPhoto(ctx, tx, a, frame, func() error {
		next, e := s.mediaSetTx(ctx, tx, a, old.MomentID, old.CanReplace)
		if e != nil {
			return e
		}
		return compareMediaSets(old, next)
	}); e != nil {
		return old, e
	}
	return old, nil
}
func (s *Store) ReadPublicMomentMediaContent(ctx context.Context, a mm.Access, selector mm.ContentSelector, encode func(mm.Attachment, []byte) ([]byte, error)) ([]byte, error) {
	if selector.Variant == "video" {
		admitted, release, e := mm.TryVideoWork(ctx)
		if e != nil {
			return nil, e
		}
		defer release()
		ctx = admitted
	}
	if !mm.ID(selector.MomentID) || !mm.ID(selector.MediaID) || selector.MomentRevision < 2 || selector.SetVersion < 1 || selector.MediaVersion < 1 || (selector.Variant != "display" && selector.Variant != "thumbnail" && selector.Variant != "video") || encode == nil {
		return nil, mm.ErrInvalid
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, a, selector.MomentID)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback(context.Background())
	set, e := s.mediaSetTx(ctx, tx, a, selector.MomentID, false)
	if e != nil {
		return nil, e
	}
	if set.Status != "READY" || set.MomentRevision != selector.MomentRevision || set.SetVersion != selector.SetVersion {
		return nil, mm.ErrNotFound
	}
	var attachment *mm.Attachment
	for i := range set.Attachments {
		if set.Attachments[i].MediaID == selector.MediaID && set.Attachments[i].MediaVersion == selector.MediaVersion {
			attachment = &set.Attachments[i]
		}
	}
	if attachment == nil {
		return nil, mm.ErrNotFound
	}
	var display, thumbnail []byte
	e = tx.QueryRow(ctx, `SELECT display,thumbnail FROM public_moment_media_attachments WHERE moment_id=$1 AND set_version=$2 AND media_id=$3 AND media_version=$4`, selector.MomentID, selector.SetVersion, selector.MediaID, selector.MediaVersion).Scan(&display, &thumbnail)
	if e != nil {
		return nil, mm.ErrUnavailable
	}
	raw := display
	if selector.Variant == "thumbnail" {
		raw = thumbnail
	}
	if selector.Variant == "video" {
		if attachment.Kind != "VIDEO" || attachment.Video == nil {
			return nil, mm.ErrNotFound
		}
		if e = tx.QueryRow(ctx, `SELECT content FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=$2 AND position=$3 AND media_id=$4 AND media_version=$5 AND content IS NOT NULL`, selector.MomentID, selector.SetVersion, attachment.Position, selector.MediaID, selector.MediaVersion).Scan(&raw); e != nil {
			return nil, mm.ErrNotFound
		}
	}
	out, e := encode(*attachment, raw)
	if e != nil {
		return nil, e
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, a, frame, func() error {
		next, e := s.mediaSetTx(ctx, tx, a, selector.MomentID, false)
		if e != nil {
			return e
		}
		return compareMediaSets(set, next)
	}); e != nil {
		return nil, e
	}
	return out, nil
}
