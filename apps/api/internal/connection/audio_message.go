package connection

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"time"

	ea "github.com/birdtie/birdtie/apps/api/internal/entityaction"
)

const AudioMessageOperationSchema = "human.audio_message_operation.v1"
const MaxChatAudioBytes = 1048576
const MaxChatAudioMillis = 90000
const MaxChatAudioJSONBytes = 1400000

// Audio is a human-to-human message. It is never an Agent input or export.
type AudioMetadata struct {
	MIME           string `json:"mimeType"`
	ByteSize       int64  `json:"byteSize"`
	SHA256         string `json:"sha256"`
	DurationMillis int64  `json:"durationMillis"`
}
type AudioMessageInput struct {
	OperationID string `json:"operationId"`
	AudioMetadata
	Content []byte `json:"-"`
}
type AudioMessageReceipt struct {
	Schema         string `json:"schemaVersion"`
	OwnerID        string `json:"ownerId"`
	ConversationID string `json:"conversationId"`
	OperationID    string `json:"operationId"`
	EffectKey      string `json:"effectKey"`
	PayloadDigest  string `json:"payloadDigest"`
	MessageID      string `json:"messageId"`
	AudioMetadata
	CreatedAt time.Time `json:"createdAt"`
}
type AudioContentEncode func(AudioMessageReceipt, []byte) ([]byte, error)
type HumanAudioMessageStore interface {
	SendHumanAudioMessage(context.Context, ea.Access, string, AudioMessageInput) (AudioMessageReceipt, error)
	ReadHumanAudioMessageOperation(context.Context, ea.Access, string, string) (AudioMessageReceipt, error)
	ValidateHumanAudioMessageResponse(context.Context, ea.Access, AudioMessageReceipt) error
	ReadHumanAudioMessageContent(context.Context, ea.Access, string, string, AudioContentEncode) ([]byte, error)
}

func ValidAudioMetadata(m AudioMetadata) bool {
	return m.MIME == "audio/mp4" && m.ByteSize > 0 && m.ByteSize <= MaxChatAudioBytes &&
		m.DurationMillis > 0 && m.DurationMillis <= MaxChatAudioMillis && len(m.SHA256) == 64 && isMessageHex(m.SHA256)
}
func AudioMessagePayloadDigest(conversation string, m AudioMetadata) string {
	return messageHash("birdtie.human-audio-payload.v1\n" + conversation + "\n" + m.MIME + "\n" + strconv.FormatInt(m.ByteSize, 10) + "\n" + m.SHA256 + "\n" + strconv.FormatInt(m.DurationMillis, 10))
}

// Keep one original effect address across text/audio. Changing kind is a conflict.
func NewAudioMessageReceipt(owner, conversation, operation, message string, m AudioMetadata, at time.Time) AudioMessageReceipt {
	return AudioMessageReceipt{AudioMessageOperationSchema, owner, conversation, operation, HumanMessageEffectKey(owner, operation), AudioMessagePayloadDigest(conversation, m), message, m, at.UTC()}
}
func ValidAudioMessageReceipt(r AudioMessageReceipt, owner, conversation, op string) bool {
	return r.Schema == AudioMessageOperationSchema && r.OwnerID == owner && r.ConversationID == conversation && r.OperationID == op &&
		shareUUID.MatchString(owner) && shareUUID.MatchString(conversation) && shareUUID.MatchString(op) && shareUUID.MatchString(r.MessageID) &&
		r.EffectKey == HumanMessageEffectKey(owner, op) && r.PayloadDigest == AudioMessagePayloadDigest(conversation, r.AudioMetadata) &&
		ValidAudioMetadata(r.AudioMetadata) && !r.CreatedAt.IsZero() && r.CreatedAt.Year() >= 2020 && r.CreatedAt.Year() <= 9999
}
func DecodeAudioMessage(raw []byte) (AudioMessageInput, error) {
	var v AudioMessageInput
	if len(raw) > MaxChatAudioJSONBytes {
		return v, ErrConflict
	}
	f, e := shareObject(raw, "operationId", "mimeType", "byteSize", "sha256", "durationMillis", "contentBase64")
	if e != nil {
		return v, e
	}
	var encoded string
	if json.Unmarshal(f["operationId"], &v.OperationID) != nil || json.Unmarshal(f["mimeType"], &v.MIME) != nil || json.Unmarshal(f["byteSize"], &v.ByteSize) != nil || json.Unmarshal(f["sha256"], &v.SHA256) != nil || json.Unmarshal(f["durationMillis"], &v.DurationMillis) != nil || json.Unmarshal(f["contentBase64"], &encoded) != nil || !shareUUID.MatchString(v.OperationID) || !ValidAudioMetadata(v.AudioMetadata) || len(encoded) > base64.StdEncoding.EncodedLen(MaxChatAudioBytes) {
		return AudioMessageInput{}, ErrConflict
	}
	v.Content, e = base64.StdEncoding.Strict().DecodeString(encoded)
	if e != nil || base64.StdEncoding.EncodeToString(v.Content) != encoded || ValidateAudioMessage(v) != nil {
		return AudioMessageInput{}, ErrConflict
	}
	return v, nil
}
func ValidateAudioMessage(v AudioMessageInput) error {
	if !shareUUID.MatchString(v.OperationID) || !ValidAudioMetadata(v.AudioMetadata) || int64(len(v.Content)) != v.ByteSize {
		return ErrConflict
	}
	h := sha256.Sum256(v.Content)
	if hex.EncodeToString(h[:]) != v.SHA256 {
		return ErrConflict
	}
	d, e := M4AAudioDuration(v.Content)
	// The duration in the immutable receipt comes from the actual track clock.
	if e != nil || d != v.DurationMillis {
		return ErrConflict
	}
	return nil
}

