package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const VideoMaterialPurpose = "HUMAN_PRIVATE_VIDEO_MATERIAL"
const MaxVideoBytes int64 = 20 * 1024 * 1024
const MaxVideoDurationMS int64 = 30000
const VideoUploadTTL = 5 * time.Minute
const VideoRetention = 30 * 24 * time.Hour

var ErrVideoUnavailable = errors.New("video provider unavailable")
var ErrVideoInvalid = errors.New("invalid video")

type VideoMaterialInput struct {
	OperationID          string `json:"operationId"`
	MIME                 string `json:"mimeType"`
	ByteSize             int64  `json:"byteSize"`
	SHA256               string `json:"sha256"`
	DurationMS           int64  `json:"durationMs"`
	Purpose              string `json:"purpose"`
	ConfirmedPrivateSave bool   `json:"confirmedPrivateSave"`
	StoragePolicyID      string `json:"storagePolicyId"`
}

func ValidateVideoMaterialInput(in VideoMaterialInput) error {
	if !ValidPrivateImageID(in.OperationID) || in.MIME != "video/mp4" || in.ByteSize < 1 || in.ByteSize > MaxVideoBytes || !privateImageSHA.MatchString(in.SHA256) || !privateImageSHA.MatchString(in.StoragePolicyID) || in.DurationMS < 1 || in.DurationMS > MaxVideoDurationMS || in.Purpose != VideoMaterialPurpose || !in.ConfirmedPrivateSave {
		return content.ErrInvalid
	}
	return nil
}

type VideoMaterialReceipt struct {
	ID      string `json:"id"`
	OwnerID string `json:"ownerAccountId"`
	VideoMaterialInput
	Status          string    `json:"status"`
	Revision        int64     `json:"revision"`
	Width           int       `json:"width"`
	Height          int       `json:"height"`
	FailureCode     string    `json:"failureCode,omitempty"`
	UploadExpiresAt time.Time `json:"uploadExpiresAt"`
	RetainUntil     time.Time `json:"retainUntil"`
}
type VideoMaterialPage struct {
	Items      []VideoMaterialReceipt `json:"items"`
	NextCursor string                 `json:"nextCursor,omitempty"`
}
type VideoMaterialRecord struct {
	Receipt                                      VideoMaterialReceipt
	QuarantineKey, PrivateKey, ApprovalSessionID string
	ApprovalGeneration                           int64
	SessionDigest                                [32]byte
	LeaseID                                      string
	LeaseUntil                                   time.Time
	CleanupPending                               bool
}
type VideoDescriptor struct {
	Width, Height int
	DurationMS    int64
}
type VideoInspector interface {
	Inspect(context.Context, []byte) (VideoDescriptor, error)
}
type VideoPrivateStorage interface {
	Storage
	OpenPrivate(context.Context, string) (io.ReadCloser, ObjectInfo, error)
	RevokeQuarantineWrite(context.Context, string) error
}
type VideoMaterialRepository interface {
	ReserveHumanVideo(context.Context, [32]byte, identity.Actor, VideoMaterialInput) (VideoMaterialRecord, error)
	QueueHumanVideo(context.Context, [32]byte, identity.Actor, string, int64) (VideoMaterialReceipt, error)
	ReadHumanVideo(context.Context, [32]byte, identity.Actor, string, bool, func(VideoMaterialRecord) ([]byte, error)) ([]byte, error)
	ListHumanVideos(context.Context, [32]byte, identity.Actor, string) (VideoMaterialPage, error)
	DeleteHumanVideo(context.Context, [32]byte, identity.Actor, string, int64) (VideoMaterialReceipt, error)
	ClaimVideoJob(context.Context) (VideoMaterialRecord, error)
	VideoJobCurrent(context.Context, VideoMaterialRecord) error
	FinishVideoJob(context.Context, VideoMaterialRecord, VideoDescriptor) (VideoMaterialReceipt, error)
	RejectVideoJob(context.Context, VideoMaterialRecord, string) error
	FinishVideoCleanup(context.Context, VideoMaterialRecord) error
}
type VideoCapabilities struct {
	Available       bool   `json:"available"`
	ProviderLabel   string `json:"providerLabel"`
	Region          string `json:"region"`
	UploadOrigin    string `json:"uploadOrigin"`
	MaxBytes        int64  `json:"maxBytes"`
	MaxDurationMS   int64  `json:"maxDurationMs"`
	MIME            string `json:"mimeType"`
	Purpose         string `json:"purpose"`
	StoragePolicyID string `json:"storagePolicyId"`
}
type VideoRuntime struct {
	Storage                             VideoPrivateStorage
	Scanner                             Scanner
	Inspector                           VideoInspector
	ProviderLabel, Region, UploadOrigin string
}

