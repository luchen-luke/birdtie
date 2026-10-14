package connection

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strings"
	"testing"
)

// Structurally valid synthetic container only, never real microphone evidence.
func audioTestAtom(kind string, data ...[]byte) []byte {
	n := 8
	for _, b := range data {
		n += len(b)
	}
	out := make([]byte, 8, n)
	binary.BigEndian.PutUint32(out, uint32(n))
	copy(out[4:], kind)
	for _, b := range data {
		out = append(out, b...)
	}
	return out
}
func audioTestContainer(handler string, duration uint32) []byte {
	ftyp := audioTestAtom("ftyp", []byte("isom\x00\x00\x00\x00isom"))
	mdat := audioTestAtom("mdat", []byte{1, 2, 3, 4})
	mdhd := make([]byte, 24)
	binary.BigEndian.PutUint32(mdhd[12:], 1000)
	binary.BigEndian.PutUint32(mdhd[16:], duration)
	hdlr := make([]byte, 12)
	copy(hdlr[8:], handler)
	entry := make([]byte, 28)
	binary.BigEndian.PutUint16(entry[6:], 1)
	binary.BigEndian.PutUint16(entry[16:], 1)
	binary.BigEndian.PutUint16(entry[18:], 16)
	binary.BigEndian.PutUint32(entry[24:], 44100<<16)
	entry = append(entry, audioTestAtom("esds", []byte{0, 0, 0, 0, 4, 1, 0x40, 0})...)
	stsd := []byte{0, 0, 0, 0, 0, 0, 0, 1}
	stsd = append(stsd, audioTestAtom("mp4a", entry)...)
	stsz := []byte{0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 1}
	stco := make([]byte, 12)
	binary.BigEndian.PutUint32(stco[4:], 1)
	binary.BigEndian.PutUint32(stco[8:], uint32(len(ftyp)+8))
	dref := audioTestAtom("dref", []byte{0, 0, 0, 0, 0, 0, 0, 1}, audioTestAtom("url ", []byte{0, 0, 0, 1}))
	stbl := audioTestAtom("stbl", audioTestAtom("stsd", stsd), audioTestAtom("stsz", stsz), audioTestAtom("stco", stco))
	minf := audioTestAtom("minf", audioTestAtom("dinf", dref), stbl)
	mdia := audioTestAtom("mdia", audioTestAtom("mdhd", mdhd), audioTestAtom("hdlr", hdlr), minf)
	return append(append(ftyp, mdat...), audioTestAtom("moov", audioTestAtom("trak", mdia))...)
}
func audioTestInput() AudioMessageInput {
	b := audioTestContainer("soun", 1234)
	h := sha256.Sum256(b)
	return AudioMessageInput{OperationID: humanMessageOpForAudio, AudioMetadata: AudioMetadata{MIME: "audio/mp4", ByteSize: int64(len(b)), SHA256: hex.EncodeToString(h[:]), DurationMillis: 1234}, Content: b}
}

const humanMessageOpForAudio = "55555555-5555-4555-8555-555555555555"

func TestAudioMessageContainerAndClosedWire(t *testing.T) {
	in := audioTestInput()
	if e := ValidateAudioMessage(in); e != nil {
		t.Fatal(e)
	}
	wire := map[string]any{"operationId": in.OperationID, "mimeType": in.MIME, "byteSize": in.ByteSize, "sha256": in.SHA256, "durationMillis": in.DurationMillis, "contentBase64": base64.StdEncoding.EncodeToString(in.Content)}
	raw, _ := json.Marshal(wire)
	got, e := DecodeAudioMessage(raw)
	if e != nil || got.AudioMetadata != in.AudioMetadata {
		t.Fatal("valid closed wire", e)
	}
	for _, change := range []string{"duplicate", "owner", "null", "hash", "size", "duration", "mime", "video", "oversize", "bad-base64", "zero-clock", "external-url", "broken-offset", "truncated"} {
		t.Run(change, func(t *testing.T) {
			v := audioTestInput()
			v.Content = append([]byte(nil), v.Content...)
			switch change {
			case "duplicate":
				raw = append(raw[:len(raw)-1], []byte(`,"byteSize":1}`)...)
			case "owner":
				wire["ownerId"] = "00000000-0000-4000-8000-000000000000"
			case "null":
				wire["mimeType"] = nil
			case "hash":
				v.SHA256 = strings.Repeat("0", 64)
			case "size":
				v.ByteSize++
			case "duration":
				v.DurationMillis++
			case "mime":
				v.MIME = "audio/mpeg"
			case "video":
				v.Content = audioTestContainer("vide", 1234)
			case "oversize":
				v.Content = make([]byte, MaxChatAudioBytes+1)
			case "bad-base64":
				wire["contentBase64"] = "!"
			case "zero-clock":
				v.Content = audioTestContainer("soun", 0)
			case "external-url":
				p := strings.Index(string(v.Content), "url ")
				v.Content[p+7] = 0
			case "broken-offset":
				p := strings.Index(string(v.Content), "stco")
				binary.BigEndian.PutUint32(v.Content[p+12:], 0)
			case "truncated":
				v.Content = v.Content[:len(v.Content)-1]
			}
			if change == "duplicate" {
				if _, e = DecodeAudioMessage(raw); e == nil {
					t.Fatal("duplicate accepted")
				}
				raw, _ = json.Marshal(map[string]any{"operationId": in.OperationID, "mimeType": in.MIME, "byteSize": in.ByteSize, "sha256": in.SHA256, "durationMillis": in.DurationMillis, "contentBase64": base64.StdEncoding.EncodeToString(in.Content)})
				return
			}
			if change == "owner" || change == "null" || change == "bad-base64" {
				b, _ := json.Marshal(wire)
				if _, e = DecodeAudioMessage(b); e == nil {
					t.Fatal("invalid wire accepted")
				}
				delete(wire, "ownerId")
				wire["mimeType"] = in.MIME
				wire["contentBase64"] = base64.StdEncoding.EncodeToString(in.Content)
				return
			}
			if change == "video" || change == "zero-clock" || change == "external-url" || change == "broken-offset" || change == "truncated" {
				h := sha256.Sum256(v.Content)
				v.SHA256 = hex.EncodeToString(h[:])
				v.ByteSize = int64(len(v.Content))
			}
			if ValidateAudioMessage(v) == nil {
				t.Fatal("invalid actual audio accepted")
			}
		})
	}
}
func TestAudioMessageDigestIncludesKindContentAndRecipient(t *testing.T) {
	m := audioTestInput().AudioMetadata
	a := AudioMessagePayloadDigest("33333333-3333-4333-8333-333333333333", m)
	if a == HumanMessagePayloadDigest("33333333-3333-4333-8333-333333333333", "语音消息") || a == AudioMessagePayloadDigest("44444444-4444-4444-8444-444444444444", m) {
		t.Fatal("mixed kind/recipient digest collision")
	}
	m.DurationMillis++
	if a == AudioMessagePayloadDigest("33333333-3333-4333-8333-333333333333", m) {
		t.Fatal("duration omitted")
	}
}