type audioAtom struct {
	kind       string
	data       []byte
	start, end int
}

func audioAtoms(b []byte, offset int) ([]audioAtom, error) {
	out := []audioAtom{}
	for p := 0; p < len(b); {
		if len(b)-p < 8 || len(out) > 4096 {
			return nil, ErrConflict
		}
		n := uint64(binary.BigEndian.Uint32(b[p : p+4]))
		header := 8
		if n == 1 {
			if len(b)-p < 16 {
				return nil, ErrConflict
			}
			n = binary.BigEndian.Uint64(b[p+8 : p+16])
			header = 16
		}
		if n == 0 || n < uint64(header) || n > uint64(len(b)-p) {
			return nil, ErrConflict
		}
		end := p + int(n)
		out = append(out, audioAtom{string(b[p+4 : p+8]), b[p+header : end], offset + p + header, offset + end})
		p = end
	}
	return out, nil
}
func oneAudioAtom(a []audioAtom, kind string) (audioAtom, error) {
	var found audioAtom
	n := 0
	for _, v := range a {
		if v.kind == kind {
			found = v
			n++
		}
	}
	if n != 1 {
		return audioAtom{}, ErrConflict
	}
	return found, nil
}
func childAudioAtom(a audioAtom, kind string) (audioAtom, error) {
	c, e := audioAtoms(a.data, a.start)
	if e != nil {
		return audioAtom{}, e
	}
	return oneAudioAtom(c, kind)
}

