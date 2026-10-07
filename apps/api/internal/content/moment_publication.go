package content

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"regexp"
	"time"
	"unicode/utf8"
)

var publicationToken = regexp.MustCompile(`^mp1\.[1-9][0-9]{0,18}\.[1-9][0-9]{0,18}\.[0-9a-f]{64}$`)

func DecodeMomentPublication(raw []byte) (MomentPublicationInput, error) {
	var out MomentPublicationInput
	if len(raw) > 4096 || !utf8.Valid(raw) {
		return out, ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	t, e := d.Token()
	if e != nil || t != json.Delim('{') {
		return out, ErrInvalid
	}
	fields := map[string]bool{}
	for d.More() {
		t, e = d.Token()
		k, ok := t.(string)
		if e != nil || !ok || fields[k] {
			return MomentPublicationInput{}, ErrInvalid
		}
		fields[k] = true
		var value json.RawMessage
		if d.Decode(&value) != nil || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return MomentPublicationInput{}, ErrInvalid
		}
		switch k {
		case "revision":
			if json.Unmarshal(value, &out.Revision) != nil {
				return MomentPublicationInput{}, ErrInvalid
			}
		case "snapshot":
			if json.Unmarshal(value, &out.Snapshot) != nil {
				return MomentPublicationInput{}, ErrInvalid
			}
		case "confirmPublic":
			if json.Unmarshal(value, &out.ConfirmPublic) != nil {
				return MomentPublicationInput{}, ErrInvalid
			}
		default:
			return MomentPublicationInput{}, ErrInvalid
		}
	}
	if t, e = d.Token(); e != nil || t != json.Delim('}') || len(fields) != 3 {
		return MomentPublicationInput{}, ErrInvalid
	}
	if _, e = d.Token(); e != io.EOF || out.Revision < 1 || !out.ConfirmPublic || !publicationToken.MatchString(out.Snapshot) {
		return MomentPublicationInput{}, ErrInvalid
	}
	return out, nil
}
func ValidateMomentPublicationPreview(p MomentPublicationPreview, id string) error {
	if p.MomentID != id || p.Revision < 1 || p.PlaceName == "" || !utf8.ValidString(p.PlaceName) || len(p.PlaceName) > 640 || p.ExpiresAt.IsZero() || !publicationToken.MatchString(p.Snapshot) {
		return ErrInvalid
	}
	_, e := NormalizeMomentInput(MomentInput{CityID: p.CityID, PlaceID: p.PlaceID, Title: p.Title, Body: p.Body, TimePrecision: "unknown", LocationPrecision: "place"})
	return e
}

// Human-only explicit publication of one currently owned draft. The snapshot
// is a current source binding, never model/Memory permission or success.
type MomentPublicationPreview struct {
	MomentID  string    `json:"momentId"`
	PlaceID   string    `json:"placeId"`
	CityID    string    `json:"cityId"`
	PlaceName string    `json:"placeName"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	Revision  int64     `json:"revision"`
	Snapshot  string    `json:"snapshot"`
	ExpiresAt time.Time `json:"expiresAt"`
}
type MomentPublicationInput struct {
	Revision      int64  `json:"revision"`
	Snapshot      string `json:"snapshot"`
	ConfirmPublic bool   `json:"confirmPublic"`
}
type MomentPublicationReceipt struct {
	MomentID    string    `json:"momentId"`
	PlaceID     string    `json:"placeId"`
	Revision    int64     `json:"revision"`
	Status      string    `json:"status"`
	PublishedAt time.Time `json:"publishedAt"`
}
type HumanMomentPublicationStore interface {
	PreviewHumanMomentPublication(context.Context, [32]byte, identity.Actor, string) (MomentPublicationPreview, error)
	PublishHumanMoment(context.Context, [32]byte, identity.Actor, string, MomentPublicationInput) (MomentPublicationReceipt, error)
}
