package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"image"
	"image/png"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

type humanImageHTTPSpy struct {
	connection.Store
	sent, read, content, final int
	access                     ea.Access
	receipt                    connection.ImageMessageReceipt
	derivative                 media.ChatImageDerivatives
	err, finalErr              error
}

func (p *humanImageHTTPSpy) SendHumanImageMessage(_ context.Context, a ea.Access, _ string, _ connection.ImageMessageInput) (connection.ImageMessageReceipt, error) {
	p.sent++
	p.access = a
	return p.receipt, p.err
}
func (p *humanImageHTTPSpy) ReadHumanImageMessageOperation(_ context.Context, a ea.Access, _, _ string) (connection.ImageMessageReceipt, error) {
	p.read++
	p.access = a
	return p.receipt, p.err
}
func (p *humanImageHTTPSpy) ValidateHumanImageMessageResponse(_ context.Context, _ ea.Access, _ connection.ImageMessageReceipt) error {
	p.final++
	return p.finalErr
}
func (p *humanImageHTTPSpy) ReadHumanImageMessageContent(_ context.Context, a ea.Access, _, _, variant string, encode connection.ImageContentEncode) ([]byte, error) {
	p.content++
	p.access = a
	if p.err != nil {
		return nil, p.err
	}
	m, _ := connection.ImageVariantMetadata(p.receipt.Image, variant)
	b := p.derivative.Full
	if variant == "thumbnail" {
		b = p.derivative.Thumbnail
	}
	out, e := encode(p.receipt, m, b)
	if e != nil {
		return nil, e
	}
	if p.finalErr != nil {
		return nil, p.finalErr
	}
	return out, nil
}
func humanImageHTTP(t *testing.T) (http.Handler, *humanImageHTTPSpy, string, connection.ImageMessageInput) {
	t.Helper()
	_, _, access, token := messagePolicyUnitHTTP(t)
	var raw bytes.Buffer
	if e := png.Encode(&raw, image.NewNRGBA(image.Rect(0, 0, 48, 24))); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(raw.Bytes())
	in := connection.ImageMessageInput{OperationID: humanHTTPOp, RecipientAccountID: privateProfileHTTPAgent, Purpose: connection.ChatImagePurpose, Caption: "", PixelRisk: "UNKNOWN", MIME: "image/png", InputSize: int64(raw.Len()), InputSHA256: hex.EncodeToString(h[:]), Content: raw.Bytes()}
	d, e := (&media.LocalChatImageDerivativeBuilder{}).Build(context.Background(), in.Content, in.MIME)
	if e != nil {
		t.Fatal(e)
	}
	p := &humanImageHTTPSpy{derivative: d, receipt: connection.NewImageMessageReceipt(privateProfileHTTPOwner, humanHTTPConv, privateProfileHTTPAgent, in, d.Metadata, time.Date(2026, 10, 12, 1, 2, 3, 0, time.UTC))}
	return New(nil, access, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil), p, token, in
}
func humanImageHTTPBody(in connection.ImageMessageInput) string {
	b, _ := json.Marshal(map[string]any{"operationId": in.OperationID, "recipientAccountId": in.RecipientAccountID, "purpose": in.Purpose, "caption": in.Caption, "pixelRisk": in.PixelRisk, "mimeType": in.MIME, "inputSize": in.InputSize, "inputSha256": in.InputSHA256, "contentBase64": base64.StdEncoding.EncodeToString(in.Content)})
	return string(b)
}
func TestHumanImageRegisteredRoutesAndClosedVariants(t *testing.T) {
	for _, stage := range []string{"POST", "GET", "thumbnail", "full"} {
		t.Run(stage, func(t *testing.T) {
			h, p, token, in := humanImageHTTP(t)
			path := "/v1/me/conversations/" + humanHTTPConv + "/image-message-operations"
			verb, body, want := "GET", "", 200
			switch stage {
			case "POST":
				verb, body, want = "POST", humanImageHTTPBody(in), 201
			case "GET":
				path += "/" + humanHTTPOp
			default:
				path = "/v1/me/conversations/" + humanHTTPConv + "/messages/" + privateProfileHTTPAgent + "/image?variant=" + stage
			}
			w := messagePolicyUnitRequest(h, verb, path, body, token, nil)
			if w.Code != want || p.sent+p.read+p.content != 1 || p.access.Actor.ID != privateProfileHTTPOwner || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("image registered route", stage, w.Code)
			}
			if stage == "full" || stage == "thumbnail" {
				m, _ := connection.ImageVariantMetadata(p.receipt.Image, stage)
				if w.Header().Get("X-Image-Variant") != stage || w.Header().Get("X-Content-SHA256") != m.SHA256 || !connection.ValidImageContent(m, w.Body.Bytes(), stage == "thumbnail") {
					t.Fatal("wrong image content/header")
				}
			} else if p.final != 1 || strings.Contains(w.Body.String(), "contentBase64") {
				t.Fatal("receipt bypassed final validation or leaked bytes")
			}
		})
	}
	for _, query := range []string{"?url=https://example.invalid/x", "?variant=full&variant=full", "?variant=small", "?", "?variant=full&width=128"} {
		h, p, token, _ := humanImageHTTP(t)
		path := "/v1/me/conversations/" + humanHTTPConv + "/messages/" + privateProfileHTTPAgent + "/image" + query
		w := messagePolicyUnitRequest(h, "GET", path, "", token, nil)
		if w.Code == 200 || p.content != 0 {
			t.Fatal("open variant query accepted", query)
		}
	}
}
func TestHumanImageFinalAuthorizationDoesNotReleaseReceiptOrPixels(t *testing.T) {
	for _, content := range []bool{false, true} {
		h, p, token, in := humanImageHTTP(t)
		p.finalErr = identity.ErrUnauthorized
		verb, path, body := "POST", "/v1/me/conversations/"+humanHTTPConv+"/image-message-operations", humanImageHTTPBody(in)
		if content {
			verb, path, body = "GET", "/v1/me/conversations/"+humanHTTPConv+"/messages/"+privateProfileHTTPAgent+"/image", ""
		}
		w := messagePolicyUnitRequest(h, verb, path, body, token, nil)
		if w.Code == 200 || w.Code == 201 || bytes.Contains(w.Body.Bytes(), p.derivative.Full) || strings.Contains(w.Body.String(), p.receipt.MessageID) {
			t.Fatal("late unauthorized image bytes/receipt released")
		}
	}
}
