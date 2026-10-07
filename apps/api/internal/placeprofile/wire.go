package placeprofile

import (
	"bytes"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"time"
	"unicode/utf8"
)

const MaxInputBytes = 16384

var timeType = reflect.TypeOf(time.Time{})

// decodeWire rejects duplicate/case-variant/unknown fields at every depth.
// Explicit null is accepted only for optional facts, never a required object.
func decodeWire(raw []byte, target any) error {
	if len(raw) == 0 || len(raw) > MaxInputBytes || !utf8.Valid(raw) {
		return ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	if e := walkWire(d, reflect.TypeOf(target).Elem(), 0); e != nil {
		return e
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrInvalid
	}
	if e := json.Unmarshal(raw, target); e != nil {
		return ErrInvalid
	}
	return nil
}
func walkWire(d *json.Decoder, t reflect.Type, depth int) error {
	if depth > 8 {
		return ErrInvalid
	}
	tok, e := d.Token()
	if e != nil {
		return ErrInvalid
	}
	optional := t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice
	if tok == nil {
		if optional {
			return nil
		}
		return ErrInvalid
	}
	for t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t == timeType {
		if _, ok := tok.(string); !ok {
			return ErrInvalid
		}
		return nil
	}
	switch t.Kind() {
	case reflect.Struct:
		if tok != json.Delim('{') {
			return ErrInvalid
		}
		fields := map[string]reflect.Type{}
		required := map[string]bool{}
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			k := strings.Split(f.Tag.Get("json"), ",")[0]
			if k == "" || k == "-" {
				continue
			}
			fields[k] = f.Type
			if f.Type.Kind() != reflect.Pointer && f.Type.Kind() != reflect.Slice {
				required[k] = true
			}
		}
		seen := map[string]bool{}
		for d.More() {
			k, e := d.Token()
			s, ok := k.(string)
			ft, exists := fields[s]
			if e != nil || !ok || !exists || seen[s] {
				return ErrInvalid
			}
			seen[s] = true
			if walkWire(d, ft, depth+1) != nil {
				return ErrInvalid
			}
		}
		if end, e := d.Token(); e != nil || end != json.Delim('}') {
			return ErrInvalid
		}
		for k := range required {
			if !seen[k] {
				return ErrInvalid
			}
		}
	case reflect.Slice:
		if tok != json.Delim('[') {
			return ErrInvalid
		}
		n := 0
		for d.More() {
			n++
			if n > 16 || walkWire(d, t.Elem(), depth+1) != nil {
				return ErrInvalid
			}
		}
		if end, e := d.Token(); e != nil || end != json.Delim(']') {
			return ErrInvalid
		}
	case reflect.String:
		if _, ok := tok.(string); !ok {
			return ErrInvalid
		}
	case reflect.Int, reflect.Int64:
		if _, ok := tok.(json.Number); !ok {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}
func DecodeSubmit(raw []byte) (SubmitInput, error) {
	var in SubmitInput
	e := decodeWire(raw, &in)
	return in, e
}
func DecodeReview(raw []byte) (ReviewInput, error) {
	var in ReviewInput
	e := decodeWire(raw, &in)
	return in, e
}
func DecodeWithdraw(raw []byte) (WithdrawInput, error) {
	var in WithdrawInput
	e := decodeWire(raw, &in)
	return in, e
}
