package agentvisual

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"hash/crc32"
	"image"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func inputFixture(t *testing.T) PreviewInput {
	t.Helper()
	var b bytes.Buffer
	png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 12, 8)))
	return PreviewInput{OperationID: "12600000-0000-4000-8000-000000000001", ClientDraftID: "12600000-0000-4000-8000-000000000002", ClientDraftRevision: 1, SourceContextType: "CITY", SourceContextID: "test-city", Purpose: Purpose, Image: ImageInput{MIME: "image/png", ContentBase64: base64.StdEncoding.EncodeToString(b.Bytes()), InputSHA256: Digest(b.Bytes()), PixelRisk: "UNKNOWN"}}
}
func TestAgentVisualPureImageStrictIndependentPurpose(t *testing.T) {
	in := inputFixture(t)
	raw, _ := json.Marshal(in)
	out, e := DecodePreview(raw)
	if e != nil || out.UserText != "" || out.TaskID != "" {
		t.Fatal("pure image requires a previous text task", e)
	}
	for _, bad := range []string{strings.Replace(string(raw), Purpose, "HUMAN_TASK_MESSAGE_ATTACHMENT", 1), strings.Replace(string(raw), `"userText":""`, `"userText":"","userText":"borrow"`, 1), strings.Replace(string(raw), `"pixelRisk":"UNKNOWN"`, `"pixelRisk":"UNKNOWN","ownerId":"borrow"`, 1), strings.Replace(string(raw), `"clientDraftRevision":1`, `"clientDraftRevision":null`, 1), string(raw) + "{}"} {
		if _, e := DecodePreview([]byte(bad)); e == nil {
			t.Fatal("unsafe selector/duplicate accepted")
		}
	}
}
func TestAgentVisualSingleFrameCanonicalPixels(t *testing.T) {
	in := inputFixture(t)
	p, b, e := DerivePixels(context.Background(), in.Image)
	if e != nil || p.Width != 12 || p.Height != 8 || p.MIME != "image/jpeg" || p.SHA256 != Digest(b) || bytes.Contains(b, []byte("secret metadata")) {
		t.Fatal(p, e)
	}
	var jpegBytes bytes.Buffer
	jpeg.Encode(&jpegBytes, image.NewRGBA(image.Rect(0, 0, 12, 8)), nil)
	for _, raw := range [][]byte{append(append([]byte{}, jpegBytes.Bytes()...), jpegBytes.Bytes()...), append(append([]byte{}, jpegBytes.Bytes()...), 0), []byte("fake jpg")} {
		bad := ImageInput{MIME: "image/jpeg", ContentBase64: base64.StdEncoding.EncodeToString(raw), InputSHA256: Digest(raw)}
		if _, _, e := DerivePixels(context.Background(), bad); e == nil {
			t.Fatal("accepted non-single JPEG")
		}
	}
	pngBytes, _ := base64.StdEncoding.DecodeString(in.Image.ContentBase64)
	chunk := make([]byte, 20)
	binary.BigEndian.PutUint32(chunk, 8)
	copy(chunk[4:8], "acTL")
	binary.BigEndian.PutUint32(chunk[8:12], 2)
	binary.BigEndian.PutUint32(chunk[16:], crc32.ChecksumIEEE(chunk[4:16]))
	animated := append(append(append([]byte{}, pngBytes[:33]...), chunk...), pngBytes[33:]...)
	bad := ImageInput{MIME: "image/png", ContentBase64: base64.StdEncoding.EncodeToString(animated), InputSHA256: Digest(animated)}
	if _, _, e := DerivePixels(context.Background(), bad); e == nil {
		t.Fatal("animated PNG accepted")
	}
}
func TestAgentVisualExactApprovalRejectsLegacyAndNull(t *testing.T) {
	in := ApproveInput{OperationID: inputFixture(t).OperationID, ExpectedVersion: 1, PayloadDigest: strings.Repeat("a", 64), PixelSHA256: strings.Repeat("b", 64), Purpose: Purpose, Confirmed: true}
	raw, _ := json.Marshal(in)
	if _, e := DecodeApproval(raw); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(string(raw), "true", "null", 1), strings.Replace(string(raw), Purpose, "MODEL_CONTEXT_EGRESS", 1), strings.Replace(string(raw), `"expectedVersion":1`, `"expectedVersion":1,"expectedVersion":2`, 1)} {
		if _, e := DecodeApproval([]byte(bad)); e == nil {
			t.Fatal("invalid exact approval")
		}
	}
}
