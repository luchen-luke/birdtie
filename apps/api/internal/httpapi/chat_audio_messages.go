package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	mp "github.com/birdtie/birdtie/apps/api/internal/agentmessagepolicy"
	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

func (s *server) audioMessageAccess(w http.ResponseWriter, r *http.Request) (ea.Access, connection.HumanAudioMessageStore, bool) {
	w.Header().Set("Cache-Control", "no-store")
	p, ok := s.connections.(connection.HumanAudioMessageStore)
	if !ok || messageOperationMissing(p) || messageOperationMissing(s.access) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return ea.Access{}, nil, false
	}
	a, ok := s.messageWriteAccess(w, r)
	if !ok {
		return a, nil, false
	}
	id := r.PathValue("conversationID")
	if !uuidPath.MatchString(id) || id != strings.ToLower(id) {
		respondError(w, 400, "invalid_conversation_id")
		return a, nil, false
	}
	return a, p, true
}
func (s *server) sendHumanAudioMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.audioMessageAccess(w, r)
	if !ok {
		return
	}
	if r.Body == nil {
		respondError(w, 400, "invalid_audio_message")
		return
	}
	raw, e := io.ReadAll(io.LimitReader(r.Body, connection.MaxChatAudioJSONBytes+1))
	if e != nil {
		respondError(w, 400, "invalid_audio_message")
		return
	}
	in, e := connection.DecodeAudioMessage(raw)
	if e != nil {
		respondError(w, 400, "invalid_audio_message")
		return
	}
	out, e := p.SendHumanAudioMessage(r.Context(), a, r.PathValue("conversationID"), in)
	if messageOperationError(w, e) {
		return
	}
	if out.AudioMetadata != in.AudioMetadata {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	s.audioMessageResponse(w, r, a, p, out, in.OperationID, 201)
}
func (s *server) readHumanAudioMessageOperation(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.audioMessageAccess(w, r)
	if !ok {
		return
	}
	op := r.PathValue("operationID")
	if !uuidPath.MatchString(op) || op != strings.ToLower(op) {
		respondError(w, 400, "invalid_audio_operation")
		return
	}
	if !messagePolicyNoBody(w, r) {
		return
	}
	out, e := p.ReadHumanAudioMessageOperation(r.Context(), a, r.PathValue("conversationID"), op)
	if messageOperationError(w, e) {
		return
	}
	s.audioMessageResponse(w, r, a, p, out, op, 200)
}
func (s *server) audioMessageResponse(w http.ResponseWriter, r *http.Request, a ea.Access, p connection.HumanAudioMessageStore, out connection.AudioMessageReceipt, op string, status int) {
	if !connection.ValidAudioMessageReceipt(out, a.Actor.ID, r.PathValue("conversationID"), op) {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	raw, e := json.Marshal(map[string]any{"data": out})
	if e != nil || len(raw) > 4096 {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	if e = p.ValidateHumanAudioMessageResponse(r.Context(), a, out); messageOperationError(w, e) {
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
func (s *server) readHumanAudioMessageContent(w http.ResponseWriter, r *http.Request) {
	a, p, ok := s.audioMessageAccess(w, r)
	if !ok {
		return
	}
	message := r.PathValue("messageID")
	if !uuidPath.MatchString(message) || message != strings.ToLower(message) {
		respondError(w, 400, "invalid_audio_message_id")
		return
	}
	if !messagePolicyNoBody(w, r) {
		return
	}
	var meta connection.AudioMetadata
	raw, e := p.ReadHumanAudioMessageContent(r.Context(), a, r.PathValue("conversationID"), message, func(receipt connection.AudioMessageReceipt, b []byte) ([]byte, error) {
		if receipt.MessageID != message || receipt.ConversationID != r.PathValue("conversationID") || !connection.ValidAudioMessageReceipt(receipt, receipt.OwnerID, receipt.ConversationID, receipt.OperationID) || connection.ValidateAudioMessage(connection.AudioMessageInput{OperationID: receipt.OperationID, AudioMetadata: receipt.AudioMetadata, Content: b}) != nil {
			return nil, connection.ErrConflict
		}
		meta = receipt.AudioMetadata
		return append([]byte(nil), b...), nil
	})
	if messageOperationError(w, e) {
		return
	}
	if r.Context().Err() != nil || !connection.ValidAudioMetadata(meta) || int64(len(raw)) != meta.ByteSize {
		messagePolicyFailure(w, mp.ErrUnavailable)
		return
	}
	w.Header().Set("Content-Type", meta.MIME)
	w.Header().Set("Content-Length", strconv.FormatInt(meta.ByteSize, 10))
	w.Header().Set("X-Content-SHA256", meta.SHA256)
	w.Header().Set("X-Audio-Duration-Millis", strconv.FormatInt(meta.DurationMillis, 10))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Content-Disposition", "inline; filename=voice.m4a")
	w.WriteHeader(200)
	_, _ = w.Write(raw)
}
