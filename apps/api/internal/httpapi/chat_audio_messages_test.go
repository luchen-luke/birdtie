package httpapi

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/connection"
	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

// A structural fixture exercises the wire; these bytes are not microphone or
// acoustic evidence. The container uses the same track/sample limits as a send.
func humanAudioHTTPInput() connection.AudioMessageInput {
	atom := func(kind string, values ...[]byte) []byte {
		n := 8
		for _, b := range values {
			n += len(b)
		}
		out := make([]byte, 8, n)
		binary.BigEndian.PutUint32(out, uint32(n))
		copy(out[4:], kind)
		for _, b := range values {
			out = append(out, b...)
		}
		return out
	}
	ftyp := atom("ftyp", []byte("isom\x00\x00\x00\x00isom"))
	mdat := atom("mdat", []byte{1, 2, 3, 4})
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	binary.BigEndian.PutUint32(mdhd[16:], 1234)
	hdlr := make([]byte, 12)
	copy(hdlr[8:], "soun")
	entry := make([]byte, 28)
	binary.BigEndian.PutUint16(entry[6:], 1)
	binary.BigEndian.PutUint16(entry[16:], 1)
	binary.BigEndian.PutUint16(entry[18:], 16)
	binary.BigEndian.PutUint32(entry[24:], 44100<<16)
	entry = append(entry, atom("esds", []byte{0, 0, 0, 0, 4, 1, 0x40, 0})...)
	stsd := append([]byte{0, 0, 0, 0, 0, 0, 0, 1}, atom("mp4a", entry)...)
	stsz := []byte{0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 1}
	stco := make([]byte, 12)
	binary.BigEndian.PutUint32(stco[4:], 1)
	binary.BigEndian.PutUint32(stco[8:], uint32(len(ftyp)+8))
	dref := atom("dref", []byte{0, 0, 0, 0, 0, 0, 0, 1}, atom("url ", []byte{0, 0, 0, 1}))
	stbl := atom("stbl", atom("stsd", stsd), atom("stsz", stsz), atom("stco", stco))
	minf := atom("minf", atom("dinf", dref), stbl)
	mdia := atom("mdia", atom("mdhd", mdhd), atom("hdlr", hdlr), minf)
	b := append(append(ftyp, mdat...), atom("moov", atom("trak", mdia))...)
	h := sha256.Sum256(b)
	return connection.AudioMessageInput{OperationID: humanHTTPOp, AudioMetadata: connection.AudioMetadata{MIME: "audio/mp4", ByteSize: int64(len(b)), SHA256: hex.EncodeToString(h[:]), DurationMillis: 1234}, Content: b}
}

type humanAudioHTTPSpy struct {
	connection.Store
	sent, read, content, final int
	access                     ea.Access
	in                         connection.AudioMessageInput
	receipt                    connection.AudioMessageReceipt
	bytes                      []byte
	err, finalErr              error
	after                      func()
}

