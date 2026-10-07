package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const publicProfileMaxBodyBytes = 8 * 1024

func publicProfileError(w http.ResponseWriter, status int, code, message string) {
	respond(w, status, map[string]any{"error": map[string]string{"code": code, "message": message}})
}

// Never expose raw parser, SQL, session or user-text errors in responses/logs.
func publicProfileFailure(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, identity.ErrUnauthorized):
		w.Header().Set("WWW-Authenticate", "Bearer")
		publicProfileError(w, 401, "unauthorized", "登录已失效，请重新登录")
	case errors.Is(err, identity.ErrInvalidProfile):
		publicProfileError(w, 400, "invalid_profile", "请检查资料内容与格式")
	case errors.Is(err, identity.ErrNotFound):
		publicProfileError(w, 404, "not_found", "当前账号的资料不存在")
	default:
		publicProfileError(w, 503, "profile_unavailable", "资料服务暂不可用，请稍后重试")
	}
}

func decodePublicProfile(w http.ResponseWriter, r *http.Request) (identity.ProfileInput, error) {
	if r.Body == nil {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	headers := r.Header.Values("Content-Type")
	if len(headers) != 1 {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	media, params, err := mime.ParseMediaType(headers[0])
	if err != nil || !strings.EqualFold(media, "application/json") {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	for name, value := range params {
		if name != "charset" || !strings.EqualFold(value, "utf-8") {
			return identity.ProfileInput{}, identity.ErrInvalidProfile
		}
	}
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, publicProfileMaxBodyBytes))
	if err != nil || !utf8.Valid(raw) {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	fields := map[string]string{}
	for decoder.More() {
		token, err = decoder.Token()
		key, ok := token.(string)
		if err != nil || !ok || (key != "displayName" && key != "bio" && key != "visibility") {
			return identity.ProfileInput{}, identity.ErrInvalidProfile
		}
		if _, exists := fields[key]; exists {
			return identity.ProfileInput{}, identity.ErrInvalidProfile
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return identity.ProfileInput{}, identity.ErrInvalidProfile
		}
		text, err := publicProfileJSONString(value)
		if err != nil {
			return identity.ProfileInput{}, err
		}
		fields[key] = text
	}
	if token, err = decoder.Token(); err != nil || token != json.Delim('}') || len(fields) != 3 {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	if _, err = decoder.Token(); err != io.EOF {
		return identity.ProfileInput{}, identity.ErrInvalidProfile
	}
	return identity.NormalizeHumanProfileInput(identity.ProfileInput{DisplayName: fields["displayName"], Bio: fields["bio"], Visibility: fields["visibility"]})
}

// encoding/json replaces lone UTF16 surrogates silently. Validate escapes
// before decoding, while preserving valid emoji pairs and literal U+FFFD.
func publicProfileJSONString(raw []byte) (string, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) < 2 || raw[0] != '"' || raw[len(raw)-1] != '"' || !json.Valid(raw) || !utf8.Valid(raw) {
		return "", identity.ErrInvalidProfile
	}
	for i := 1; i < len(raw)-1; i++ {
		if raw[i] != '\\' {
			continue
		}
		i++
		if raw[i] != 'u' {
			continue
		}
		code, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return "", identity.ErrInvalidProfile
		}
		i += 4
		if code >= 0xdc00 && code <= 0xdfff {
			return "", identity.ErrInvalidProfile
		}
		if code >= 0xd800 && code <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return "", identity.ErrInvalidProfile
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return "", identity.ErrInvalidProfile
			}
			i += 6
		}
	}
	var text string
	if json.Unmarshal(raw, &text) != nil {
		return "", identity.ErrInvalidProfile
	}
	return text, nil
}
