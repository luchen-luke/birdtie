package postgres

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
	"sort"
	"time"
)

var _ mm.Store = (*Store)(nil)

func mediaActor(a mm.Access) identity.Actor {
	return identity.Actor{ID: a.AccountID, AccountType: a.AccountType}
}
func (s *Store) beginMomentMedia(ctx context.Context, a mm.Access, moment, op string) (context.Context, humanWriteFrame, error) {
	ctx, original, e := s.humanWriteBound(ctx, a.SessionDigest, mediaActor(a))
	f := humanWriteFrame{a: mediaActor(a), d: a.SessionDigest, session: original, family: "MOMENT_MEDIA", op: op}
	if e != nil {
		return ctx, f, e
	}
	if (moment != "" && !mm.ID(moment)) || (op != "" && !mm.ID(op)) {
		return ctx, f, mm.ErrInvalid
	}
	f.tx, e = s.pool.Begin(ctx)
	if e != nil {
		return ctx, f, mm.ErrUnavailable
	}
	fail := func(e error) (context.Context, humanWriteFrame, error) {
		f.tx.Rollback(context.Background())
		return ctx, f, e
	}
	if _, e = f.tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(mm.ErrUnavailable)
	}
	if op != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,152))`, a.AccountID+":media-op:"+op); e != nil {
			return fail(mm.ErrUnavailable)
		}
	}
	var owner string
	if e = f.tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.AccountID).Scan(&owner); e != nil {
		return fail(mm.ErrChanged)
	}
	if moment != "" {
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,152))`, "moment-media:"+moment); e != nil {
			return fail(mm.ErrUnavailable)
		}
		if e = f.tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 AND author_account_id=$2 FOR SHARE`, moment, a.AccountID).Scan(&owner); errors.Is(e, pgx.ErrNoRows) {
			return fail(mm.ErrForbidden)
		} else if e != nil {
			return fail(mm.ErrUnavailable)
		}
		if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, "moment-photo-slot:"+moment); e != nil {
			return fail(mm.ErrUnavailable)
		}
	}
	return ctx, f, nil
}
func mediaJSON(value any) []byte { b, _ := json.Marshal(value); return b }
func stageReceiptTx(ctx context.Context, f humanWriteFrame) (mm.StageReceipt, error) {
	var r mm.StageReceipt
	var b []byte
	e := f.tx.QueryRow(ctx, `SELECT receipt FROM moment_media_prepare_operations WHERE actor_account_id=$1 AND operation_id=$2`, f.a.ID, f.op).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	return r, e
}
func mediaReceiptTx(ctx context.Context, f humanWriteFrame, moment string) (mm.Receipt, error) {
	var r mm.Receipt
	var b []byte
	e := f.tx.QueryRow(ctx, `SELECT receipt FROM public_moment_media_operations WHERE actor_account_id=$1 AND operation_id=$2 AND moment_id=$3`, f.a.ID, f.op, moment).Scan(&b)
	if e == nil {
		e = json.Unmarshal(b, &r)
	}
	return r, e
}
func (s *Store) PrepareMomentMedia(ctx context.Context, a mm.Access, in mm.StageInput) (mm.StageReceipt, error) {
	ctx, f, e := s.beginMomentMedia(ctx, a, "", in.OperationID)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := mm.StageDigest(in)
	if old, e := stageReceiptTx(ctx, f); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return old, mm.ErrChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return old, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	d, e := mm.Prepare(ctx, in, (&media.LocalImageDerivativeBuilder{}).BuildPublic)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	var count int
	if e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_media_materials WHERE owner_account_id=$1 AND retain_until>clock_timestamp()`, a.AccountID).Scan(&count); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if count >= 36 {
		return mm.StageReceipt{}, mm.ErrChanged
	}
	now, _, e := s.humanWriteNow(ctx, f, nil)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	var id string
	if e = f.tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	material := mm.Stage{MediaID: id, Version: 1, LocalScope: in.LocalScope, DisplaySHA: d.DisplaySHA, DisplayBytes: len(d.Display), Width: d.Width, Height: d.Height, ThumbnailSHA: d.ThumbnailSHA, ThumbnailBytes: len(d.Thumbnail), ValidationKind: mm.ValidationKind, ValidationStatus: "VALIDATED", AntivirusScan: "NOT_RUN", PrivacyRisk: "UNKNOWN", PreparedAt: now, RetainUntil: now.Add(mm.StageTTL)}
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_media_materials(id,owner_account_id,local_scope,display,thumbnail,metadata,prepared_at,retain_until)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, a.AccountID, in.LocalScope, d.Display, d.Thumbnail, mediaJSON(material), now, material.RetainUntil); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	r := mm.StageReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", PayloadDigest: digest, Material: &material, CommittedAt: now}
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_media_prepare_operations(actor_account_id,operation_id,receipt)VALUES($1,$2,$3)`, a.AccountID, in.OperationID, mediaJSON(r)); e != nil {
		return r, mm.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Store) ReadMomentMediaPreparation(ctx context.Context, a mm.Access, op string, stop bool) (mm.StageReceipt, error) {
	ctx, f, e := s.beginMomentMedia(ctx, a, "", op)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := stageReceiptTx(ctx, f)
	if errors.Is(e, pgx.ErrNoRows) && stop {
		now, _, er := s.humanWriteNow(ctx, f, nil)
		if er != nil {
			return r, er
		}
		r = mm.StageReceipt{OperationID: op, Status: "STOPPED", CommittedAt: now}
		_, e = f.tx.Exec(ctx, `INSERT INTO moment_media_prepare_operations(actor_account_id,operation_id,receipt)VALUES($1,$2,$3)`, a.AccountID, op, mediaJSON(r))
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return r, mm.ErrNotFound
	}
	if e != nil {
		return r, mm.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return r, e
	}
	return r, nil
}

type momentMediaMaterial struct {
	value              mm.Stage
	display, thumbnail []byte
	video              []byte
	stamp              string
}

func mediaMaterialsTx(ctx context.Context, tx pgx.Tx, owner string, inputs []mm.AttachmentInput) ([]momentMediaMaterial, string, time.Time, error) {
	values := make([]momentMediaMaterial, len(inputs))
	ids := make([]string, len(inputs))
	for i, v := range inputs {
		ids[i] = v.MediaID
	}
	sort.Strings(ids)
	// Lock in canonical ID order; output order is still the author's attachment order.
	for _, id := range ids {
		var locked string
		e := tx.QueryRow(ctx, `SELECT id FROM moment_media_materials WHERE id=$1 AND owner_account_id=$2 FOR SHARE`, id, owner).Scan(&locked)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, "", time.Time{}, mm.ErrChanged
		}
		if e != nil {
			return nil, "", time.Time{}, mm.ErrUnavailable
		}
	}
	proof := []any{}
	var deadline time.Time
	for i, in := range inputs {
		var b []byte
		v := &values[i]
		var until time.Time
		e := tx.QueryRow(ctx, `SELECT metadata,display,thumbnail,xmin::text,retain_until FROM moment_media_materials WHERE id=$1 AND owner_account_id=$2 AND version=$3 AND retain_until>clock_timestamp()`, in.MediaID, owner, in.MediaVersion).Scan(&b, &v.display, &v.thumbnail, &v.stamp, &until)
		if errors.Is(e, pgx.ErrNoRows) {
			return nil, "", deadline, mm.ErrChanged
		}
		if e != nil || json.Unmarshal(b, &v.value) != nil || !mm.StageValid(v.value) || v.value.MediaID != in.MediaID || v.value.Version != in.MediaVersion || mm.DigestBytes(v.display) != v.value.DisplaySHA || mm.DigestBytes(v.thumbnail) != v.value.ThumbnailSHA {
			return nil, "", deadline, mm.ErrUnavailable
		}
		proof = append(proof, []any{in.MediaID, in.MediaVersion, v.stamp, v.value.DisplaySHA, v.value.ThumbnailSHA, in.Rights.Digest()})
		if v.value.Kind == "VIDEO" {
			if len(inputs) != 1 {
				return nil, "", deadline, mm.ErrInvalid
			}
			var videoProof string
			v.video, videoProof, e = videoStageMaterialTx(ctx, tx, v.value)
			if e != nil {
				return nil, "", deadline, e
			}
			proof = append(proof, []any{"VIDEO", v.value.Video, videoProof})
		}
		if deadline.IsZero() || until.Before(deadline) {
			deadline = until
		}
		if in.Rights.RetentionUntil.Before(deadline) {
			deadline = in.Rights.RetentionUntil
		}
	}
	return values, mm.Digest(proof), deadline, nil
}

type momentMediaSource struct {
	parent                         momentMediaParent
	version                        int64
	status, slotStamp, legacyStamp string
	materials                      []momentMediaMaterial
	materialProof, stamp           string
	deadline                       time.Time
}

func (s *Store) mediaSourceTx(ctx context.Context, tx pgx.Tx, a mm.Access, moment string, in mm.Input) (momentMediaSource, error) {
	var src momentMediaSource
	var e error
	src.parent, e = momentMediaParentTx(ctx, tx, a, moment)
	if e != nil {
		return src, e
	}
	if src.parent.owner != a.AccountID || src.parent.revision != in.ExpectedMomentRevision {
		return src, mm.ErrChanged
	}
	e = tx.QueryRow(ctx, `SELECT version,status,xmin::text FROM public_moment_media_sets WHERE moment_id=$1 FOR UPDATE`, moment).Scan(&src.version, &src.status, &src.slotStamp)
	if errors.Is(e, pgx.ErrNoRows) {
		src.version = 0
		src.status = "EMPTY"
		src.slotStamp = "none"
	} else if e != nil {
		return src, mm.ErrUnavailable
	}
	if src.version != in.ExpectedSetVersion || in.Action == "DELETE" && src.status != "READY" {
		return src, mm.ErrChanged
	}
	if in.Action == "DELETE" {
		var body string
		if e = tx.QueryRow(ctx, `SELECT body FROM moments WHERE id=$1`, moment).Scan(&body); e != nil {
			return src, mm.ErrUnavailable
		}
		// A person must first add text or explicitly withdraw a media-only
		// publication. Expiry cleanup is independent and can still retire bytes.
		if !content.HasMomentBody(body) {
			return src, mm.ErrChanged
		}
	}
	src.deadline = src.parent.deadline
	matProof := ""
	if in.Action == "REPLACE" {
		var until time.Time
		src.materials, matProof, until, e = mediaMaterialsTx(ctx, tx, a.AccountID, in.Attachments)
		if e != nil {
			return src, e
		}
		if until.Before(src.deadline) {
			src.deadline = until
		}
	}
	e = tx.QueryRow(ctx, `SELECT jsonb_build_array(photo_id,version,status,xmin::text)::text FROM public_moment_photos WHERE moment_id=$1`, moment).Scan(&src.legacyStamp)
	if errors.Is(e, pgx.ErrNoRows) {
		src.legacyStamp = "none"
	} else if e != nil {
		return src, mm.ErrUnavailable
	}
	src.materialProof = matProof
	src.stamp = mm.Digest([]any{src.parent.binding, src.version, src.slotStamp, src.legacyStamp, matProof})
	return src, nil
}

type MomentMediaMaterialSnapshot struct {
	Materials     []mm.Stage
	Thumbnails    [][]byte
	SourceVersion string
	ValidUntil    time.Time
}

// Lock the whole thread's material set once, before its per-entry captures.
func (s *Store) LockMomentMediaMaterialsInTx(ctx context.Context, tx pgx.Tx, a mm.Access, mediaIDs []string) error {
	if len(mediaIDs) < 1 || len(mediaIDs) > 180 {
		return mm.ErrInvalid
	}
	original, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, mediaActor(a))
	if !present || !valid {
		return mm.ErrChanged
	}
	current, _, e := s.humanWriteSession(ctx, tx, a.SessionDigest, mediaActor(a), false)
	if e != nil || current != original {
		return mm.ErrChanged
	}
	unique := map[string]bool{}
	for _, id := range mediaIDs {
		if !mm.ID(id) {
			return mm.ErrInvalid
		}
		unique[id] = true
	}
	ids := make([]string, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		var found string
		e := tx.QueryRow(ctx, `SELECT id FROM moment_media_materials WHERE id=$1 AND owner_account_id=$2 AND retain_until>clock_timestamp() FOR SHARE`, id, a.AccountID).Scan(&found)
		if errors.Is(e, pgx.ErrNoRows) {
			return mm.ErrChanged
		}
		if e != nil {
			return mm.ErrUnavailable
		}
	}
	return nil
}

// CaptureMomentMediaMaterialsInTx binds owned staging selectors and per-image
// rights for a future parent. It reads no Moment and writes no publication.
func (s *Store) CaptureMomentMediaMaterialsInTx(ctx context.Context, tx pgx.Tx, a mm.Access, ordered []mm.AttachmentInput) (MomentMediaMaterialSnapshot, error) {
	var out MomentMediaMaterialSnapshot
	original, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, mediaActor(a))
	if !present || !valid {
		return out, mm.ErrChanged
	}
	current, _, e := s.humanWriteSession(ctx, tx, a.SessionDigest, mediaActor(a), false)
	if e != nil || current != original {
		return out, mm.ErrChanged
	}
	in := mm.Input{OperationID: "00000000-0000-4000-8000-000000000152", Action: "REPLACE", ExpectedMomentRevision: 2, Attachments: ordered}
	if mm.ValidateInput(in, false, time.Now().UTC()) != nil {
		return out, mm.ErrInvalid
	}
	values, proof, until, e := mediaMaterialsTx(ctx, tx, a.AccountID, ordered)
	if e != nil {
		return out, e
	}
	out = MomentMediaMaterialSnapshot{Materials: []mm.Stage{}, Thumbnails: [][]byte{}, SourceVersion: proof, ValidUntil: until}
	for _, v := range values {
		out.Materials = append(out.Materials, v.value)
		out.Thumbnails = append(out.Thumbnails, v.thumbnail)
	}
	return out, nil
}
func (s *Store) ReadOwnMomentMedia(ctx context.Context, a mm.Access, moment string) (mm.Set, error) {
	ctx, f, e := s.beginMomentMedia(ctx, a, moment, "")
	if e != nil {
		return mm.Set{}, e
	}
	defer f.tx.Rollback(context.Background())
	v, e := s.mediaSetTx(ctx, f.tx, a, moment, true)
	if e != nil {
		return v, e
	}
	old := v
	if e = s.humanWriteFinish(ctx, f, func() error {
		next, e := s.mediaSetTx(ctx, f.tx, a, moment, true)
		if e != nil {
			return e
		}
		return compareMediaSets(old, next)
	}); e != nil {
		return v, e
	}
	v.OriginalFrame = f.session
	v.ResponseProof = &mediaSetProof{s, a, v}
	return v, nil
}

type mediaPreviewProof struct {
	s      *Store
	a      mm.Access
	moment string
	in     mm.Input
	source string
	until  time.Time
	frame  identity.HumanControlFrame
}

func (p *mediaPreviewProof) RevalidateHumanControl(ctx context.Context) error {
	ctx = identity.WithHumanControlFrame(ctx, p.a.SessionDigest, mediaActor(p.a), p.frame)
	ctx, f, e := p.s.beginMomentMedia(ctx, p.a, p.moment, p.in.OperationID)
	if e != nil {
		return e
	}
	defer f.tx.Rollback(context.Background())
	return p.s.humanWriteFinish(ctx, f, func() error {
		src, e := p.s.mediaSourceTx(ctx, f.tx, p.a, p.moment, p.in)
		if e != nil {
			return e
		}
		if src.stamp != p.source || src.deadline.Before(p.until) || !p.until.After(time.Now().UTC()) {
			return mm.ErrChanged
		}
		return nil
	})
}
func (s *Store) PreviewMomentMedia(ctx context.Context, a mm.Access, moment string, in mm.Input) (mm.Preview, error) {
	if mm.ValidateInput(in, false, time.Now().UTC()) != nil {
		return mm.Preview{}, mm.ErrInvalid
	}
	ctx, f, e := s.beginMomentMedia(ctx, a, moment, in.OperationID)
	if e != nil {
		return mm.Preview{}, e
	}
	defer f.tx.Rollback(context.Background())
	if _, e = mediaReceiptTx(ctx, f, moment); e == nil {
		return mm.Preview{}, mm.ErrChanged
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return mm.Preview{}, mm.ErrUnavailable
	}
	src, e := s.mediaSourceTx(ctx, f.tx, a, moment, in)
	if e != nil {
		return mm.Preview{}, e
	}
	base, e := s.humanWritePreview(ctx, f, moment, in.Action, mm.InputDigest(moment, in), src.stamp, &src.deadline)
	if e != nil {
		return mm.Preview{}, e
	}
	v := mm.Preview{HumanControlPreview: base, MomentID: moment, ExpectedMomentRevision: in.ExpectedMomentRevision, ExpectedSetVersion: in.ExpectedSetVersion, SourceVersion: src.stamp, Images: []mm.PreviewImage{}}
	for i, material := range src.materials {
		v.Images = append(v.Images, mm.PreviewImage{AttachmentInput: in.Attachments[i], Stage: material.value, ThumbnailBase64: base64.StdEncoding.EncodeToString(material.thumbnail)})
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, base.ExpiresAt, func() error {
		next, e := s.mediaSourceTx(ctx, f.tx, a, moment, in)
		if e != nil {
			return e
		}
		if next.stamp != src.stamp || next.deadline.Before(base.ExpiresAt) {
			return mm.ErrChanged
		}
		return nil
	})); e != nil {
		return v, e
	}
	v.ResponseProof = &mediaPreviewProof{s, a, moment, in, src.stamp, base.ExpiresAt, f.session}
	return v, nil
}

// AttachMomentMediaInTx is an INTERNAL composition helper for atomic threads.
// Caller must bind this exact ordered material and rights in its own approval,
// revalidate all source proofs, and finish the original HumanControl transaction.
// It never starts/commits a transaction or grants publication by itself.
func (s *Store) AttachMomentMediaInTx(ctx context.Context, tx pgx.Tx, a mm.Access, moment string, revision, expected int64, ordered []mm.AttachmentInput) (mm.Set, error) {
	original, valid, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, mediaActor(a))
	if !present || !valid {
		return mm.Set{}, mm.ErrChanged
	}
	current, _, e := s.humanWriteSession(ctx, tx, a.SessionDigest, mediaActor(a), false)
	if e != nil || current != original {
		return mm.Set{}, mm.ErrChanged
	}
	in := mm.Input{OperationID: "00000000-0000-4000-8000-000000000152", Action: "REPLACE", ExpectedMomentRevision: revision, ExpectedSetVersion: expected, Attachments: ordered}
	if mm.ValidateInput(in, false, time.Now().UTC()) != nil {
		return mm.Set{}, mm.ErrInvalid
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,152))`, "moment-media:"+moment); e != nil {
		return mm.Set{}, mm.ErrUnavailable
	}
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,145))`, "moment-photo-slot:"+moment); e != nil {
		return mm.Set{}, mm.ErrUnavailable
	}
	src, e := s.mediaSourceTx(ctx, tx, a, moment, in)
	if e != nil {
		return mm.Set{}, e
	}
	return s.applyMomentMediaTx(ctx, tx, a, moment, in, src)
}
func (s *Store) applyMomentMediaTx(ctx context.Context, tx pgx.Tx, a mm.Access, moment string, in mm.Input, src momentMediaSource) (mm.Set, error) {
	version := in.ExpectedSetVersion + 1
	status := "READY"
	if in.Action == "DELETE" {
		status = "DELETED"
	}
	if _, e := tx.Exec(ctx, `UPDATE public_moment_photos SET thumbnail=NULL,sha256=NULL,status='DELETED',version=least(version+1,8999999999999999),updated_at=clock_timestamp() WHERE moment_id=$1 AND status='READY'`, moment); e != nil {
		return mm.Set{}, mm.ErrUnavailable
	}
	if _, e := tx.Exec(ctx, `UPDATE public_moment_media_attachments SET display=NULL,thumbnail=NULL WHERE moment_id=$1 AND display IS NOT NULL`, moment); e != nil {
		return mm.Set{}, mm.ErrUnavailable
	}
	if _, e := tx.Exec(ctx, `INSERT INTO public_moment_media_sets(moment_id,owner_account_id,version,approved_moment_revision,status,source_binding)VALUES($1,$2,$3,$4,$5,$6)ON CONFLICT(moment_id)DO UPDATE SET version=EXCLUDED.version,approved_moment_revision=EXCLUDED.approved_moment_revision,status=EXCLUDED.status,source_binding=EXCLUDED.source_binding,updated_at=clock_timestamp()`, moment, a.AccountID, version, in.ExpectedMomentRevision, status, src.parent.binding); e != nil {
		return mm.Set{}, mm.ErrUnavailable
	}
	for i, m := range src.materials {
		rights := in.Attachments[i].Rights
		meta := mediaAttachment(moment, in.ExpectedMomentRevision, version, i, m.value, rights, src.deadline)
		if _, e := tx.Exec(ctx, `INSERT INTO public_moment_media_attachments(moment_id,set_version,position,media_id,media_version,metadata,rights,retention_until,display,thumbnail)VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)`, moment, version, i, m.value.MediaID, m.value.Version, mediaJSON(meta), mediaJSON(rights), rights.RetentionUntil, m.display, m.thumbnail); e != nil {
			return mm.Set{}, mm.ErrUnavailable
		}
		if m.value.Kind == "VIDEO" {
			if _, e := tx.Exec(ctx, `INSERT INTO public_moment_media_video_payloads(moment_id,set_version,position,media_id,media_version,content,sha256,contract)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, moment, version, i, m.value.MediaID, m.value.Version, m.video, meta.Video.SHA256, mediaJSON(meta.Video)); e != nil {
				return mm.Set{}, mm.ErrUnavailable
			}
		}
	}
	return s.mediaSetTx(ctx, tx, a, moment, true)
}
func (s *Store) CommitMomentMedia(ctx context.Context, a mm.Access, moment string, in mm.Input) (mm.Receipt, error) {
	if mm.ValidateInput(in, true, time.Now().UTC()) != nil {
		return mm.Receipt{}, mm.ErrInvalid
	}
	ctx, f, e := s.beginMomentMedia(ctx, a, moment, in.OperationID)
	if e != nil {
		return mm.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := mm.InputDigest(moment, in)
	if old, e := mediaReceiptTx(ctx, f, moment); e == nil {
		if old.Status == "SUCCEEDED" && old.PayloadDigest != digest {
			return old, mm.ErrChanged
		}
		if e = s.humanWriteFinish(ctx, f); e != nil {
			return old, e
		}
		return old, nil
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return mm.Receipt{}, mm.ErrUnavailable
	}
	src, e := s.mediaSourceTx(ctx, f.tx, a, moment, in)
	if e != nil {
		return mm.Receipt{}, e
	}
	if src.stamp != in.SourceVersion {
		return mm.Receipt{}, mm.ErrChanged
	}
	until, e := s.humanWriteApprove(ctx, f, moment, in.Action, digest, src.stamp, in.Approval, &src.deadline)
	if e != nil {
		return mm.Receipt{}, e
	}
	set, e := s.applyMomentMediaTx(ctx, f.tx, a, moment, in, src)
	if e != nil {
		return mm.Receipt{}, e
	}
	var now time.Time
	if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return mm.Receipt{}, mm.ErrUnavailable
	}
	r := mm.Receipt{OperationID: in.OperationID, Status: "SUCCEEDED", MomentID: moment, Action: in.Action, ExpectedMomentRevision: in.ExpectedMomentRevision, ExpectedSetVersion: in.ExpectedSetVersion, AppliedSetVersion: set.SetVersion, PayloadDigest: digest, Media: []mm.StageSelector{}, CommittedAt: now}
	for _, m := range in.Attachments {
		r.Media = append(r.Media, mm.StageSelector{MediaID: m.MediaID, Version: m.MediaVersion})
	}
	if _, e = f.tx.Exec(ctx, `INSERT INTO public_moment_media_operations(actor_account_id,operation_id,moment_id,receipt)VALUES($1,$2,$3,$4)`, a.AccountID, in.OperationID, moment, mediaJSON(r)); e != nil {
		return r, mm.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, humanWriteGateDeadline(ctx, f, until, func() error {
		parent, e := momentMediaParentTx(ctx, f.tx, a, moment)
		if e != nil {
			return e
		}
		if parent.binding != src.parent.binding || parent.deadline.Before(until) {
			return mm.ErrChanged
		}
		if in.Action == "REPLACE" {
			_, proof, deadline, e := mediaMaterialsTx(ctx, f.tx, a.AccountID, in.Attachments)
			if e != nil {
				return e
			}
			if proof != src.materialProof || deadline.Before(until) {
				return mm.ErrChanged
			}
		}
		return nil
	})); e != nil {
		return r, e
	}
	return r, nil
}
func (s *Store) ReadMomentMediaOperation(ctx context.Context, a mm.Access, moment, op string, stop bool) (mm.Receipt, error) {
	ctx, f, e := s.beginMomentMedia(ctx, a, moment, op)
	if e != nil {
		return mm.Receipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	r, e := mediaReceiptTx(ctx, f, moment)
	if errors.Is(e, pgx.ErrNoRows) && stop {
		var at time.Time
		if e = f.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&at); e != nil {
			return r, mm.ErrUnavailable
		}
		r = mm.Receipt{OperationID: op, MomentID: moment, Status: "STOPPED", Media: []mm.StageSelector{}, CommittedAt: at}
		_, e = f.tx.Exec(ctx, `INSERT INTO public_moment_media_operations(actor_account_id,operation_id,moment_id,receipt)VALUES($1,$2,$3,$4)`, a.AccountID, op, moment, mediaJSON(r))
	}
	if errors.Is(e, pgx.ErrNoRows) {
		return r, mm.ErrNotFound
	}
	if e != nil {
		return r, mm.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f); e != nil {
		return r, e
	}
	return r, nil
}