func (p *humanAudioHTTPSpy) SendHumanAudioMessage(_ context.Context, a ea.Access, _ string, in connection.AudioMessageInput) (connection.AudioMessageReceipt, error) {
	p.sent++
	p.access = a
	p.in = in
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.err
}
func (p *humanAudioHTTPSpy) ReadHumanAudioMessageOperation(_ context.Context, a ea.Access, _, _ string) (connection.AudioMessageReceipt, error) {
	p.read++
	p.access = a
	if p.after != nil {
		p.after()
	}
	return p.receipt, p.err
}
func (p *humanAudioHTTPSpy) ValidateHumanAudioMessageResponse(_ context.Context, _ ea.Access, _ connection.AudioMessageReceipt) error {
	p.final++
	return p.finalErr
}
func (p *humanAudioHTTPSpy) ReadHumanAudioMessageContent(_ context.Context, a ea.Access, _, _ string, encode connection.AudioContentEncode) ([]byte, error) {
	p.content++
	p.access = a
	if p.err != nil {
		return nil, p.err
	}
	b, e := encode(p.receipt, p.bytes)
	if e != nil {
		return nil, e
	}
	if p.after != nil {
		p.after()
	}
	if p.finalErr != nil {
		return nil, p.finalErr
	}
	return b, nil
}
func humanAudioHTTP(t *testing.T) (http.Handler, *humanAudioHTTPSpy, string) {
	_, _, a, token := messagePolicyUnitHTTP(t)
	in := humanAudioHTTPInput()
	p := &humanAudioHTTPSpy{bytes: in.Content, receipt: connection.NewAudioMessageReceipt(privateProfileHTTPOwner, humanHTTPConv, humanHTTPOp, privateProfileHTTPAgent, in.AudioMetadata, time.Date(2026, 10, 10, 1, 2, 3, 0, time.UTC))}
	return New(nil, a, nil, nil, nil, nil, nil, nil, nil, nil, p, nil, false, nil, nil, nil), p, token
}
func humanAudioHTTPBody(in connection.AudioMessageInput) string {
	raw, _ := json.Marshal(map[string]any{"operationId": in.OperationID, "mimeType": in.MIME, "byteSize": in.ByteSize, "sha256": in.SHA256, "durationMillis": in.DurationMillis, "contentBase64": base64.StdEncoding.EncodeToString(in.Content)})
	return string(raw)
}
func TestHumanAudioRegisteredRoutesAndExplicitReceipt(t *testing.T) {
	for _, method := range []string{"POST", "GET", "CONTENT"} {
		t.Run(method, func(t *testing.T) {
			h, p, token := humanAudioHTTP(t)
			path := "/v1/me/conversations/" + humanHTTPConv + "/audio-message-operations"
			body := ""
			verb := "GET"
			want := 200
			switch method {
			case "POST":
				verb = "POST"
				body = humanAudioHTTPBody(humanAudioHTTPInput())
				want = 201
			case "GET":
				path += "/" + humanHTTPOp
			case "CONTENT":
				path = "/v1/me/conversations/" + humanHTTPConv + "/messages/" + privateProfileHTTPAgent + "/audio"
			}
			w := messagePolicyUnitRequest(h, verb, path, body, token, nil)
			if w.Code != want || p.sent+p.read+p.content != 1 || p.access.Actor.ID != privateProfileHTTPOwner || w.Header().Get("Cache-Control") != "no-store" {
				t.Fatalf("route %s status=%d calls=%d", method, w.Code, p.sent+p.read+p.content)
			}
			if method == "CONTENT" {
				if !bytes.Equal(w.Body.Bytes(), p.bytes) || w.Header().Get("Content-Type") != "audio/mp4" || w.Header().Get("X-Content-SHA256") != p.receipt.SHA256 {
					t.Fatal("content/metadata do not bind the authorized audio")
				}
			} else if p.final != 1 || strings.Contains(w.Body.String(), "contentBase64") {
				t.Fatal("receipt must be validated and contain no private audio bytes")
			}
		})
	}
	missing := New(nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, nil, false, nil, nil, nil)
	for _, path := range []string{"/audio-message-operations", "/audio-message-operations/" + humanHTTPOp, "/messages/" + privateProfileHTTPAgent + "/audio"} {
		w := httptest.NewRecorder()
		method := "GET"
		if path == "/audio-message-operations" {
			method = "POST"
		}
		missing.ServeHTTP(w, httptest.NewRequest(method, "/v1/me/conversations/"+humanHTTPConv+path, nil))
		if w.Code != 503 {
			t.Fatal("route missing or permits unconfigured store", w.Code)
		}
	}
}
func TestHumanAudioSendDenialsAndLateResponse(t *testing.T) {
	for _, tc := range []string{"anonymous", "organization", "owner-override", "duplicate-field", "blocked", "nonparticipant", "expired-session", "late-session", "late-block", "late-cancel", "mismatched-audio", "missing-schema"} {
		t.Run(tc, func(t *testing.T) {
			h, p, token := humanAudioHTTP(t)
			body := humanAudioHTTPBody(humanAudioHTTPInput())
			want, calls := 503, 1
			var change func(*http.Request)
			switch tc {
			case "anonymous":
				token = ""
				want, calls = 401, 0
			case "organization":
				change = func(r *http.Request) { r.Header.Set("X-Birdtie-Organization-Workspace", privateProfileHTTPForeign) }
				want, calls = 403, 0
			case "owner-override":
				body = strings.TrimSuffix(body, "}") + `,"ownerId":"` + privateProfileHTTPForeign + `"}`
				want, calls = 400, 0
			case "duplicate-field":
				body = strings.TrimSuffix(body, "}") + `,"durationMillis":1}`
				want, calls = 400, 0
			case "blocked", "nonparticipant":
				p.err = connection.ErrNotFound
				want = 404
			case "expired-session":
				p.err = identity.ErrUnauthorized
				want = 401
			case "late-session":
				p.finalErr = identity.ErrUnauthorized
				want = 401
			case "late-block":
				p.finalErr = connection.ErrNotFound
				want = 404
			case "late-cancel":
				change = func(r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					*r = *r.WithContext(ctx)
					p.after = cancel
				}
			case "mismatched-audio":
				p.receipt.DurationMillis++
			case "missing-schema":
				p.err = errors.New("synthetic migration unavailable")
			}
			w := messagePolicyUnitRequest(h, "POST", "/v1/me/conversations/"+humanHTTPConv+"/audio-message-operations", body, token, change)
			if w.Code != want || p.sent != calls || strings.Contains(w.Body.String(), `"messageId"`) {
				t.Fatalf("%s status=%d sends=%d body=%s", tc, w.Code, p.sent, w.Body.String())
			}
		})
	}
}
func TestHumanAudioContentDenialsDoNotReleaseBytes(t *testing.T) {
	for _, tc := range []string{"anonymous", "blocked", "nonparticipant", "late-session", "late-block", "late-cancel", "foreign-message", "corrupt-content"} {
		t.Run(tc, func(t *testing.T) {
			h, p, token := humanAudioHTTP(t)
			want := 404
			var change func(*http.Request)
			switch tc {
			case "anonymous":
				token = ""
				want = 401
			case "blocked", "nonparticipant":
				p.err = connection.ErrNotFound
			case "late-session":
				p.finalErr = identity.ErrUnauthorized
				want = 401
			case "late-block":
				p.finalErr = connection.ErrNotFound
			case "late-cancel":
				want = 503
				change = func(r *http.Request) {
					ctx, cancel := context.WithCancel(r.Context())
					*r = *r.WithContext(ctx)
					p.after = cancel
				}
			case "foreign-message":
				p.receipt.MessageID = privateProfileHTTPForeign
				want = 409
			case "corrupt-content":
				p.bytes = append([]byte(nil), p.bytes...)
				p.bytes[len(p.bytes)-1] ^= 1
				want = 409
			}
			w := messagePolicyUnitRequest(h, "GET", "/v1/me/conversations/"+humanHTTPConv+"/messages/"+privateProfileHTTPAgent+"/audio", "", token, change)
			if w.Code != want || w.Header().Get("Content-Type") == "audio/mp4" || bytes.Equal(w.Body.Bytes(), p.bytes) {
				t.Fatalf("denied audio leaked: %s status=%d", tc, w.Code)
			}
		})
	}
}
