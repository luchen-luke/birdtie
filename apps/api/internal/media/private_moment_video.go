package media

import (
	"context"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/legacyvideo"
	"time"
)

const PrivateVideoSchema = "private-moment-video-v1"

type PrivateVideoReceipt struct {
	SchemaVersion  string    `json:"schemaVersion"`
	ID             string    `json:"id"`
	MomentID       string    `json:"momentId"`
	OwnerID        string    `json:"ownerAccountId"`
	Origin         string    `json:"origin"`
	Status         string    `json:"status"`
	Revision       int64     `json:"revision"`
	Position       int       `json:"position"`
	MIME           string    `json:"mimeType"`
	InputByteSize  int64     `json:"inputByteSize"`
	ByteSize       int64     `json:"byteSize"`
	SHA256         string    `json:"sha256"`
	Width          int       `json:"width"`
	Height         int       `json:"height"`
	DurationMS     int64     `json:"durationMs"`
	Frames         int64     `json:"frameCount"`
	AudioCodec     string    `json:"codecAudio"`
	PosterBytes    int64     `json:"posterBytes"`
	PosterSHA256   string    `json:"posterSha256"`
	ValidationKind string    `json:"validationKind"`
	AntivirusScan  string    `json:"antivirusScan"`
	PrivacyRisk    string    `json:"privacyRisk"`
	RetainUntil    time.Time `json:"retainUntil"`
}

func (v PrivateVideoReceipt) Valid() bool {
	return v.SchemaVersion == PrivateVideoSchema && ValidPrivateImageID(v.ID) && ValidPrivateImageID(v.MomentID) && ValidPrivateImageID(v.OwnerID) && v.Origin == "LEGACY_PRIVATE_IMPORT" && (v.Status == "ready_private" || v.Status == "deleted") && v.Revision > 0 && v.Position >= 0 && v.MIME == "video/mp4" && v.InputByteSize > 0 && v.InputByteSize <= legacyvideo.MaxInputBytes && v.ByteSize > 0 && v.ByteSize <= legacyvideo.MaxContentBytes && privateImageSHA.MatchString(v.SHA256) && v.Width > 0 && v.Height > 0 && v.Width <= 1920 && v.Height <= 1920 && v.Width*v.Height <= 2073600 && v.DurationMS > 0 && v.DurationMS <= legacyvideo.MaxDurationMS && v.Frames > 0 && v.Frames <= 7200 && (v.AudioCodec == "aac" || v.AudioCodec == "none") && v.PosterBytes > 0 && v.PosterBytes <= legacyvideo.MaxPosterBytes && privateImageSHA.MatchString(v.PosterSHA256) && v.ValidationKind == legacyvideo.Validation && v.AntivirusScan == "NOT_RUN" && v.PrivacyRisk == "UNKNOWN" && !v.RetainUntil.IsZero()
}

type PrivateVideoEncode func([]PrivateVideoReceipt, []byte) ([]byte, error)
type HumanPrivateVideoStore interface {
	ReadHumanPrivateVideos(context.Context, [32]byte, identity.Actor, string, string, string, PrivateVideoEncode) ([]byte, error)
	DeleteHumanPrivateVideo(context.Context, [32]byte, identity.Actor, string, string, int64) (PrivateVideoReceipt, error)
}
