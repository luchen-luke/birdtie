package systempush

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sync"
	"testing"
)

func testKeys(t *testing.T, active string, values map[string]byte) []byte {
	t.Helper()
	d := KeyringDocument{Schema: "birdtie-push-keyring-v1", ActiveKeyID: active, Keys: map[string]string{}, FingerprintKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{99}, 32))}
	for id, v := range values {
		d.Keys[id] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32))
	}
	b, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func TestSystemPushKeyringPersistentAADRotationAndRedaction(t *testing.T) {
	first := testKeys(t, "one", map[string]byte{"one": 1})
	r, e := ParseKeyring(first)
	if e != nil {
		t.Fatal(e)
	}
	secret := Address("synthetic-provider-address-never-log-this-123456789")
	aad := []byte("owner/session/binding/version/provider")
	cipher, e := r.Seal(secret, aad)
	if e != nil {
		t.Fatal(e)
	}
	other, e := r.Seal(secret, aad)
	if e != nil || bytes.Equal(cipher.Nonce, other.Nonce) {
		t.Fatal("nonce reuse", e)
	}
	restarted, e := ParseKeyring(first)
	if e != nil {
		t.Fatal(e)
	}
	plain, e := restarted.Open(cipher, aad)
	if e != nil || !bytes.Equal(plain.Bytes(), secret.Bytes()) {
		t.Fatal("restart lost persistent key", e)
	}
	if _, e = r.Open(cipher, []byte("different-binding")); e == nil {
		t.Fatal("AAD not bound")
	}
	broken := cipher
	broken.Ciphertext = append([]byte{}, cipher.Ciphertext...)
	broken.Ciphertext[0] ^= 1
	if _, e = r.Open(broken, aad); e == nil {
		t.Fatal("tampering accepted")
	}
	fingerprint, _ := r.Fingerprint(FCMToken, "app", "FCM", secret)
	next := testKeys(t, "two", map[string]byte{"one": 1, "two": 2})
	if e = r.Reload(next); e != nil {
		t.Fatal(e)
	}
	fp, _ := r.Fingerprint(FCMToken, "app", "FCM", secret)
	if fp != fingerprint {
		t.Fatal("rotation changed address fingerprint")
	}
	after, e := r.Seal(secret, aad)
	if e != nil || after.KeyID != "two" {
		t.Fatal("active key not switched", e)
	}
	if _, e = r.Open(cipher, aad); e != nil {
		t.Fatal("old key lost", e)
	}
	if r.Reload(testKeys(t, "two", map[string]byte{"two": 2})) == nil || r.Reload(testKeys(t, "two", map[string]byte{"one": 3, "two": 2})) == nil {
		t.Fatal("reload removed/replaced live old key")
	}
	bad := append([]byte{}, next...)
	bad = bytes.Replace(bad, []byte(`"schema":`), []byte(`"schema":"birdtie-push-keyring-v1","schema":`), 1)
	if r.Reload(bad) == nil {
		t.Fatal("duplicate JSON key accepted")
	}
	data, _ := json.Marshal(secret)
	if bytes.Contains(data, secret.Bytes()) || bytes.Contains([]byte(fmt.Sprintf("%+v %#v", secret, secret)), secret.Bytes()) {
		t.Fatal("secret formatted")
	}
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 30; j++ {
				if r.Reload(next) != nil {
					t.Error("concurrent reload failed")
				}
				sealed, e := r.Seal(secret, aad)
				if e != nil {
					t.Error(e)
					return
				}
				if _, e = r.Open(sealed, aad); e != nil {
					t.Error(e)
				}
			}
		}()
	}
	wg.Wait()
}
func TestSystemPushConfigDefaultOffAndClosedSchemes(t *testing.T) {
	c, e := LoadConfig(func(string) (string, bool) { return "", false })
	if e != nil || c.Enabled || len(c.Kinds()) != 0 {
		t.Fatal("not default OFF")
	}
	if p, e := NewProviders(c); e != nil || len(p) != 0 {
		t.Fatal("disabled providers read config", e)
	}
	in := RegistrationInput{Schema: "system-push-registration-v1", OperationID: "11111111-1111-4111-8111-111111111111", RegistrationKind: FCMToken, Platform: "ANDROID", Permission: "AUTHORIZED", Address: Address("synthetic-fcm-registration-token-123456789")}
	if ValidateRegistration(in) != nil {
		t.Fatal("FCM_TOKEN unavailable")
	}
	in.RegistrationKind = "FID"
	if ValidateRegistration(in) == nil {
		t.Fatal("FID conflated")
	}
	in.RegistrationKind = APNSToken
	in.Platform = "IOS"
	in.Address = Address(string(bytes.Repeat([]byte{'a'}, 64)))
	in.Permission = "PROVISIONAL"
	if ValidateRegistration(in) != nil {
		t.Fatal("APNS provisional unavailable")
	}
	if _, e = ProofDigest(base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{1}, 31))); e == nil {
		t.Fatal("weak installation proof accepted")
	}
}