// Parse the actual bounded M4A container, not caller-supplied MIME/duration.
// Exactly one local AAC sound track; video, external references, broken sample
// tables and oversized/malformed atoms are rejected. This is not a claim of
// semantic speech analysis or acoustic decoding on the server.
func M4AAudioDuration(b []byte) (int64, error) {
	if len(b) < 64 || len(b) > MaxChatAudioBytes {
		return 0, ErrConflict
	}
	a, e := audioAtoms(b, 0)
	if e != nil {
		return 0, e
	}
	ftyp, e := oneAudioAtom(a, "ftyp")
	if e != nil || len(ftyp.data) < 8 {
		return 0, ErrConflict
	}
	brand := string(ftyp.data[:4])
	if brand != "isom" && brand != "mp42" && brand != "M4A " && brand != "3gp4" && brand != "3gp6" {
		return 0, ErrConflict
	}
	mdat, e := oneAudioAtom(a, "mdat")
	if e != nil || len(mdat.data) == 0 {
		return 0, ErrConflict
	}
	moov, e := oneAudioAtom(a, "moov")
	if e != nil {
		return 0, e
	}
	for _, v := range a {
		switch v.kind {
		case "ftyp", "moov", "mdat", "free", "skip":
		default:
			return 0, ErrConflict
		}
	}
	trak, e := childAudioAtom(moov, "trak")
	if e != nil {
		return 0, e
	}
	mdia, e := childAudioAtom(trak, "mdia")
	if e != nil {
		return 0, e
	}
	hdlr, e := childAudioAtom(mdia, "hdlr")
	if e != nil || len(hdlr.data) < 12 || string(hdlr.data[8:12]) != "soun" {
		return 0, ErrConflict
	}
	mdhd, e := childAudioAtom(mdia, "mdhd")
	if e != nil || len(mdhd.data) < 20 {
		return 0, ErrConflict
	}
	var scale uint32
	var duration uint64
	switch mdhd.data[0] {
	case 0:
		scale = binary.BigEndian.Uint32(mdhd.data[12:16])
		duration = uint64(binary.BigEndian.Uint32(mdhd.data[16:20]))
	case 1:
		if len(mdhd.data) < 32 {
			return 0, ErrConflict
		}
		scale = binary.BigEndian.Uint32(mdhd.data[20:24])
		duration = binary.BigEndian.Uint64(mdhd.data[24:32])
	default:
		return 0, ErrConflict
	}
	if scale == 0 || duration == 0 || duration > uint64(scale)*90 {
		return 0, ErrConflict
	}
	millis := int64((duration*1000 + uint64(scale)/2) / uint64(scale))
	if millis < 1 || millis > MaxChatAudioMillis {
		return 0, ErrConflict
	}
	minf, e := childAudioAtom(mdia, "minf")
	if e != nil {
		return 0, e
	}
	dinf, e := childAudioAtom(minf, "dinf")
	if e != nil {
		return 0, e
	}
	dref, e := childAudioAtom(dinf, "dref")
	if e != nil || len(dref.data) < 8 || binary.BigEndian.Uint32(dref.data[4:8]) != 1 {
		return 0, ErrConflict
	}
	refs, e := audioAtoms(dref.data[8:], dref.start+8)
	if e != nil || len(refs) != 1 || refs[0].kind != "url " || len(refs[0].data) != 4 || binary.BigEndian.Uint32(refs[0].data) != 1 {
		return 0, ErrConflict
	}
	stbl, e := childAudioAtom(minf, "stbl")
	if e != nil {
		return 0, e
	}
	stsd, e := childAudioAtom(stbl, "stsd")
	if e != nil || len(stsd.data) < 8 || binary.BigEndian.Uint32(stsd.data[4:8]) != 1 {
		return 0, ErrConflict
	}
	entries, e := audioAtoms(stsd.data[8:], stsd.start+8)
	if e != nil || len(entries) != 1 || entries[0].kind != "mp4a" || len(entries[0].data) < 28 {
		return 0, ErrConflict
	}
	sample := entries[0].data
	channels := binary.BigEndian.Uint16(sample[16:18])
	rate := binary.BigEndian.Uint32(sample[24:28]) >> 16
	if binary.BigEndian.Uint16(sample[6:8]) != 1 || channels < 1 || channels > 2 || binary.BigEndian.Uint16(sample[18:20]) != 16 || rate < 8000 || rate > 96000 {
		return 0, ErrConflict
	}
	// AAC decoder configuration is required; no implicit foreign codec.
	extra, e := audioAtoms(sample[28:], entries[0].start+28)
	if e != nil {
		return 0, e
	}
	esds, e := oneAudioAtom(extra, "esds")
	if e != nil || len(esds.data) < 8 {
		return 0, ErrConflict
	}
	// A DecoderConfigDescriptor (0x04) declares MPEG-4 Audio object type 0x40.
	aac := false
	for p := 4; p+2 < len(esds.data); p++ {
		if esds.data[p] != 4 {
			continue
		}
		j := p + 1
		for k := 0; k < 4 && j < len(esds.data); k++ {
			v := esds.data[j]
			j++
			if v&128 == 0 {
				if j < len(esds.data) && esds.data[j] == 0x40 {
					aac = true
				}
				break
			}
		}
	}
	if !aac {
		return 0, ErrConflict
	}
	stsz, e := childAudioAtom(stbl, "stsz")
	if e != nil || len(stsz.data) < 12 {
		return 0, ErrConflict
	}
	fixed := binary.BigEndian.Uint32(stsz.data[4:8])
	count := binary.BigEndian.Uint32(stsz.data[8:12])
	if count < 1 || count > 100000 {
		return 0, ErrConflict
	}
	total := uint64(fixed) * uint64(count)
	if fixed == 0 {
		if uint64(len(stsz.data)) != 12+uint64(count)*4 {
			return 0, ErrConflict
		}
		for i := uint32(0); i < count; i++ {
			n := binary.BigEndian.Uint32(stsz.data[12+i*4 : 16+i*4])
			if n == 0 {
				return 0, ErrConflict
			}
			total += uint64(n)
		}
	} else if len(stsz.data) != 12 {
		return 0, ErrConflict
	}
	if total == 0 || total > uint64(len(mdat.data)) {
		return 0, ErrConflict
	}
	children, e := audioAtoms(stbl.data, stbl.start)
	if e != nil {
		return 0, e
	}
	chunkCount := 0
	for _, chunk := range children {
		width := 0
		if chunk.kind == "stco" {
			width = 4
		}
		if chunk.kind == "co64" {
			width = 8
		}
		if width == 0 {
			continue
		}
		chunkCount++
		if len(chunk.data) < 8 {
			return 0, ErrConflict
		}
		n := binary.BigEndian.Uint32(chunk.data[4:8])
		if n < 1 || n > count || uint64(len(chunk.data)) != 8+uint64(n)*uint64(width) {
			return 0, ErrConflict
		}
		for i := uint32(0); i < n; i++ {
			p := 8 + int(i)*width
			off := uint64(binary.BigEndian.Uint32(chunk.data[p : p+4]))
			if width == 8 {
				off = binary.BigEndian.Uint64(chunk.data[p : p+8])
			}
			if off < uint64(mdat.start) || off >= uint64(mdat.end) {
				return 0, ErrConflict
			}
		}
	}
	if chunkCount != 1 {
		return 0, ErrConflict
	}
	return millis, nil
}
