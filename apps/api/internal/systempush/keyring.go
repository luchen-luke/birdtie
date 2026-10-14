package systempush

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"os"
	"regexp"
	"sync/atomic"
)

var ErrKeyring = errors.New("system push keyring unavailable")

type keySnapshot struct {
	active      string
	keys        map[string][32]byte
	fingerprint [32]byte
}
type Keyring struct{ current atomic.Pointer[keySnapshot] }
type KeyringDocument struct {
	Schema         string            `json:"schema"`
	ActiveKeyID    string            `json:"activeKeyId"`
	Keys           map[string]string `json:"keys"`
	FingerprintKey string            `json:"fingerprintKey"`
}

var keyIDPattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{1,64}$`)

func ParseKeyring(raw []byte) (*Keyring, error) {
	s, e := parseKeys(raw)
	if e != nil {
		return nil, e
	}
	r := &Keyring{}
	r.current.Store(s)
	return r, nil
}

// Duplicate JSON keys are rejected, including inside the key map.
func uniqueJSON(raw []byte) error {
	d := json.NewDecoder(bytes.NewReader(raw))
	var walk func() error
	walk = func() error {
		t, e := d.Token()
		if e != nil {
			return e
		}
		if delim, ok := t.(json.Delim); ok {
			switch delim {
			case '{':
				seen := map[string]bool{}
				for d.More() {
					k, e := d.Token()
					if e != nil {
						return e
					}
					s, ok := k.(string)
					if !ok || seen[s] {
						return ErrKeyring
					}
					seen[s] = true
					if e = walk(); e != nil {
						return e
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim('}') {
					return ErrKeyring
				}
			case '[':
				for d.More() {
					if e = walk(); e != nil {
						return e
					}
				}
				t, e = d.Token()
				if e != nil || t != json.Delim(']') {
					return ErrKeyring
				}
			default:
				return ErrKeyring
			}
		}
		return nil
	}
	if e := walk(); e != nil {
		return ErrKeyring
	}
	if _, e := d.Token(); e != io.EOF {
		return ErrKeyring
	}
	return nil
}
func parseKeys(raw []byte) (*keySnapshot, error) {
	if len(raw) == 0 || len(raw) > 16<<10 || uniqueJSON(raw) != nil {
		return nil, ErrKeyring
	}
	var doc KeyringDocument
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if d.Decode(&doc) != nil || doc.Schema != "birdtie-push-keyring-v1" || len(doc.Keys) == 0 || len(doc.Keys) > 16 || !keyIDPattern.MatchString(doc.ActiveKeyID) {
		return nil, ErrKeyring
	}
	parse := func(s string) ([32]byte, error) {
		var key [32]byte
		b, e := base64.StdEncoding.DecodeString(s)
		if e != nil || len(b) != 32 || base64.StdEncoding.EncodeToString(b) != s {
			return key, ErrKeyring
		}
		copy(key[:], b)
		if key == ([32]byte{}) {
			return key, ErrKeyring
		}
		return key, nil
	}
	s := &keySnapshot{active: doc.ActiveKeyID, keys: map[string][32]byte{}}
	var e error
	if s.fingerprint, e = parse(doc.FingerprintKey); e != nil {
		return nil, e
	}
	seen := map[[32]byte]bool{s.fingerprint: true}
	for id, v := range doc.Keys {
		if !keyIDPattern.MatchString(id) {
			return nil, ErrKeyring
		}
		k, e := parse(v)
		if e != nil || seen[k] {
			return nil, ErrKeyring
		}
		seen[k] = true
		s.keys[id] = k
	}
	if _, ok := s.keys[s.active]; !ok {
		return nil, ErrKeyring
	}
	return s, nil
}
func readProtectedFile(path string, max int64) ([]byte, error) {
	f, e := os.Open(path)
	if e != nil {
		return nil, ErrKeyring
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > max {
		return nil, ErrKeyring
	}
	// On Unix this enforces owner-only files; Windows access is governed by ACLs.
	if info.Mode().Perm()&0077 != 0 && os.PathSeparator != '\\' {
		return nil, ErrKeyring
	}
	b, e := io.ReadAll(io.LimitReader(f, max+1))
	if e != nil || int64(len(b)) > max {
		return nil, ErrKeyring
	}
	return b, nil
}
func LoadKeyringFile(path string) (*Keyring, error) {
	b, e := readProtectedFile(path, 16<<10)
	if e != nil {
		return nil, ErrKeyring
	}
	defer clear(b)
	return ParseKeyring(b)
}

// Reload retains every old decryption key and a stable fingerprint key. Retiring
// old keys is an explicit process restart after a completed database rotation.
func (r *Keyring) ReloadFile(path string) error {
	b, e := readProtectedFile(path, 16<<10)
	if e != nil {
		return e
	}
	defer clear(b)
	return r.Reload(b)
}
func (r *Keyring) Reload(raw []byte) error {
	if r == nil {
		return ErrKeyring
	}
	next, e := parseKeys(raw)
	if e != nil {
		return e
	}
	for {
		old := r.current.Load()
		if old == nil {
			return ErrKeyring
		}
		if subtle.ConstantTimeCompare(old.fingerprint[:], next.fingerprint[:]) != 1 {
			return ErrKeyring
		}
		for id, k := range old.keys {
			if next.keys[id] != k {
				return ErrKeyring
			}
		}
		if r.current.CompareAndSwap(old, next) {
			return nil
		}
	}
}
func (r *Keyring) ActiveKeyID() string {
	if r == nil || r.current.Load() == nil {
		return ""
	}
	return r.current.Load().active
}

type Ciphertext struct {
	KeyID      string
	Nonce      []byte
	Ciphertext []byte
}

func (r *Keyring) Seal(address SecretAddress, aad []byte) (Ciphertext, error) {
	if r == nil || r.current.Load() == nil {
		return Ciphertext{}, ErrKeyring
	}
	s := r.current.Load()
	k := s.keys[s.active]
	b, e := aes.NewCipher(k[:])
	if e != nil {
		return Ciphertext{}, ErrKeyring
	}
	g, e := cipher.NewGCM(b)
	if e != nil {
		return Ciphertext{}, ErrKeyring
	}
	nonce := make([]byte, g.NonceSize())
	if _, e = rand.Read(nonce); e != nil {
		return Ciphertext{}, ErrKeyring
	}
	auth := append(append([]byte{}, aad...), []byte("\x00"+s.active)...)
	return Ciphertext{s.active, nonce, g.Seal(nil, nonce, address.Bytes(), auth)}, nil
}
func (r *Keyring) Open(c Ciphertext, aad []byte) (SecretAddress, error) {
	if r == nil || r.current.Load() == nil {
		return SecretAddress{}, ErrKeyring
	}
	s := r.current.Load()
	k, ok := s.keys[c.KeyID]
	if !ok {
		return SecretAddress{}, ErrKeyring
	}
	b, e := aes.NewCipher(k[:])
	if e != nil {
		return SecretAddress{}, ErrKeyring
	}
	g, e := cipher.NewGCM(b)
	if e != nil || len(c.Nonce) != g.NonceSize() {
		return SecretAddress{}, ErrKeyring
	}
	auth := append(append([]byte{}, aad...), []byte("\x00"+c.KeyID)...)
	v, e := g.Open(nil, c.Nonce, c.Ciphertext, auth)
	if e != nil {
		return SecretAddress{}, ErrKeyring
	}
	defer clear(v)
	return Address(string(v)), nil
}
func (r *Keyring) Fingerprint(k Kind, application, environment string, address SecretAddress) ([32]byte, error) {
	if r == nil || r.current.Load() == nil {
		return [32]byte{}, ErrKeyring
	}
	s := r.current.Load()
	h := hmac.New(sha256.New, s.fingerprint[:])
	h.Write([]byte(string(k) + "\x00" + application + "\x00" + environment + "\x00"))
	h.Write(address.Bytes())
	var out [32]byte
	copy(out[:], h.Sum(nil))
	return out, nil
}
