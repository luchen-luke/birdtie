// Package audittrace carries correlation only. A request ID is untrusted client
// text, never identity, consent, authorization, a logical operation or an effect.
package audittrace

import "context"

type requestKey struct{}

func ValidRequestID(id string) bool {
	if len(id) < 8 || len(id) > 64 {
		return false
	}
	for _, c := range []byte(id) {
		if !(c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func WithRequestID(ctx context.Context, id string) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if !ValidRequestID(id) {
		id = ""
	}
	return context.WithValue(ctx, requestKey{}, id)
}

func RequestID(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(requestKey{}).(string)
	if !ValidRequestID(id) {
		return ""
	}
	return id
}
