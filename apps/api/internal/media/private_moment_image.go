package media

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"regexp"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const PrivateImagePurpose = "PRIVATE_MOMENT_ATTACHMENT"
const PrivateImagePreviewTTL = 5 * time.Minute
const PrivateImageRetention = 30 * 24 * time.Hour

var privateImageUUID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var privateImageSHA = regexp.MustCompile(`^[0-9a-f]{64}$`)

// This is a human private attachment, never a model/Memory/publication grant.
// UNKNOWN and USER_MASKED describe user choices, not a clean scanner verdict.
type PrivateImageInput struct {
	OperationID    string `json:"operationId"`
	MomentRevision int64  `json:"momentRevision"`
	MIME           string `json:"mimeType"`
	ByteSize       int64  `json:"byteSize"`
	SHA256         string `json:"sha256"`
	PixelRisk      string `json:"pixelRisk"`
	Purpose        string `json:"purpose"`
}
type PrivateImageReceipt struct {
	ID               string    `json:"id"`
	OperationID      string    `json:"operationId"`
	MomentID         string    `json:"momentId"`
	OwnerID          string    `json:"ownerAccountId"`
	MomentRevision   int64     `json:"momentRevision"`
	MIME             string    `json:"mimeType"`
	ByteSize         int64     `json:"byteSize"`
	InputSHA256      string    `json:"inputSha256"`
	SHA256           string    `json:"sha256"`
	PixelRisk        string    `json:"pixelRisk"`
	Purpose          string    `json:"purpose"`
	Status           string    `json:"status"`
	Revision         int64     `json:"revision"`
	PreviewExpiresAt time.Time `json:"previewExpiresAt"`
	RetainUntil      time.Time `json:"retainUntil"`
}

// Encode functions only build bounded bytes, never send HTTP or invoke a model.
// Native reads encode under resource locks, then recheck the current wall clock,
// source and session before committing and releasing the encoded response.
type PrivateImageEncode func([]PrivateImageReceipt, []byte) ([]byte, error)
type HumanPrivateImageStore interface {
	PreviewHumanPrivateImage(context.Context, [32]byte, identity.Actor, string, PrivateImageInput) (PrivateImageReceipt, error)
	SaveHumanPrivateImage(context.Context, [32]byte, identity.Actor, string, string, string, []byte) (PrivateImageReceipt, error)
	ReadHumanPrivateImages(context.Context, [32]byte, identity.Actor, string, string, bool, PrivateImageEncode) ([]byte, error)
	ReadHumanPrivateImageOperation(context.Context, [32]byte, identity.Actor, string, string, PrivateImageEncode) ([]byte, error)
	DeleteHumanPrivateImage(context.Context, [32]byte, identity.Actor, string, string, int64) error
}

func ValidPrivateImageID(id string) bool { return privateImageUUID.MatchString(id) }
func ValidatePrivateImageInput(p PrivateImageInput) error {
	if !ValidPrivateImageID(p.OperationID) || p.MomentRevision < 1 || p.Purpose != PrivateImagePurpose ||
		(p.MIME != "image/png" && p.MIME != "image/jpeg") || p.ByteSize < 1 || p.ByteSize > MaxImageInputBytes ||
		!privateImageSHA.MatchString(p.SHA256) || (p.PixelRisk != "UNKNOWN" && p.PixelRisk != "USER_MASKED") {
		return content.ErrInvalid
	}
	return nil
}

// Verify the exact reviewed input before using the existing concrete codec.
// Only the derivative is returned for persistence; the original encoded file
// and metadata are not stored. Pixel content persists unless the user masks it;
// re-encoding does not inspect or remove sensitive pixel content.
func PreparePrivateImage(ctx context.Context, p PrivateImageInput, data []byte) ([]byte, string, error) {
	if err := ValidatePrivateImageInput(p); err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	if int64(len(data)) != p.ByteSize || hex.EncodeToString(sum[:]) != p.SHA256 {
		return nil, "", content.ErrConflict
	}
	r, _, err := (&LocalImageDerivativeBuilder{}).BuildPublic(ctx, bytes.NewReader(data), p.MIME)
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	out, err := io.ReadAll(io.LimitReader(r, MaxImageOutputBytes+1))
	if err != nil {
		return nil, "", err
	}
	if len(out) > MaxImageOutputBytes {
		return nil, "", ErrImageLimit
	}
	sum = sha256.Sum256(out)
	return out, hex.EncodeToString(sum[:]), nil
}
