// Package profileavatar owns explicit human profile pictures. It grants no
// Agent, model or organization permission and stores only bounded thumbnails.
package profileavatar

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	_ "image/png"
	"regexp"
	"strconv"
)

const MaxInputBytes = 5 * 1024 * 1024
const MaxThumbnailBytes = 128 * 1024
const Size = 128

var ErrInvalid = errors.New("invalid person avatar")
var ErrConflict = errors.New("person avatar changed")
var ErrNotFound = errors.New("person avatar unavailable")
var ErrUnavailable = errors.New("person avatar service unavailable")
var idPattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

func ValidID(id string) bool {
	return idPattern.MatchString(id) && id != "00000000-0000-0000-0000-000000000000"
}

type Metadata struct {
	AccountID     string `json:"accountId"`
	Version       int64  `json:"version"`
	ThumbnailPath string `json:"thumbnailPath"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	ByteCount     int    `json:"byteCount"`
	SHA256        string `json:"sha256"`
}

func Path(id string, version int64) string {
	return "/v1/accounts/" + id + "/avatar/" + strconv.FormatInt(version, 10) + "/content"
}
func (m Metadata) Valid() bool {
	_, e := hex.DecodeString(m.SHA256)
	return ValidID(m.AccountID) && m.Version > 0 && m.ThumbnailPath == Path(m.AccountID, m.Version) && m.Width == Size && m.Height == Size && m.ByteCount > 0 && m.ByteCount <= MaxThumbnailBytes && len(m.SHA256) == 64 && e == nil
}

type State struct {
	Version int64     `json:"version"`
	Avatar  *Metadata `json:"avatar"`
}
type Result struct {
	OperationID    string `json:"operationId"`
	Action         string `json:"action"`
	AppliedVersion int64  `json:"appliedVersion"`
	Current        State  `json:"current"`
}
type Input struct {
	OperationID     string
	ExpectedVersion int64
	Content         []byte
	ContentType     string
	Delete          bool
}
type Thumbnail struct {
	Bytes       []byte
	InputSHA256 string
	SHA256      string
}

// DecodeConfig runs before decoding. Bounds limit allocation and processing;
// re-encoding strips EXIF, text chunks, paths and the original full-size photo.
func Prepare(raw []byte, contentType string) (Thumbnail, error) {
	if len(raw) == 0 || len(raw) > MaxInputBytes || (contentType != "image/jpeg" && contentType != "image/png") {
		return Thumbnail{}, ErrInvalid
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 4096 || config.Height > 4096 || int64(config.Width)*int64(config.Height) > 16*1024*1024 || (format != "jpeg" && format != "png") || contentType != "image/"+format {
		return Thumbnail{}, ErrInvalid
	}
	if format == "png" {
		for at := 8; at+12 <= len(raw); {
			size := int(binary.BigEndian.Uint32(raw[at : at+4]))
			if size > len(raw)-at-12 {
				return Thumbnail{}, ErrInvalid
			}
			kind := string(raw[at+4 : at+8])
			if kind == "acTL" || kind == "fcTL" || kind == "fdAT" {
				return Thumbnail{}, ErrInvalid
			}
			at += size + 12
		}
	}
	source, actual, err := image.Decode(bytes.NewReader(raw))
	if err != nil || actual != format {
		return Thumbnail{}, ErrInvalid
	}
	bounds := source.Bounds()
	side := bounds.Dx()
	if bounds.Dy() < side {
		side = bounds.Dy()
	}
	x0, y0 := bounds.Min.X+(bounds.Dx()-side)/2, bounds.Min.Y+(bounds.Dy()-side)/2
	target := image.NewRGBA(image.Rect(0, 0, Size, Size))
	for y := 0; y < Size; y++ {
		for x := 0; x < Size; x++ {
			// Sample the center of each destination pixel. Composite transparency on
			// white so PNG transparency cannot turn into an unexpected black picture.
			r, g, b, a := source.At(x0+(2*x+1)*side/(2*Size), y0+(2*y+1)*side/(2*Size)).RGBA()
			target.SetRGBA(x, y, color.RGBA{uint8((r + 65535 - a) >> 8), uint8((g + 65535 - a) >> 8), uint8((b + 65535 - a) >> 8), 255})
		}
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, target, &jpeg.Options{Quality: 85}) != nil || out.Len() > MaxThumbnailBytes {
		return Thumbnail{}, ErrInvalid
	}
	inputSum, sum := sha256.Sum256(raw), sha256.Sum256(out.Bytes())
	return Thumbnail{out.Bytes(), hex.EncodeToString(inputSum[:]), hex.EncodeToString(sum[:])}, nil
}

// Access lives here to avoid coupling ordinary DTOs back to identity types.
type Access struct {
	AccountID     string
	AccountType   string
	SessionDigest [32]byte
}
type Store interface {
	ReadOwnAvatar(context.Context, Access) (State, error)
	ChangeOwnAvatar(context.Context, Access, Input) (Result, error)
	ReadOwnAvatarOperation(context.Context, Access, string) (Result, error)
	ReadAvatarContent(context.Context, Access, string, int64) (Metadata, []byte, error)
}