func (r VideoRuntime) Available() bool {
	return r.Storage != nil && r.Scanner != nil && r.Inspector != nil && r.ProviderLabel != "" && r.Region != "" && r.UploadOrigin != ""
}
func (r VideoRuntime) PolicyID() string {
	if !r.Available() {
		return ""
	}
	backend := ""
	if storage, ok := r.Storage.(*HTTPVideoStorage); ok {
		backend = storage.BaseURL
	}
	h := sha256.Sum256([]byte(r.ProviderLabel + "\x00" + r.Region + "\x00" + r.UploadOrigin + "\x00" + backend))
	return hex.EncodeToString(h[:])
}

type VideoUploadPlan struct {
	Receipt       VideoMaterialReceipt `json:"receipt"`
	UploadURL     string               `json:"uploadUrl,omitempty"`
	UploadHeaders map[string]string    `json:"uploadHeaders,omitempty"`
	ExpiresAt     time.Time            `json:"expiresAt,omitempty"`
}
type VideoMaterialService struct {
	Repo    VideoMaterialRepository
	Runtime VideoRuntime
}

func NewVideoMaterialService(repo VideoMaterialRepository, runtime VideoRuntime) *VideoMaterialService {
	return &VideoMaterialService{repo, runtime}
}
func (s *VideoMaterialService) Capabilities() VideoCapabilities {
	available := s != nil && s.Repo != nil && s.Runtime.Available()
	r := VideoRuntime{}
	if s != nil {
		r = s.Runtime
	}
	return VideoCapabilities{Available: available, ProviderLabel: r.ProviderLabel, Region: r.Region, UploadOrigin: r.UploadOrigin, MaxBytes: MaxVideoBytes, MaxDurationMS: MaxVideoDurationMS, MIME: "video/mp4", Purpose: VideoMaterialPurpose, StoragePolicyID: r.PolicyID()}
}
func (s *VideoMaterialService) Create(ctx context.Context, d [32]byte, a identity.Actor, in VideoMaterialInput) (VideoUploadPlan, error) {
	if e := ValidateVideoMaterialInput(in); e != nil {
		return VideoUploadPlan{}, e
	}
	if !s.Capabilities().Available {
		return VideoUploadPlan{}, ErrVideoUnavailable
	}
	if in.StoragePolicyID != s.Runtime.PolicyID() {
		return VideoUploadPlan{}, content.ErrConflict
	}
	v, e := s.Repo.ReserveHumanVideo(ctx, d, a, in)
	if e != nil {
		return VideoUploadPlan{}, e
	}
	p := VideoUploadPlan{Receipt: v.Receipt}
	if v.Receipt.Status != "quarantine" {
		return p, nil
	}
	if !v.Receipt.UploadExpiresAt.After(time.Now().UTC()) {
		return p, content.ErrConflict
	}
	t, e := s.Runtime.Storage.SignQuarantineWrite(ctx, UploadIntent{ID: v.Receipt.ID, OwnerID: a.ID, ObjectKey: v.QuarantineKey, MIMEType: in.MIME, MaxBytes: in.ByteSize, SHA256Hex: in.SHA256, ExpiresAt: v.Receipt.UploadExpiresAt})
	if e != nil {
		return p, ErrVideoUnavailable
	}
	if t.URL == "" || !t.ExpiresAt.After(time.Now().UTC()) || t.ExpiresAt.After(v.Receipt.UploadExpiresAt) {
		return p, ErrVideoUnavailable
	}
	// Signing never means uploaded or saved. No original or private read URL.
	p.UploadURL = t.URL
	p.UploadHeaders = t.Headers
	p.ExpiresAt = t.ExpiresAt
	return p, nil
}
func (s *VideoMaterialService) Complete(ctx context.Context, d [32]byte, a identity.Actor, id string, rev int64) (VideoMaterialReceipt, error) {
	if s == nil || s.Repo == nil || !s.Runtime.Available() {
		return VideoMaterialReceipt{}, ErrVideoUnavailable
	}
	return s.Repo.QueueHumanVideo(ctx, d, a, id, rev)
}
func (s *VideoMaterialService) List(ctx context.Context, d [32]byte, a identity.Actor, cursor string) (VideoMaterialPage, error) {
	if s == nil || s.Repo == nil {
		return VideoMaterialPage{}, ErrVideoUnavailable
	}
	return s.Repo.ListHumanVideos(ctx, d, a, cursor)
}
func (s *VideoMaterialService) Read(ctx context.Context, d [32]byte, a identity.Actor, id string, operation bool, encode func(VideoMaterialReceipt) ([]byte, error)) ([]byte, error) {
	if s == nil || s.Repo == nil {
		return nil, ErrVideoUnavailable
	}
	return s.Repo.ReadHumanVideo(ctx, d, a, id, operation, func(v VideoMaterialRecord) ([]byte, error) { return encode(v.Receipt) })
}
func (s *VideoMaterialService) Content(ctx context.Context, d [32]byte, a identity.Actor, id string) ([]byte, error) {
	if s == nil || s.Repo == nil || !s.Runtime.Available() {
		return nil, ErrVideoUnavailable
	}
	return s.Repo.ReadHumanVideo(ctx, d, a, id, false, func(v VideoMaterialRecord) ([]byte, error) {
		if v.Receipt.StoragePolicyID != s.Runtime.PolicyID() {
			return nil, ErrVideoUnavailable
		}
		if v.Receipt.Status != "ready_private" {
			return nil, content.ErrNotFound
		}
		r, info, e := s.Runtime.Storage.OpenPrivate(ctx, v.PrivateKey)
		if e != nil {
			return nil, ErrVideoUnavailable
		}
		defer r.Close()
		b, e := io.ReadAll(io.LimitReader(r, MaxVideoBytes+1))
		if e != nil || int64(len(b)) != v.Receipt.ByteSize || info.ByteSize != v.Receipt.ByteSize {
			return nil, ErrVideoUnavailable
		}
		h := sha256.Sum256(b)
		if hex.EncodeToString(h[:]) != v.Receipt.SHA256 {
			return nil, ErrVideoUnavailable
		}
		return b, nil
	})
}
func (s *VideoMaterialService) Delete(ctx context.Context, d [32]byte, a identity.Actor, id string, rev int64) (VideoMaterialReceipt, error) {
	if s == nil || s.Repo == nil {
		return VideoMaterialReceipt{}, ErrVideoUnavailable
	}
	return s.Repo.DeleteHumanVideo(ctx, d, a, id, rev)
}

