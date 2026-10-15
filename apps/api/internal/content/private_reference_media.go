package content

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"io"
	"time"
	"unicode/utf8"
)

const ReferenceImageSchema = "owned-reference-images-v1"

const ReferenceImageTargetsSchema = "owned-reference-image-targets-v1"

type ReferenceImageTargetsQuery struct{ TargetType, Cursor string }

func (q ReferenceImageTargetsQuery) Valid() bool {
	return q.TargetType == "PLACE" && len(q.Cursor) <= 4096
}

// A target is a navigation selector, never public Place or image authority.
type ReferenceImageTarget struct {
	TargetID   string `json:"targetId"`
	Name       string `json:"name"`
	ImageCount int    `json:"imageCount"`
}

type ReferenceImageTargetsPage struct {
	SchemaVersion string                     `json:"schemaVersion"`
	TargetType    string                     `json:"targetType"`
	Items         []ReferenceImageTarget     `json:"items"`
	ObservedAt    time.Time                  `json:"observedAt"`
	ValidUntil    time.Time                  `json:"validUntil"`
	SourceVersion string                     `json:"sourceVersion"`
	NextCursor    *string                    `json:"nextCursor"`
	ResponseProof identity.HumanControlProof `json:"-"`
}

func (v ReferenceImageTargetsPage) Valid(now time.Time) bool {
	if v.SchemaVersion != ReferenceImageTargetsSchema || v.TargetType != "PLACE" || v.Items == nil || len(v.Items) > 20 || v.ObservedAt.IsZero() || v.ObservedAt.After(now.Add(time.Second)) || !v.ValidUntil.After(now) || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 30*time.Second || !MomentControlHex(v.SourceVersion) {
		return false
	}
	seen := map[string]bool{}
	for _, item := range v.Items {
		if !identity.HumanControlUUID(item.TargetID) || seen[item.TargetID] || !utf8.ValidString(item.Name) || len(item.Name) == 0 || len(item.Name) > 16000 || item.ImageCount < 1 || item.ImageCount > 10000 {
			return false
		}
		seen[item.TargetID] = true
	}
	return v.NextCursor == nil || len(v.Items) == 20 && len(*v.NextCursor) > 0 && len(*v.NextCursor) <= 4096
}

type HumanReferenceImageTargetsStore interface {
	ReadHumanReferenceImageTargets(context.Context, [32]byte, identity.Actor, ReferenceImageTargetsQuery) (ReferenceImageTargetsPage, error)
}

type ReferenceImageQuery struct{ TargetType, TargetID, Cursor string }

func (q ReferenceImageQuery) Valid() bool {
	return (q.TargetType == "PLACE" || q.TargetType == "JOURNEY" || q.TargetType == "MOMENT") && identity.HumanControlUUID(q.TargetID) && len(q.Cursor) <= 4096
}

type ReferenceImage struct {
	ID                 string `json:"id"`
	Kind               string `json:"kind"`
	Revision           int64  `json:"revision"`
	Position           int64  `json:"position"`
	MIMEType           string `json:"mimeType"`
	Width              int    `json:"width"`
	Height             int    `json:"height"`
	ByteCount          int    `json:"byteCount"`
	SHA256             string `json:"sha256"`
	ContentPath        string `json:"contentPath"`
	SourceVersion      string `json:"sourceVersion"`
	SourceType         string `json:"sourceType"`
	SourceReviewStatus string `json:"sourceReviewStatus"`
	SourceIsCover      bool   `json:"sourceIsCover"`
	SourceRightsBasis  string `json:"sourceRightsBasis"`
	SourceAttribution  string `json:"sourceAttribution"`
	SourcePublishable  *bool  `json:"sourcePublishable"`
	CanPublish         bool   `json:"canPublish"`
	CanCopy            bool   `json:"canCopy"`
}
type ReferenceImagePage struct {
	SchemaVersion string                     `json:"schemaVersion"`
	TargetType    string                     `json:"targetType"`
	TargetID      string                     `json:"targetId"`
	Items         []ReferenceImage           `json:"items"`
	ObservedAt    time.Time                  `json:"observedAt"`
	ValidUntil    time.Time                  `json:"validUntil"`
	SourceVersion string                     `json:"sourceVersion"`
	NextCursor    *string                    `json:"nextCursor"`
	ResponseProof identity.HumanControlProof `json:"-"`
}

