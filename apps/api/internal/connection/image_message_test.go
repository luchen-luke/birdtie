package connection

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

const imageTestOwner = "11111111-1111-4111-8111-111111111111"
const imageTestConversation = "22222222-2222-4222-8222-222222222222"
const imageTestRecipient = "33333333-3333-4333-8333-333333333333"
const imageTestOperation = "44444444-4444-4444-8444-444444444444"

func imageWireInput() ImageMessageInput {
	b := []byte{1, 2, 3}
	h := sha256.Sum256(b)
	// Wire validation verifies bounded bytes/hash, not pixel validity; codec
	// tests independently use actual PNG/JPEG images.
	return ImageMessageInput{OperationID: imageTestOperation, RecipientAccountID: imageTestRecipient, Purpose: ChatImagePurpose, Caption: "你好\ncafé", PixelRisk: "USER_MASKED", MIME: "image/png", InputSize: 3, InputSHA256: hex.EncodeToString(h[:]), Content: b}
}
func imageWireBody(v ImageMessageInput) map[string]any {
	return map[string]any{"operationId": v.OperationID, "recipientAccountId": v.RecipientAccountID, "purpose": v.Purpose, "caption": v.Caption, "pixelRisk": v.PixelRisk, "mimeType": v.MIME, "inputSize": v.InputSize, "inputSha256": v.InputSHA256, "contentBase64": base64.StdEncoding.EncodeToString(v.Content)}
}
func imageTestMetadata() ImageMetadata {
	return ImageMetadata{Schema: ConversationImageSchema, ImageDerivativeMetadata: ImageDerivativeMetadata{MIME: "image/png", ByteSize: 1000, SHA256: strings.Repeat("a", 64), Width: 512, Height: 128}, Thumbnail: ImageDerivativeMetadata{MIME: "image/png", ByteSize: 500, SHA256: strings.Repeat("b", 64), Width: 256, Height: 64}}
}
func TestImageMessageClosedInputAndCanonicalBytes(t *testing.T) {
	in := imageWireInput()
	body := imageWireBody(in)
	raw, _ := json.Marshal(body)
	got, e := DecodeImageMessage(raw)
	if e != nil || ImageMessagePayloadDigest(imageTestConversation, got) != ImageMessagePayloadDigest(imageTestConversation, in) {
		t.Fatal("valid closed input", e)
	}
	for _, change := range []func(map[string]any){
		func(v map[string]any) { v["ownerId"] = imageTestOwner }, func(v map[string]any) { v["taskId"] = imageTestOperation }, func(v map[string]any) { v["url"] = "https://example.invalid/image" },
		func(v map[string]any) { v["purpose"] = "HUMAN_TASK_MESSAGE_ATTACHMENT" }, func(v map[string]any) { v["caption"] = "a\x00b" }, func(v map[string]any) { v["caption"] = strings.Repeat("中", 667) },
		func(v map[string]any) { v["pixelRisk"] = "CLEAN" }, func(v map[string]any) { v["mimeType"] = "image/gif" }, func(v map[string]any) { v["inputSize"] = 2 }, func(v map[string]any) { v["inputSize"] = 3.5 },
		func(v map[string]any) { v["inputSha256"] = strings.Repeat("f", 64) }, func(v map[string]any) { v["contentBase64"] = "AQID\n" }, func(v map[string]any) { v["caption"] = nil }, func(v map[string]any) { delete(v, "caption") },
	} {
		v := imageWireBody(in)
		change(v)
		raw, _ := json.Marshal(v)
		if _, e := DecodeImageMessage(raw); e == nil {
			t.Fatal("invalid image input accepted")
		}
	}
	raw, _ = json.Marshal(body)
	duplicate := append(append([]byte(nil), raw[:len(raw)-1]...), []byte(`,"caption":"x"}`)...)
	if _, e := DecodeImageMessage(duplicate); e == nil {
		t.Fatal("duplicate caption accepted")
	}
	for _, caption := range []string{"", "图片消息", "语音消息", "  文字  "} {
		in.Caption = caption
		raw, _ = json.Marshal(imageWireBody(in))
		if _, e := DecodeImageMessage(raw); e != nil {
			t.Fatal("valid caption changed", caption, e)
		}
	}
}
func TestImageReceiptApprovalAndImmutableDerivativeAreSeparate(t *testing.T) {
	in := imageWireInput()
	meta := imageTestMetadata()
	r := NewImageMessageReceipt(imageTestOwner, imageTestConversation, imageTestOperation, in, meta, time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC))
	if !ValidImageMessageReceipt(r, imageTestOwner, imageTestConversation, in.OperationID) || !ImageReceiptMatchesInput(r, imageTestConversation, in) || r.Image.SHA256 == r.InputSHA256 {
		t.Fatal("server derivative falsely equated to input")
	}
	if r.EffectKey != HumanMessageEffectKey(imageTestOwner, in.OperationID) {
		t.Fatal("image creates another effect domain")
	}
	if r.PayloadDigest != "d9ef834761e8b2bb0b2ad74c726e3131e79d96ba975b5d546ac0c35578b217da" || r.CaptionSHA256 != "b2f8916970c6a4306b8c3ab4ca68d5a1ba783231d8e336616497ea0d8b4bc08d" || r.EffectKey != "5bc69c34eb8ac769f76d90b7ac044f7c428af2c06db0d30ebcb9bcf47ee22fe1" {
		t.Fatal("UTF8 framing differs from shared independent vector")
	}
	for _, change := range []func(*ImageMessageInput){func(v *ImageMessageInput) { v.Caption += "!" }, func(v *ImageMessageInput) { v.RecipientAccountID = imageTestOwner }, func(v *ImageMessageInput) { v.PixelRisk = "UNKNOWN" }, func(v *ImageMessageInput) { v.InputSHA256 = strings.Repeat("c", 64) }} {
		v := in
		change(&v)
		if ImageReceiptMatchesInput(r, imageTestConversation, v) {
			t.Fatal("changed approved payload replay")
		}
	}
	meta.Thumbnail.Width = 255
	if ValidImageMetadata(meta) {
		t.Fatal("thumbnail wrong aspect metadata")
	}
	meta = imageTestMetadata()
	meta.Width = 8193
	if ValidImageMetadata(meta) {
		t.Fatal("oversized full")
	}
}
func TestImageEnvelopeFitsFullInputAndEscapedCaption(t *testing.T) {
	in := imageWireInput()
	in.Content = make([]byte, MaxChatImageInputBytes)
	in.InputSize = int64(len(in.Content))
	h := sha256.Sum256(in.Content)
	in.InputSHA256 = hex.EncodeToString(h[:])
	in.Caption = strings.Repeat("\u0001", 2000)
	raw, _ := json.Marshal(imageWireBody(in))
	if len(raw) > MaxChatImageJSONBytes {
		t.Fatal("bounded envelope excludes a valid escaped caption")
	}
	if _, e := DecodeImageMessage(raw); e != nil {
		t.Fatal("valid maximum input with escaped caption", e)
	}
}