// This worker is the only route to ready_private. Every uncertain provider,
// decoder and scanner result fails closed, while tombstones fence late work.
func (s *VideoMaterialService) ProcessNext(ctx context.Context) (bool, error) {
	// A claimed operation cannot outlive the deletion cleanup grace. Provider
	// adapters must acknowledge completed writes synchronously and idempotently.
	ctx, cancel := context.WithTimeout(ctx, 150*time.Second)
	defer cancel()
	if !s.Capabilities().Available {
		return false, ErrVideoUnavailable
	}
	v, e := s.Repo.ClaimVideoJob(ctx)
	if errors.Is(e, content.ErrNotFound) {
		return false, nil
	}
	if e != nil {
		return false, e
	}
	if v.CleanupPending {
		if v.Receipt.StoragePolicyID != s.Runtime.PolicyID() {
			return true, ErrVideoUnavailable
		}
		if e = s.Runtime.Storage.RevokeQuarantineWrite(ctx, v.Receipt.ID); e != nil {
			return true, ErrVideoUnavailable
		}
		if e = s.Runtime.Storage.DeleteQuarantine(ctx, v.QuarantineKey); e != nil {
			return true, ErrVideoUnavailable
		}
		if v.Receipt.Status != "ready_private" {
			if e = s.Runtime.Storage.DeletePrivate(ctx, v.PrivateKey); e != nil {
				return true, ErrVideoUnavailable
			}
		}
		return true, s.Repo.FinishVideoCleanup(ctx, v)
	}
	if v.Receipt.StoragePolicyID != s.Runtime.PolicyID() {
		return true, s.Repo.RejectVideoJob(ctx, v, "provider_changed")
	}
	reject := func(code string, e error) (bool, error) {
		if failure := s.Repo.RejectVideoJob(ctx, v, code); failure != nil {
			return true, failure
		}
		return true, e
	}
	if e = s.Repo.VideoJobCurrent(ctx, v); e != nil {
		return reject("source_retired", e)
	}
	r, info, e := s.Runtime.Storage.OpenQuarantine(ctx, v.QuarantineKey)
	if e != nil {
		return reject("storage_unavailable", ErrVideoUnavailable)
	}
	b, e := io.ReadAll(io.LimitReader(r, MaxVideoBytes+1))
	r.Close()
	if e != nil || int64(len(b)) != v.Receipt.ByteSize || info.ByteSize != v.Receipt.ByteSize || int64(len(b)) > MaxVideoBytes {
		return reject("invalid_bytes", ErrVideoInvalid)
	}
	h := sha256.Sum256(b)
	if hex.EncodeToString(h[:]) != v.Receipt.SHA256 {
		return reject("checksum_mismatch", ErrVideoInvalid)
	}
	if e = ValidateMP4Signature(b); e != nil {
		return reject("invalid_mp4", e)
	}
	descriptor, e := s.Runtime.Inspector.Inspect(ctx, b)
	if e != nil {
		return reject("decode_invalid", ErrVideoInvalid)
	}
	if descriptor.DurationMS < 1 || descriptor.DurationMS > MaxVideoDurationMS || descriptor.Width < 1 || descriptor.Height < 1 || descriptor.Width > 1920 || descriptor.Height > 1920 || int64(descriptor.Width)*int64(descriptor.Height) > 1920*1080 || absVideo(descriptor.DurationMS-v.Receipt.DurationMS) > 250 {
		return reject("video_limit", ErrVideoInvalid)
	}
	if e = s.Repo.VideoJobCurrent(ctx, v); e != nil {
		return reject("source_retired", e)
	}
	verdict, e := s.Runtime.Scanner.Scan(ctx, bytes.NewReader(b))
	if e != nil || verdict != ScanClean {
		return reject("scan_rejected", ErrVideoInvalid)
	}
	if e = s.Repo.VideoJobCurrent(ctx, v); e != nil {
		return reject("source_retired", e)
	}
	if e = s.Runtime.Storage.PutPrivate(ctx, v.PrivateKey, bytes.NewReader(b)); e != nil {
		return reject("storage_unavailable", ErrVideoUnavailable)
	}
	private, copyInfo, e := s.Runtime.Storage.OpenPrivate(ctx, v.PrivateKey)
	if e != nil {
		return reject("storage_unavailable", ErrVideoUnavailable)
	}
	copyBytes, copyError := io.ReadAll(io.LimitReader(private, MaxVideoBytes+1))
	private.Close()
	copyHash := sha256.Sum256(copyBytes)
	if copyError != nil || copyInfo.ByteSize != v.Receipt.ByteSize || int64(len(copyBytes)) != v.Receipt.ByteSize || hex.EncodeToString(copyHash[:]) != v.Receipt.SHA256 {
		return reject("storage_unavailable", ErrVideoUnavailable)
	}
	if _, e = s.Repo.FinishVideoJob(ctx, v, descriptor); e != nil {
		// A lost commit reply may already be ready. Never blindly delete its
		// private bytes; a fenced terminal cleanup job handles orphaned data.
		if errors.Is(e, identity.ErrUnauthorized) {
			_ = s.Repo.RejectVideoJob(ctx, v, "source_retired")
		}
		return true, e
	}
	if e = s.Runtime.Storage.DeleteQuarantine(ctx, v.QuarantineKey); e != nil {
		return true, ErrVideoUnavailable
	}
	return true, nil
}
func absVideo(n int64) int64 {
	if n < 0 {
		return -n
	}
	return n
}