func (v ReferenceImagePage) Valid(now time.Time) bool {
	if v.SchemaVersion != ReferenceImageSchema || !(ReferenceImageQuery{TargetType: v.TargetType, TargetID: v.TargetID}).Valid() || v.Items == nil || len(v.Items) > 20 || !v.ValidUntil.After(now) || v.ObservedAt.IsZero() || v.ObservedAt.After(now.Add(time.Second)) || !v.ValidUntil.After(v.ObservedAt) || v.ValidUntil.Sub(v.ObservedAt) > 30*time.Second || !MomentControlHex(v.SourceVersion) {
		return false
	}
	seen := map[string]bool{}
	for _, i := range v.Items {
		if !identity.HumanControlUUID(i.ID) || seen[i.ID] || i.Revision < 1 || i.CanCopy || i.CanPublish || !MomentControlHex(i.SourceVersion) || !MomentControlHex(i.SHA256) || i.Width < 1 || i.Height < 1 || i.Width > 8192 || i.Height > 8192 || int64(i.Width)*int64(i.Height) > 48*1024*1024 || i.ByteCount < 1 || i.ByteCount > 16*1024*1024 || (i.MIMEType != "image/jpeg" && i.MIMEType != "image/png") || i.SourceReviewStatus == "removed" {
			return false
		}
		seen[i.ID] = true
	}
	return true
}

type ReferenceImageContent struct {
	Image         ReferenceImage
	Bytes         []byte
	ValidUntil    time.Time
	ResponseProof identity.HumanControlProof
}
type ReferenceImageDelete struct {
	OperationID           string `json:"operationId"`
	ExpectedRevision      int64  `json:"expectedRevision"`
	ExpectedSourceVersion string `json:"expectedSourceVersion"`
}

func (v ReferenceImageDelete) Valid() bool {
	return identity.HumanControlUUID(v.OperationID) && v.ExpectedRevision > 0 && MomentControlHex(v.ExpectedSourceVersion)
}

type ReferenceImageReceipt struct {
	SchemaVersion string                     `json:"schemaVersion"`
	OperationID   string                     `json:"operationId"`
	ImageID       string                     `json:"imageId"`
	Revision      int64                      `json:"revision"`
	Status        string                     `json:"status"`
	Effect        string                     `json:"effect"`
	CommittedAt   time.Time                  `json:"committedAt"`
	ResponseProof identity.HumanControlProof `json:"-"`
}
type HumanReferenceImageStore interface {
	ReadHumanReferenceImages(context.Context, [32]byte, identity.Actor, ReferenceImageQuery) (ReferenceImagePage, error)
	ReadHumanReferenceImageContent(context.Context, [32]byte, identity.Actor, string, int64, string) (ReferenceImageContent, error)
	DeleteHumanReferenceImage(context.Context, [32]byte, identity.Actor, string, ReferenceImageDelete) (ReferenceImageReceipt, error)
	ReadHumanReferenceImageOperation(context.Context, [32]byte, identity.Actor, string) (ReferenceImageReceipt, error)
}

func DecodeReferenceImageDelete(raw []byte) (ReferenceImageDelete, error) {
	var v ReferenceImageDelete
	d := json.NewDecoder(bytes.NewReader(raw))
	token, e := d.Token()
	if e != nil || token != json.Delim('{') {
		return v, ErrInvalid
	}
	seen := map[string]bool{}
	for d.More() {
		key, e := d.Token()
		k, ok := key.(string)
		if e != nil || !ok || seen[k] {
			return v, ErrInvalid
		}
		seen[k] = true
		switch k {
		case "operationId":
			e = d.Decode(&v.OperationID)
		case "expectedRevision":
			e = d.Decode(&v.ExpectedRevision)
		case "expectedSourceVersion":
			e = d.Decode(&v.ExpectedSourceVersion)
		default:
			return v, ErrInvalid
		}
		if e != nil {
			return v, ErrInvalid
		}
	}
	token, e = d.Token()
	if e != nil || token != json.Delim('}') || len(seen) != 3 || !v.Valid() {
		return v, ErrInvalid
	}
	var extra any
	if d.Decode(&extra) != io.EOF {
		return v, ErrInvalid
	}
	return v, nil
}
