package placephoto

import (
	"bytes"
	"encoding/json"
	"image"
	"image/png"
	"strings"
	"testing"
	"time"
)

const photoID = "9931ef56-8c9d-4241-9437-99373492c5a1"
const photoOp = "9931ef56-8c9d-4241-9437-99373492c5a2"

func TestPlacePhotoConcreteRightsSourceLeaseAndReceipt(t *testing.T) {
	now := time.Now().UTC()
	m := Metadata{EntityType: "place", EntityID: photoID, Version: 1, ThumbnailPath: Path(photoID, 1), Width: 320, Height: 180, ByteCount: 200, SHA256: strings.Repeat("a", 64), ExpiresAt: now.Add(20 * time.Second), Attribution: "明确获准"}
	if !m.Valid(now) {
		t.Fatal("valid original photo")
	}
	for _, change := range []func(*Metadata){func(m *Metadata) { m.EntityType = "activity" }, func(m *Metadata) { m.ThumbnailPath = "https://images.invalid/arbitrary" }, func(m *Metadata) { m.Width = 128 }, func(m *Metadata) { m.ExpiresAt = now.Add(2 * time.Minute) }} {
		c := m
		change(&c)
		if c.Valid(now) {
			t.Fatal("photo borrowed another purpose")
		}
	}
	current := m
	current.ExpiresAt = now.Add(30 * time.Second)
	if !PreserveLease(&m, &current, now) || !current.ExpiresAt.Equal(m.ExpiresAt) {
		t.Fatal("renewed lease")
	}
	current = m
	current.ExpiresAt = now.Add(10 * time.Second)
	if PreserveLease(&m, &current, now) {
		t.Fatal("shortened source allowed old lease")
	}
	r := Receipt{PlaceID: photoID, OperationID: photoOp, Action: "SUBMIT", CandidateID: photoOp, CandidateVersion: 1, Status: "SUCCEEDED"}
	if !r.Valid(photoID, photoOp) {
		t.Fatal("retained candidate receipt")
	}
	r.Status = "STOPPED"
	if r.Valid(photoID, photoOp) {
		t.Fatal("success reinterpreted as zero")
	}
	r = Receipt{PlaceID: photoID, OperationID: photoOp, Action: "STOP", Status: "STOPPED"}
	if !r.Valid(photoID, photoOp) {
		t.Fatal("original tombstone")
	}
	raw, _ := json.Marshal(State{Version: 1, PlacePhoto: &m, SourceProof: "PRIVATE_SOURCE"})
	if bytes.Contains(raw, []byte("PRIVATE_SOURCE")) {
		t.Fatal("public proof leak")
	}
}
func TestPlacePhotoSubmissionNeedsSpecificRetainPublishRights(t *testing.T) {
	var b bytes.Buffer
	if png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 24, 18))) != nil {
		t.Fatal("fixture")
	}
	now := time.Now().UTC()
	rights := Rights{Basis: "OWNED", Attribution: "合成测试", RetentionUntil: now.Add(time.Hour), CanRetain: true, CanPublish: true}
	in := Input{OperationID: photoOp, Action: "SUBMIT", ContentType: "image/png", Content: b.Bytes(), Rights: &rights}
	d, e := Normalize(in, now)
	if e != nil || len(d.Bytes) == 0 || len(d.SHA256) != 64 {
		t.Fatal("actual normalized image", e)
	}
	for _, mode := range []string{"retain", "publish", "expired", "unlicensed"} {
		t.Run(mode, func(t *testing.T) {
			bad := rights
			switch mode {
			case "retain":
				bad.CanRetain = false
			case "publish":
				bad.CanPublish = false
			case "expired":
				bad.RetentionUntil = now
			case "unlicensed":
				bad.Basis = "LICENSED"
			}
			input := in
			input.Rights = &bad
			if _, e := Normalize(input, now); e == nil {
				t.Fatal("unapproved pixel purpose retained")
			}
		})
	}
	if Digest(photoID, in, d.SHA256, rights.Digest()) == Digest(photoOp, in, d.SHA256, rights.Digest()) {
		t.Fatal("target not bound")
	}
	approved := Input{OperationID: photoOp, Action: "APPROVE", CandidateID: photoID, CandidateVersion: 1, RightsConfirmed: true, PhotoConfirmed: true}
	if _, e := Normalize(approved, now); e != nil {
		t.Fatal(e)
	}
	approved.Content = b.Bytes()
	if _, e := Normalize(approved, now); e == nil {
		t.Fatal("reviewer replaced submitted pixels")
	}
	retired := Input{OperationID: photoOp, Action: "WITHDRAW", CandidateID: photoID, CandidateVersion: MaxVersion, ExpectedVersion: MaxVersion}
	if _, e := Normalize(retired, now); e != nil {
		t.Fatal("last version prevented privacy withdrawal", e)
	}
}
