package agentorganization

import (
	"bytes"
	"encoding/json"
	"io"
	"strings"
	"unicode/utf8"
)

// Exactly one query. A model/client cannot choose a speaker, source or grant.
func DecodeQuery(body []byte) (string, error) {
	if len(body) == 0 || len(body) > 4096 || !utf8.Valid(body) {
		return "", ErrInvalid
	}
	d := json.NewDecoder(bytes.NewReader(body))
	token, err := d.Token()
	if err != nil || token != json.Delim('{') {
		return "", ErrInvalid
	}
	query := ""
	seen := false
	for d.More() {
		key, e := d.Token()
		if e != nil || key != "query" || seen {
			return "", ErrInvalid
		}
		seen = true
		if e = d.Decode(&query); e != nil {
			return "", ErrInvalid
		}
	}
	if token, err = d.Token(); err != nil || token != json.Delim('}') || !seen {
		return "", ErrInvalid
	}
	if _, err = d.Token(); err != io.EOF {
		return "", ErrInvalid
	}
	query = strings.TrimSpace(query)
	if !ValidQuery(query) {
		return "", ErrInvalid
	}
	return query, nil
}
