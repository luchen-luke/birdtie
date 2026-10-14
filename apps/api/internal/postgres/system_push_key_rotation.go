package postgres

import (
	"context"

	sp "github.com/birdtie/birdtie/apps/api/internal/systempush"
	"github.com/jackc/pgx/v5"
)

// RotateAddressKeys is a bounded trusted maintenance operation, never an HTTP
// capability. It changes ciphertext only; binding and business version survive.
func (p *SystemPush) RotateAddressKeys(ctx context.Context, limit int) (int, error) {
	if !p.Enabled() {
		return 0, sp.ErrDisabled
	}
	if limit < 1 || limit > 100 {
		return 0, sp.ErrInvalid
	}
	active := p.ring.ActiveKeyID()
	if active == "" {
		return 0, sp.ErrUnavailable
	}
	tx, e := p.store.pool.BeginTx(ctx, pgx.TxOptions{})
	if e != nil {
		return 0, sp.ErrUnavailable
	}
	defer tx.Rollback(context.Background())
	rows, e := tx.Query(ctx, `SELECT installation_id::text,`+pushInstallationColumns+` FROM system_push_installations WHERE state='ACTIVE' AND address_key_id<>$1 ORDER BY installation_id LIMIT $2 FOR UPDATE SKIP LOCKED`, active, limit)
	if e != nil {
		return 0, sp.ErrUnavailable
	}
	type entry struct {
		iid string
		row pushInstallationRow
	}
	all := []entry{}
	for rows.Next() {
		var r entry
		v := &r.row
		e = rows.Scan(&r.iid, &v.version, &v.proof, &v.owner, &v.session, &v.generation, &v.binding, &v.platform, &v.kind, &v.app, &v.environment, &v.permission, &v.state, &v.keyID, &v.nonce, &v.cipher, &v.fingerprint, &v.registered, &v.until)
		if e != nil {
			rows.Close()
			return 0, sp.ErrUnavailable
		}
		all = append(all, r)
	}
	rows.Close()
	if rows.Err() != nil {
		return 0, sp.ErrUnavailable
	}
	for _, r := range all {
		if r.row.keyID == nil {
			return 0, sp.ErrUnavailable
		}
		aad := pushAAD(r.iid, r.row)
		address, e := p.ring.Open(sp.Ciphertext{KeyID: *r.row.keyID, Nonce: r.row.nonce, Ciphertext: r.row.cipher}, aad)
		if e != nil {
			return 0, sp.ErrUnavailable
		}
		encrypted, e := p.ring.Seal(address, aad)
		if e != nil {
			return 0, sp.ErrUnavailable
		}
		tag, e := tx.Exec(ctx, `UPDATE system_push_installations SET address_key_id=$2,address_nonce=$3,address_ciphertext=$4 WHERE installation_id=$1 AND address_key_id=$5 AND address_nonce=$6 AND address_ciphertext=$7`, r.iid, encrypted.KeyID, encrypted.Nonce, encrypted.Ciphertext, *r.row.keyID, r.row.nonce, r.row.cipher)
		if e != nil || tag.RowsAffected() != 1 {
			return 0, sp.ErrConflict
		}
	}
	if e = tx.Commit(ctx); e != nil {
		return 0, sp.ErrUnavailable
	}
	return len(all), nil
}
func (p *SystemPush) AddressKeyCounts(ctx context.Context) (map[string]int64, error) {
	if p == nil {
		return nil, sp.ErrUnavailable
	}
	rows, e := p.store.pool.Query(ctx, `SELECT address_key_id,count(*) FROM system_push_installations WHERE state='ACTIVE' GROUP BY address_key_id`)
	if e != nil {
		return nil, sp.ErrUnavailable
	}
	defer rows.Close()
	out := map[string]int64{}
	for rows.Next() {
		var id string
		var n int64
		if e = rows.Scan(&id, &n); e != nil {
			return nil, sp.ErrUnavailable
		}
		out[id] = n
	}
	if rows.Err() != nil {
		return nil, sp.ErrUnavailable
	}
	return out, nil
}
