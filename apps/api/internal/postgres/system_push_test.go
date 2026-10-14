package postgres

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"testing"

	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
)

func pushTestKeys(t *testing.T, active string, keys map[string]byte) []byte {
	t.Helper()
	d := sp.KeyringDocument{Schema: "birdtie-push-keyring-v1", ActiveKeyID: active, Keys: map[string]string{}, FingerprintKey: base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{99}, 32))}
	for id, v := range keys {
		d.Keys[id] = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{v}, 32))
	}
	b, e := json.Marshal(d)
	if e != nil {
		t.Fatal(e)
	}
	return b
}
func pushTestConfig() sp.Config {
	return sp.Config{Enabled: true, KeyringFile: "synthetic-not-read", FCM: sp.FCMConfig{Enabled: true, ProjectID: "birdtie-test", CredentialsFile: "synthetic-not-read", ApplicationID: "app.birdtie.test"}, APNS: sp.APNSConfig{Enabled: true, KeyFile: "synthetic-not-read", KeyID: "KEY0000001", TeamID: "TEAM000001", Topic: "app.birdtie.test", Environment: "sandbox"}}
}
func TestSystemPushAADHasOwnerSessionBindingVersionAndNamespace(t *testing.T) {
	owner, session, binding, app, env := "owner", "session", "binding", "app", "FCM"
	generation := int64(1)
	kind := sp.FCMToken
	row := pushInstallationRow{version: 7, owner: &owner, session: &session, generation: &generation, binding: &binding, kind: &kind, app: &app, environment: &env}
	initial := string(pushAAD("installation", row))
	for _, change := range []func(*pushInstallationRow){func(r *pushInstallationRow) { r.version++ }, func(r *pushInstallationRow) { other := "different-owner"; r.owner = &other }, func(r *pushInstallationRow) { other := "different-session"; r.session = &other }, func(r *pushInstallationRow) { other := int64(2); r.generation = &other }, func(r *pushInstallationRow) { other := "different-binding"; r.binding = &other }, func(r *pushInstallationRow) { other := "different-app"; r.app = &other }, func(r *pushInstallationRow) { other := "production"; r.environment = &other }} {
		next := row
		change(&next)
		if initial == string(pushAAD("installation", next)) {
			t.Fatal("AAD lost a binding dimension")
		}
	}
}
