package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	mm "github.com/birdtie/birdtie/apps/api/internal/momentmedia"
	"github.com/jackc/pgx/v5"
)

var _ mm.VideoStore = (*Store)(nil)

func (s *Store) ConfigureMomentMediaVideo(codec *mm.FFmpegVideoNormalizer) error {
	if codec.Validate() != nil {
		return mm.ErrUnavailable
	}
	copy := *codec
	if !s.momentVideo.CompareAndSwap(nil, &copy) {
		return mm.ErrChanged
	}
	return nil
}
func (s *Store) MomentMediaVideoAvailable() bool { return s != nil && s.momentVideo.Load() != nil }

func (s *Store) PrepareMomentMediaVideo(ctx context.Context, a mm.Access, in mm.VideoStageInput) (mm.StageReceipt, error) {
	if !s.MomentMediaVideoAvailable() {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if !mm.ID(in.OperationID) || !mm.ID(in.LocalScope) || in.Purpose != mm.VideoPurpose || in.ContentType != "video/mp4" || len(in.Content) < 24 || len(in.Content) > mm.MaxVideoBytes {
		return mm.StageReceipt{}, mm.ErrInvalid
	}
	ctx, release, e := mm.TryVideoWork(ctx)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	defer release()
	ctx, f, e := s.beginMomentMedia(ctx, a, "", in.OperationID)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	defer f.tx.Rollback(context.Background())
	digest := mm.VideoStageDigest(in)
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
	if _, e = f.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,155))`, a.AccountID+":video-stage-quota"); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	var count int
	if e = f.tx.QueryRow(ctx, `SELECT count(*) FROM moment_media_video_materials v JOIN moment_media_materials m ON m.id=v.media_id WHERE m.owner_account_id=$1 AND m.retain_until>clock_timestamp()`, a.AccountID).Scan(&count); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if count >= 20 {
		return mm.StageReceipt{}, mm.ErrChanged
	}
	d, e := s.momentVideo.Load().Normalize(ctx, in)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	now, _, e := s.humanWriteNow(ctx, f, nil)
	if e != nil {
		return mm.StageReceipt{}, e
	}
	var id string
	if e = f.tx.QueryRow(ctx, `SELECT gen_random_uuid()`).Scan(&id); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	contract := d.Contract
	contract.Path = mm.VideoStagePath(id, 1, "video")
	stage := mm.Stage{SchemaVersion: "public-moment-media-stage-v2", Kind: "VIDEO", Purpose: mm.VideoPurpose, Video: &contract, MediaID: id, Version: 1, LocalScope: in.LocalScope, DisplaySHA: d.DisplaySHA, DisplayBytes: len(d.Display), Width: d.Width, Height: d.Height, ThumbnailSHA: d.ThumbnailSHA, ThumbnailBytes: len(d.Thumbnail), ValidationKind: mm.VideoValidation, ValidationStatus: "VALIDATED", AntivirusScan: "NOT_RUN", PrivacyRisk: "UNKNOWN", PreparedAt: now, RetainUntil: now.Add(mm.StageTTL)}
	if !mm.StageValid(stage) {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_media_materials(id,owner_account_id,local_scope,display,thumbnail,metadata,prepared_at,retain_until)VALUES($1,$2,$3,$4,$5,$6,$7,$8)`, id, a.AccountID, in.LocalScope, d.Display, d.Thumbnail, mediaJSON(stage), stage.PreparedAt, stage.RetainUntil); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_media_video_materials(media_id,content,sha256,contract)VALUES($1,$2,$3,$4)`, id, d.Video, contract.SHA256, mediaJSON(contract)); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	r := mm.StageReceipt{OperationID: in.OperationID, Status: "SUCCEEDED", PayloadDigest: digest, Material: &stage, CommittedAt: now}
	if _, e = f.tx.Exec(ctx, `INSERT INTO moment_media_prepare_operations(actor_account_id,operation_id,receipt)VALUES($1,$2,$3)`, a.AccountID, in.OperationID, mediaJSON(r)); e != nil {
		return mm.StageReceipt{}, mm.ErrUnavailable
	}
	if e = s.humanWriteFinish(ctx, f, func() error {
		var current []byte
		var currentHash string
		if e = f.tx.QueryRow(ctx, `SELECT metadata,encode(sha256(v.content),'hex') FROM moment_media_materials m JOIN moment_media_video_materials v ON v.media_id=m.id WHERE m.id=$1 AND m.owner_account_id=$2 AND m.retain_until>clock_timestamp()`, id, a.AccountID).Scan(&current, &currentHash); e != nil {
			return mm.ErrChanged
		}
		var parsed mm.Stage
		if json.Unmarshal(current, &parsed) != nil || mm.Digest(parsed) != mm.Digest(stage) || currentHash != contract.SHA256 {
			return mm.ErrChanged
		}
		return nil
	}); e != nil {
		return mm.StageReceipt{}, e
	}
	return r, nil
}

func videoStageMaterialTx(ctx context.Context, tx pgx.Tx, stage mm.Stage) ([]byte, string, error) {
	var clip, raw []byte
	var stamp string
	e := tx.QueryRow(ctx, `SELECT content,contract,xmin::text FROM moment_media_video_materials WHERE media_id=$1 FOR SHARE`, stage.MediaID).Scan(&clip, &raw, &stamp)
	if e != nil {
		return nil, "", mm.ErrChanged
	}
	var contract mm.VideoContract
	if json.Unmarshal(raw, &contract) != nil || stage.Video == nil || mm.Digest(contract) != mm.Digest(stage.Video) || len(clip) != contract.Bytes || mm.DigestBytes(clip) != contract.SHA256 {
		return nil, "", mm.ErrChanged
	}
	return clip, mm.Digest([]any{stamp, contract}), nil
}

func publicVideoBindingTx(ctx context.Context, tx pgx.Tx, a mm.Attachment, moment string, set int64) (string, error) {
	var raw []byte
	var actualHash, stamp string
	var count int
	e := tx.QueryRow(ctx, `SELECT contract,encode(sha256(content),'hex'),octet_length(content),xmin::text FROM public_moment_media_video_payloads WHERE moment_id=$1 AND set_version=$2 AND position=$3 AND media_id=$4 AND media_version=$5 AND content IS NOT NULL`, moment, set, a.Position, a.MediaID, a.MediaVersion).Scan(&raw, &actualHash, &count, &stamp)
	if e != nil {
		return "", mm.ErrChanged
	}
	var c mm.VideoContract
	if json.Unmarshal(raw, &c) != nil || a.Video == nil || mm.Digest(c) != mm.Digest(a.Video) || actualHash != c.SHA256 || count != c.Bytes {
		return "", mm.ErrChanged
	}
	return mm.Digest([]any{stamp, c}), nil
}

func (s *Store) ReadMomentVideoStageContent(ctx context.Context, a mm.Access, id string, version int64, variant string, encode func(mm.Stage, []byte) ([]byte, error)) ([]byte, error) {
	if !mm.ID(id) || version != 1 || (variant != "display" && variant != "thumbnail" && variant != "video") || encode == nil {
		return nil, mm.ErrInvalid
	}
	ctx, release, e := mm.TryVideoWork(ctx)
	if e != nil {
		return nil, e
	}
	defer release()
	ctx, f, e := s.beginMomentMedia(ctx, a, "", "")
	if e != nil {
		return nil, e
	}
	defer f.tx.Rollback(context.Background())
	var raw, display, thumbnail []byte
	var stamp string
	e = f.tx.QueryRow(ctx, `SELECT metadata,display,thumbnail,xmin::text FROM moment_media_materials WHERE id=$1 AND owner_account_id=$2 AND version=$3 AND retain_until>clock_timestamp() FOR SHARE`, id, a.AccountID, version).Scan(&raw, &display, &thumbnail, &stamp)
	if errors.Is(e, pgx.ErrNoRows) {
		return nil, mm.ErrNotFound
	}
	if e != nil {
		return nil, mm.ErrUnavailable
	}
	var stage mm.Stage
	if json.Unmarshal(raw, &stage) != nil || !mm.StageValid(stage) || stage.Kind != "VIDEO" || stage.MediaID != id || stage.Version != version {
		return nil, mm.ErrChanged
	}
	clip, videoStamp, e := videoStageMaterialTx(ctx, f.tx, stage)
	if e != nil {
		return nil, e
	}
	body := display
	if variant == "thumbnail" {
		body = thumbnail
	} else if variant == "video" {
		body = clip
	}
	out, e := encode(stage, body)
	if e != nil {
		return nil, e
	}
	if e = s.humanWriteFinish(ctx, f, func() error {
		var current string
		var at time.Time
		if e = f.tx.QueryRow(ctx, `SELECT xmin::text,clock_timestamp() FROM moment_media_materials WHERE id=$1 AND owner_account_id=$2 AND retain_until>clock_timestamp()`, id, a.AccountID).Scan(&current, &at); e != nil || current != stamp || !stage.RetainUntil.After(at) {
			return mm.ErrChanged
		}
		_, next, e := videoStageMaterialTx(ctx, f.tx, stage)
		if e != nil || next != videoStamp {
			return mm.ErrChanged
		}
		return nil
	}); e != nil {
		return nil, e
	}
	return out, nil
}