func ValidateMP4Signature(b []byte) error {
	if int64(len(b)) > MaxVideoBytes || len(b) < 24 {
		return ErrVideoInvalid
	}
	offset, boxes := 0, 0
	ftyp, moov, mdat := false, false, false
	for offset < len(b) {
		if len(b)-offset < 8 || boxes >= 512 {
			return ErrVideoInvalid
		}
		size := uint64(b[offset])<<24 | uint64(b[offset+1])<<16 | uint64(b[offset+2])<<8 | uint64(b[offset+3])
		header := 8
		kind := string(b[offset+4 : offset+8])
		if size == 1 {
			if len(b)-offset < 16 {
				return ErrVideoInvalid
			}
			size = 0
			for _, x := range b[offset+8 : offset+16] {
				size = size<<8 | uint64(x)
			}
			header = 16
		}
		if size == 0 {
			size = uint64(len(b) - offset)
		}
		if size < uint64(header) || size > uint64(len(b)-offset) {
			return ErrVideoInvalid
		}
		end := offset + int(size)
		switch kind {
		case "ftyp":
			if offset != 0 || size < uint64(header+8) {
				return ErrVideoInvalid
			}
			brand := string(b[offset+header : offset+header+4])
			if brand != "isom" && brand != "iso2" && brand != "mp41" && brand != "mp42" && brand != "avc1" && brand != "M4V " {
				return ErrVideoInvalid
			}
			ftyp = true
		case "moov":
			moov = true
		case "mdat":
			mdat = true
		}
		offset = end
		boxes++
	}
	if !ftyp || !moov || !mdat {
		return ErrVideoInvalid
	}
	return nil
}
