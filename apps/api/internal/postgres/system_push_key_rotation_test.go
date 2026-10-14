package postgres

import (
	"bytes"
	"testing"

	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
)

func TestSystemPushNativePersistentCipherAtomicKeyRotation(t *testing.T) {
	ownedMigrationDatabase(t)
	f := newSystemPushFixture(t)
	b := f.f.f.private.base
	op := f.register(t, 0, sp.FCMToken)
	row, e := scanPushInstallation(b.pool.QueryRow(b.ctx, `SELECT `+pushInstallationColumns+` FROM system_push_installations WHERE installation_id=$1`, f.a.InstallationID))
	if e != nil {
		t.Fatal(e)
	}
	restarted, e := sp.ParseKeyring(pushTestKeys(t, "one", map[string]byte{"one": 1}))
	if e != nil {
		t.Fatal(e)
	}
	address, e := restarted.Open(sp.Ciphertext{KeyID: *row.keyID, Nonce: row.nonce, Ciphertext: row.cipher}, pushAAD(f.a.InstallationID, row))
	if e != nil || !address.Valid(sp.FCMToken) {
		t.Fatal("persistent restart did not decrypt", e)
	}
	if e = f.ring.Reload(pushTestKeys(t, "two", map[string]byte{"one": 1, "two": 2})); e != nil {
		t.Fatal(e)
	}
	n, e := f.p.RotateAddressKeys(b.ctx, 1)
	if e != nil || n != 1 {
		t.Fatal("bounded native rotation", n, e)
	}
	after, e := scanPushInstallation(b.pool.QueryRow(b.ctx, `SELECT `+pushInstallationColumns+` FROM system_push_installations WHERE installation_id=$1`, f.a.InstallationID))
	if e != nil || *after.keyID != "two" || after.version != row.version || *after.binding != *op.BindingID || !bytes.Equal(row.fingerprint, after.fingerprint) || bytes.Equal(row.nonce, after.nonce) {
		t.Fatal("rotation mutated business binding/fingerprint or reused nonce", e)
	}
	onlyNew, e := sp.ParseKeyring(pushTestKeys(t, "two", map[string]byte{"two": 2}))
	if e != nil {
		t.Fatal(e)
	}
	plain, e := onlyNew.Open(sp.Ciphertext{KeyID: *after.keyID, Nonce: after.nonce, Ciphertext: after.cipher}, pushAAD(f.a.InstallationID, after))
	if e != nil || !bytes.Equal(plain.Bytes(), address.Bytes()) {
		t.Fatal("completed rotation needs retired old key", e)
	}
	counts, e := f.p.AddressKeyCounts(b.ctx)
	if e != nil || counts["one"] != 0 || counts["two"] != 1 {
		t.Fatal("key retirement counts incorrect", counts, e)
	}
	n, e = f.p.RotateAddressKeys(b.ctx, 100)
	if e != nil || n != 0 {
		t.Fatal("second rotation not idempotent", n, e)
	}
}
