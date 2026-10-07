package agentmemory

import (
	"bytes"
	"encoding/json"
	"io"
	"math"
	"strconv"
	"time"
	"unicode"
	"unicode/utf8"
)

// jsonTree rejects duplicate decoded keys at every depth before normal
// unmarshalling can erase them. Its bounds apply to all structured containers.
type jsonTree struct {
	decoder *json.Decoder
	nodes   int
	max     int
	depth   int
}

func validJSONString(value string, key bool) bool {
	if !utf8.ValidString(value) || (key && (value == "" || len(value) > MaxStructuredKeyBytes)) {
		return false
	}
	for _, character := range value {
		if character == utf8.RuneError || (unicode.IsControl(character) && (key || (character != '\n' && character != '\t'))) {
			return false
		}
	}
	return true
}

func (tree *jsonTree) value(depth int) (any, error) {
	tree.nodes++
	if tree.nodes > tree.max || depth > tree.depth {
		return nil, ErrInvalid
	}
	token, err := tree.decoder.Token()
	if err != nil {
		return nil, ErrInvalid
	}
	switch value := token.(type) {
	case json.Delim:
		switch value {
		case '{':
			out := map[string]any{}
			for tree.decoder.More() {
				keyToken, err := tree.decoder.Token()
				key, ok := keyToken.(string)
				if err != nil || !ok || !validJSONString(key, true) || len(out) >= MaxObjectMembers {
					return nil, ErrInvalid
				}
				if _, duplicate := out[key]; duplicate {
					return nil, ErrInvalid
				}
				child, err := tree.value(depth + 1)
				if err != nil {
					return nil, ErrInvalid
				}
				out[key] = child
			}
			end, err := tree.decoder.Token()
			if err != nil || end != json.Delim('}') {
				return nil, ErrInvalid
			}
			return out, nil
		case '[':
			out := []any{}
			for tree.decoder.More() {
				if len(out) >= MaxArrayItems {
					return nil, ErrInvalid
				}
				child, err := tree.value(depth + 1)
				if err != nil {
					return nil, ErrInvalid
				}
				out = append(out, child)
			}
			end, err := tree.decoder.Token()
			if err != nil || end != json.Delim(']') {
				return nil, ErrInvalid
			}
			return out, nil
		default:
			return nil, ErrInvalid
		}
	case string:
		if !validJSONString(value, false) {
			return nil, ErrInvalid
		}
		return value, nil
	case json.Number:
		parsed, err := strconv.ParseFloat(string(value), 64)
		if len(value) > 64 || err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return nil, ErrInvalid
		}
		return value, nil
	case bool, nil:
		return value, nil
	default:
		return nil, ErrInvalid
	}
}

func decodeObject(raw []byte, depth, nodes int) (map[string]any, error) {
	if len(raw) == 0 || len(raw) > MaxBodyBytes || !utf8.Valid(raw) {
		return nil, ErrInvalid
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	tree := jsonTree{decoder: decoder, max: nodes, depth: depth}
	value, err := tree.value(1)
	object, ok := value.(map[string]any)
	if err != nil || !ok {
		return nil, ErrInvalid
	}
	if _, err = decoder.Token(); err != io.EOF {
		return nil, ErrInvalid
	}
	return object, nil
}

// NormalizeStructuredValue accepts a jsonb read's formatting within the body
// bound, then returns fresh canonical JSON with an 8KiB semantic encoding cap.
// Data keys carry user declarations, not trusted source/permission facts.
func NormalizeStructuredValue(raw json.RawMessage) (json.RawMessage, error) {
	object, err := decodeObject(raw, MaxStructuredDepth, MaxStructuredNodes)
	if err != nil {
		return nil, ErrInvalid
	}
	encoded, err := json.Marshal(object)
	if err != nil || len(encoded) > MaxStructuredValueBytes {
		return nil, ErrInvalid
	}
	return json.RawMessage(encoded), nil
}

func integerField(object map[string]any, key string) (int64, error) {
	number, ok := object[key].(json.Number)
	if !ok {
		return 0, ErrInvalid
	}
	value, err := strconv.ParseInt(string(number), 10, 64)
	if err != nil {
		return 0, ErrInvalid
	}
	return value, nil
}

func stringField(object map[string]any, key string) (string, error) {
	value, ok := object[key].(string)
	if !ok {
		return "", ErrInvalid
	}
	return value, nil
}

// DecodePutInput checks exact seven-key wire shape, every duplicate/unknown
// key, types and bounded contents. Expiry relative to the server clock is
// checked separately by NormalizePutInput immediately before the real write.
func DecodePutInput(raw []byte) (PutInput, error) {
	object, err := decodeObject(raw, MaxStructuredDepth+1, MaxStructuredNodes+16)
	if err != nil || len(object) != 7 {
		return PutInput{}, ErrInvalid
	}
	for _, required := range []string{"expectedVersion", "memoryType", "memoryKey", "summary", "structuredValue", "visibility", "validUntil"} {
		if _, exists := object[required]; !exists {
			return PutInput{}, ErrInvalid
		}
	}
	expected, err := integerField(object, "expectedVersion")
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	stringsByKey := map[string]string{}
	for _, key := range []string{"memoryType", "memoryKey", "summary", "visibility", "validUntil"} {
		value, err := stringField(object, key)
		if err != nil {
			return PutInput{}, ErrInvalid
		}
		stringsByKey[key] = value
	}
	until, err := time.Parse(time.RFC3339Nano, stringsByKey["validUntil"])
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	if _, ok := object["structuredValue"].(map[string]any); !ok {
		return PutInput{}, ErrInvalid
	}
	value, err := json.Marshal(object["structuredValue"])
	if err != nil {
		return PutInput{}, ErrInvalid
	}
	return normalizePutShape(PutInput{ExpectedVersion: expected,
		MemoryType: MemoryType(stringsByKey["memoryType"]), MemoryKey: stringsByKey["memoryKey"],
		Summary: stringsByKey["summary"], StructuredValue: value,
		Visibility: Visibility(stringsByKey["visibility"]), ValidUntil: until})
}

func DecodeDeleteInput(raw []byte) (DeleteInput, error) {
	object, err := decodeObject(raw, 2, 2)
	if err != nil || len(object) != 1 {
		return DeleteInput{}, ErrInvalid
	}
	version, err := integerField(object, "expectedVersion")
	if err != nil {
		return DeleteInput{}, ErrInvalid
	}
	input := DeleteInput{ExpectedVersion: version}
	if ValidateDeleteInput(input) != nil {
		return DeleteInput{}, ErrInvalid
	}
	return input, nil
}

// Default json.Unmarshal must not bypass the strict domain decoder.
func (input *PutInput) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodePutInput(raw)
	if err != nil {
		*input = PutInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}

func (input *DeleteInput) UnmarshalJSON(raw []byte) error {
	decoded, err := DecodeDeleteInput(raw)
	if err != nil {
		*input = DeleteInput{}
		return ErrInvalid
	}
	*input = decoded
	return nil
}
