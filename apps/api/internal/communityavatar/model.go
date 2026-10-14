// Package communityavatar owns explicitly approved, community-bound thumbnails.
package communityavatar

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	av "github.com/birdtie/birdtie/apps/api/internal/profileavatar"
)

const MaxVersion int64 = 9007199254740991

var ErrInvalid = errors.New("invalid community avatar")
var ErrChanged = errors.New("community avatar changed")
var ErrForbidden = errors.New("community avatar permission denied")
var ErrNotFound = errors.New("community avatar not found")
var ErrUnavailable = errors.New("community avatar unavailable")

type Access = av.Access
type Metadata struct {
	CommunityID   string `json:"communityId"`
	Version       int64  `json:"version"`
	ThumbnailPath string `json:"thumbnailPath"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ByteCount     int    `json:"byteCount"`
	SHA256        string `json:"sha256"`
}

func Path(id string, v int64) string {
	return "/v1/communities/" + id + "/avatar/" + strconv.FormatInt(v, 10) + "/content"
}
func (m Metadata) Valid() bool {
	_, e := hex.DecodeString(m.SHA256)
	return av.ValidID(m.CommunityID) && m.Version > 0 && m.Version <= MaxVersion && m.ThumbnailPath == Path(m.CommunityID, m.Version) && m.Width == av.Size && m.Height == av.Size && m.ByteCount > 0 && m.ByteCount <= av.MaxThumbnailBytes && len(m.SHA256) == 64 && e == nil
}

type State struct {
	Version int64     `json:"version"`
	Avatar  *Metadata `json:"avatar"`
}
type Input struct {
	OperationID     string
	ExpectedVersion int64
	ContentType     string
	Content         []byte
	Delete          bool
	Approval        string
}
type Approval struct {
	CommunityID     string    `json:"communityId"`
	OperationID     string    `json:"operationId"`
	ExpectedVersion int64     `json:"expectedVersion"`
	Action          string    `json:"action"`
	InputSHA256     string    `json:"inputSha256"`
	ExpiresAt       time.Time `json:"expiresAt"`
	Token           string    `json:"token"`
}
type Result struct {
	CommunityID    string `json:"communityId"`
	OperationID    string `json:"operationId"`
	Action         string `json:"action"`
	AppliedVersion int64  `json:"appliedVersion"`
	Current        State  `json:"current"`
}
type Store interface {
	ReadCommunityAvatar(context.Context, Access, string) (State, error)
	PreviewCommunityAvatar(context.Context, Access, string, Input) (Approval, error)
	ChangeCommunityAvatar(context.Context, Access, string, Input) (Result, error)
	ReadCommunityAvatarOperation(context.Context, Access, string, string) (Result, error)
	ReadCommunityAvatarContent(context.Context, Access, string, int64) (Metadata, []byte, error)
}

func Normalize(in Input) (Input, av.Thumbnail, string, string, error) {
	if !av.ValidID(in.OperationID) || in.ExpectedVersion < 0 || in.ExpectedVersion >= MaxVersion {
		return Input{}, av.Thumbnail{}, "", "", ErrInvalid
	}
	if in.Delete {
		if len(in.Content) != 0 || in.ContentType != "" {
			return Input{}, av.Thumbnail{}, "", "", ErrInvalid
		}
		return in, av.Thumbnail{}, "DELETE", "", nil
	}
	thumb, e := av.Prepare(in.Content, in.ContentType)
	if e != nil {
		return Input{}, av.Thumbnail{}, "", "", ErrInvalid
	}
	return in, thumb, "SAVE", thumb.SHA256, nil
}
func Digest(id string, in Input, action, sum string) string {
	raw, _ := json.Marshal([]any{id, in.OperationID, in.ExpectedVersion, action, sum})
	h := sha256.Sum256(raw)
	return hex.EncodeToString(h[:])
}
