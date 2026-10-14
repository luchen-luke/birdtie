package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/media"
)

func (s *server) imageMessageAccess(w http.ResponseWriter, r *http.Request, content bool) (ea.Access, connection.HumanImageMessageStore, string, bool) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := s.connections.(connection.HumanImageMessageStore)
	if !ok || messageOperationMissing(p) || messageOperationMissing(s.access) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, "", false
	}
	variant := "thumbnail"
	authRequest := r
	if content {
		if r.URL.ForceQuery || (r.URL.RawQuery != "" && r.URL.RawQuery != "variant=thumbnail" && r.URL.RawQuery != "variant=full") {
			messagePolicyFailure(w, mp.ErrInvalid)
			return ea.Access{}, nil, "", false
		}
		if r.URL.RawQuery == "variant=full" {
			variant = "full"
		}
		// Only the already validated closed media variant is removed. All normal
		// identity/workspace/Session guards remain in messageWriteAccess.
		authRequest = r.Clone(r.Context())
		urlCopy := *r.URL
		urlCopy.RawQuery, urlCopy.ForceQuery = "", false
		authRequest.URL = &urlCopy
	}
	a, ok := s.messageWriteAccess(w, authRequest)
	if !ok {
		return a, nil, "", false
	}
	id := r.PathValue("conversationID")
	if !uuidPath.MatchString(id) || id != strings.ToLower(id) {
		respondError(w, 400, "invalid_conversation_id")
		return a, nil, "", false
	}
	return a, p, variant, true
}
func (s *server) sendHumanImageMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, _, ok := s.imageMessageAccess(w, r, false)
	if !ok {
		return
	}
	if r.Body == nil {
		respondError(w, 400, "invalid_image_message")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, connection.MaxChatImageJSONBytes+1))
	if e != nil {
		respondError(w, 400, "invalid_image_message")
		return
	}
	in, e := connection.DecodeImageMessage(raw)
	if e != nil {
		respondError(w, 400, "invalid_image_message")
		return
	}
	out, e := p.SendHumanImageMessage(r.Context(), a, r.PathValue("conversationID"), in)
	if errors.Is(e, connection.ErrInvalidImage) {
		respondError(w, 400, "invalid_image_message")
		return
	}
	if messageOperationError(w, e) {
		return
	}
	if !connection.ImageReceiptMatchesInput(out, r.PathValue("conversationID"), in) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.imageMessageResponse(w, r, a, p, out, in.OperationID, 201)
}
func (s *server) readHumanImageMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, _, ok := s.imageMessageAccess(w, r, false)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	if !uuidPath.MatchString(op) || op != strings.ToLower(op) {
		respondError(w, 400, "invalid_image_operation")
		return
	}
	if !messagePolicyNoBody(w, r) {
		return
	}
	out, e := p.ReadHumanImageMessageOperation(r.Context(), a, r.PathValue("conversationID"), op)
	if messageOperationError(w, e) {
		return
	}
	s.imageMessageResponse(w, r, a, p, out, op, 200)
}
func (s *server) imageMessageResponse(w http.ResponseWriter, r *http.Request, a ea.Access, p connection.HumanImageMessageStore, out connection.ImageMessageReceipt, op string, status int) {
	if !connection.ValidImageMessageReceipt(out, a.Actor.ID, r.PathValue("conversationID"), op) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw) > 4096 {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	if e = p.ValidateHumanImageMessageResponse(r.Context(), a, out); messageOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write(append(raw, '\n'))
}
func (s *server) readHumanImageMessageContent(w http.ResponseWriter, r *http.Request) {
	a, p, variant, ok := s.imageMessageAccess(w, r, true)
	if !ok {
		return
	}
	message := r.PathValue("messageID")
	if !uuidPath.MatchString(message) || message != strings.ToLower(message) {
		respondError(w, 400, "invalid_image_message_id")
		return
	}
	if !messagePolicyNoBody(w, r) {
		return
	}
	var meta connection.ImageDerivativeMetadata
	raw, e := p.ReadHumanImageMessageContent(r.Context(), a, r.PathValue("conversationID"), message, variant, func(receipt connection.ImageMessageReceipt, m connection.ImageDerivativeMetadata, b []byte) ([]byte, error) {
		expected, valid := connection.ImageVariantMetadata(receipt.Image, variant)
		if receipt.MessageID != message || receipt.ConversationID != r.PathValue("conversationID") || !connection.ValidImageMessageReceipt(receipt, receipt.OwnerID, receipt.ConversationID, receipt.OperationID) || !valid || m != expected || !connection.ValidImageContent(m, b, variant == "thumbnail") || media.ValidateChatImageDerivative(r.Context(), b, m.MIME, m.Width, m.Height) != nil {
			return nil, connection.ErrConflict
		}
		meta = m
		return append([]byte(nil), b...), nil
	})
	if messageOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil || !connection.ValidImageContent(meta, raw, variant == "thumbnail") {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", meta.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ByteSize, 10))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	w.Header().Set("X-Image-Width", strconv.Itoa(meta.Width))
	w.Header().Set("X-Image-Height", strconv.Itoa(meta.Height))
	w.Header().Set("X-Image-Variant", variant)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline; filename=image")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
