package postgres

import (
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/birdtie/birdtie/apps/api/internal/sessionrefresh"
	"github.com/jackc/pgx/v5"
)

// attachPersistentSession is part of the original explicit phone verification
// transaction. No expired/legacy access token can enroll a device credential.
func (s *Store) attachPersistentSession(ctx context.Context, tx pgx.Tx, accountID, sessionID string, phone [32]byte, response *sessionrefresh.Response) error {
	digest, err := sessionrefresh.ParseToken(response.RefreshToken)
	if err != nil {
		return err
	}
	var family string
	err = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 INSERT INTO persistent_session_families(account_id,identity_issuer,identity_subject,authentication_method,created_at,expires_at)
 SELECT $1,$2,$3,'dev_phone',stamp.at,stamp.at+interval '30 days' FROM stamp
 RETURNING id::text,expires_at`, accountID, devPhoneIssuer, hex.EncodeToString(phone[:])).Scan(&family, &response.RefreshExpiresAt)
	if err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persistent_session_tokens(token_sha256,family_id)VALUES($1,$2)`, digest[:], family); err != nil {
		return err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persistent_session_access(session_id,family_id)VALUES($1,$2)`, sessionID, family); err != nil {
		return err
	}
	response.AccountID = accountID
	return nil
}

func (s *Store) VerifyDevPhoneChallengePersistent(ctx context.Context, phone [32]byte, codeValid bool, accessToken, refreshToken string) (sessionrefresh.Response, error) {
	var empty sessionrefresh.Response
	digest, err := identity.ParseBearer("Bearer " + accessToken)
	if err != nil {
		return empty, err
	}
	if _, err = sessionrefresh.ParseToken(refreshToken); err != nil {
		return empty, err
	}
	response := sessionrefresh.Response{AccessToken: accessToken, TokenType: "Bearer", ExpiresInSeconds: 28800, IdleTimeoutSeconds: 1800, RefreshToken: refreshToken, AuthMethod: "dev_phone"}
	if err = s.verifyDevPhoneChallenge(ctx, phone, codeValid, digest, &response); err != nil {
		return empty, err
	}
	return response, nil
}

type persistentFamily struct {
	id, account, issuer, subject, method string
	expires                              time.Time
	revoked                              *time.Time
}

// Account before Family before Token matches domain account/session locking.
// Identity facts are rechecked after locks against the actual native clock.
func (s *Store) lockPersistentFamily(ctx context.Context, tx pgx.Tx, digest [32]byte, requireCurrent bool) (persistentFamily, error) {
	var f persistentFamily
	err := tx.QueryRow(ctx, `SELECT f.id::text,f.account_id::text,f.identity_issuer,f.identity_subject FROM persistent_session_tokens t JOIN persistent_session_families f ON f.id=t.family_id WHERE t.token_sha256=$1`, digest[:]).Scan(&f.id, &f.account, &f.issuer, &f.subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, identity.ErrUnauthorized
	}
	if err != nil {
		return f, err
	}
	// Explicit fixed-code login takes this same identity lock before touching
	// Account/identity rows. Serialize here before Account so a concurrent login
	// cannot hold identity FOR UPDATE while our refresh holds Account SHARE.
	if _, err = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, f.issuer+":"+f.subject); err != nil {
		return f, err
	}
	var status, kind string
	if err = tx.QueryRow(ctx, `SELECT status,account_type FROM accounts WHERE id=$1 FOR SHARE`, f.account).Scan(&status, &kind); err != nil {
		return f, err
	}
	err = tx.QueryRow(ctx, `SELECT identity_issuer,identity_subject,authentication_method,expires_at,revoked_at FROM persistent_session_families WHERE id=$1 AND account_id=$2 FOR UPDATE`, f.id, f.account).Scan(&f.issuer, &f.subject, &f.method, &f.expires, &f.revoked)
	if err != nil {
		return f, err
	}
	if requireCurrent {
		var boundAccount string
		err = tx.QueryRow(ctx, `SELECT account_id::text FROM account_auth_identities WHERE issuer=$1 AND subject=$2 AND account_id=$3 FOR SHARE`, f.issuer, f.subject, f.account).Scan(&boundAccount)
		if errors.Is(err, pgx.ErrNoRows) {
			return f, identity.ErrUnauthorized
		}
		if err != nil {
			return f, err
		}
		var live bool
		err = tx.QueryRow(ctx, `SELECT $1::timestamptz>clock_timestamp()`, f.expires).Scan(&live)
		if err != nil {
			return f, err
		}
		if status != "active" || kind != "person" || f.revoked != nil || !live || f.issuer != devPhoneIssuer || f.method != "dev_phone" || !s.devPhoneEnabled {
			return f, identity.ErrUnauthorized
		}
	}
	return f, nil
}

func persistentFamilyBeforeCommit(ctx context.Context, tx pgx.Tx, f persistentFamily) error {
	var current bool
	err := tx.QueryRow(ctx, `SELECT expires_at>clock_timestamp() AND revoked_at IS NULL FROM persistent_session_families WHERE id=$1`, f.id).Scan(&current)
	if err != nil {
		return err
	}
	if !current {
		return identity.ErrUnauthorized
	}
	return nil
}

func revokePersistentFamily(ctx context.Context, tx pgx.Tx, family string) error {
	if _, err := tx.Exec(ctx, `UPDATE persistent_session_families SET revoked_at=clock_timestamp() WHERE id=$1 AND revoked_at IS NULL`, family); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id IN(SELECT session_id FROM persistent_session_access WHERE family_id=$1)AND revoked_at IS NULL`, family)
	return err
}

func (s *Store) RefreshSession(ctx context.Context, oldToken, operationID string) (sessionrefresh.Response, error) {
	var empty sessionrefresh.Response
	digest, err := sessionrefresh.ParseToken(oldToken)
	if err != nil {
		return empty, identity.ErrUnauthorized
	}
	operationID, err = sessionrefresh.NormalizeOperationID(operationID)
	if err != nil {
		return empty, err
	}
	if !s.devPhoneEnabled {
		return empty, identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return empty, err
	}
	defer tx.Rollback(context.Background())
	family, err := s.lockPersistentFamily(ctx, tx, digest, true)
	if err != nil {
		return empty, err
	}
	var used *time.Time
	var previousOperation *string
	var ciphertext, successor []byte
	var receiptDeadline *time.Time
	err = tx.QueryRow(ctx, `SELECT consumed_at,operation_id::text,receipt_ciphertext,successor_sha256,receipt_expires_at FROM persistent_session_tokens WHERE token_sha256=$1 AND family_id=$2 FOR UPDATE`, digest[:], family.id).Scan(&used, &previousOperation, &ciphertext, &successor, &receiptDeadline)
	if err != nil {
		return empty, err
	}
	if used != nil {
		var withinWindow bool
		err = tx.QueryRow(ctx, `SELECT $1::timestamptz>clock_timestamp() AND EXISTS(SELECT 1 FROM persistent_session_tokens WHERE family_id=$2 AND token_sha256=$3 AND consumed_at IS NULL)`, receiptDeadline, family.id, successor).Scan(&withinWindow)
		if err != nil {
			return empty, err
		}
		if previousOperation == nil || *previousOperation != operationID || !withinWindow {
			if err = revokePersistentFamily(ctx, tx, family.id); err != nil {
				return empty, err
			}
			if err = tx.Commit(ctx); err != nil {
				return empty, err
			}
			return empty, identity.ErrUnauthorized
		}
		response, err := sessionrefresh.OpenReceipt(oldToken, operationID, family.id, ciphertext)
		if err != nil {
			return empty, err
		}
		nextDigest, err := sessionrefresh.ParseToken(response.RefreshToken)
		if err != nil || subtle.ConstantTimeCompare(nextDigest[:], successor) != 1 || response.AccountID != family.account || !response.RefreshExpiresAt.Equal(family.expires) {
			return empty, identity.ErrUnauthorized
		}
		accessDigest := sha256.Sum256([]byte(response.AccessToken))
		var accessLive bool
		err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM sessions s JOIN persistent_session_access a ON a.session_id=s.id WHERE a.family_id=$1 AND s.token_sha256=$2 AND s.account_id=$3 AND s.revoked_at IS NULL AND s.expires_at>clock_timestamp() AND s.idle_expires_at>clock_timestamp())`, family.id, accessDigest[:], family.account).Scan(&accessLive)
		if err != nil {
			return empty, err
		}
		if !accessLive {
			return empty, identity.ErrUnauthorized
		}
		if err = persistentFamilyBeforeCommit(ctx, tx, family); err != nil {
			return empty, err
		}
		if err = tx.Commit(ctx); err != nil {
			return empty, err
		}
		return response, nil
	}
	access, accessDigest, err := identity.NewToken()
	if err != nil {
		return empty, err
	}
	next, nextDigest, err := sessionrefresh.NewToken()
	if err != nil {
		return empty, err
	}
	response := sessionrefresh.Response{AccessToken: access, TokenType: "Bearer", RefreshToken: next, RefreshExpiresAt: family.expires, AccountID: family.account, AuthMethod: "dev_phone"}
	// Retire old access and its approvals before taking the issuance clock: a
	// session-row wait must not let this device pass its fixed family deadline.
	if _, err = tx.Exec(ctx, `UPDATE sessions SET revoked_at=clock_timestamp() WHERE id IN(SELECT session_id FROM persistent_session_access WHERE family_id=$1)AND revoked_at IS NULL`, family.id); err != nil {
		return empty, err
	}
	var sessionID string
	err = tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at)
 INSERT INTO sessions(account_id,token_sha256,authentication_method,created_at,expires_at,idle_expires_at)
 SELECT $1,$2,'dev_phone',stamp.at,LEAST(stamp.at+interval '8 hours',$3::timestamptz),LEAST(stamp.at+interval '30 minutes',$3::timestamptz)
 FROM stamp WHERE $3::timestamptz>stamp.at+interval '1 second'
 RETURNING id::text,floor(extract(epoch FROM(expires_at-created_at)))::integer,floor(extract(epoch FROM(idle_expires_at-created_at)))::integer`, family.account, accessDigest[:], family.expires).Scan(&sessionID, &response.ExpiresInSeconds, &response.IdleTimeoutSeconds)
	if errors.Is(err, pgx.ErrNoRows) {
		return empty, identity.ErrUnauthorized
	}
	if err != nil {
		return empty, err
	}
	encrypted, err := sessionrefresh.SealReceipt(oldToken, operationID, family.id, response)
	if err != nil {
		return empty, err
	}
	// Consume first, releasing the one-current-token index. The successor FK is
	// deferred until this single transaction inserts the replacement token.
	_, err = tx.Exec(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE persistent_session_tokens SET consumed_at=stamp.at,operation_id=$2,successor_sha256=$3,receipt_ciphertext=$4,receipt_expires_at=stamp.at+interval '60 seconds' FROM stamp WHERE token_sha256=$1 AND consumed_at IS NULL`, digest[:], operationID, nextDigest[:], encrypted)
	if err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persistent_session_tokens(token_sha256,family_id)VALUES($1,$2)`, nextDigest[:], family.id); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO persistent_session_access(session_id,family_id)VALUES($1,$2)`, sessionID, family.id); err != nil {
		return empty, err
	}
	if _, err = tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose)VALUES($1,'session_issue','session',$2,'allowed','device_session_refresh')`, family.account, sessionID); err != nil {
		return empty, err
	}
	if err = persistentFamilyBeforeCommit(ctx, tx, family); err != nil {
		return empty, err
	}
	if err = tx.Commit(ctx); err != nil {
		return empty, err
	}
	return response, nil
}

// Possession of a known device token can only revoke its own family. This
// remains possible after access expiry and after a pending rotation consumed it.
func (s *Store) RevokeRefreshFamily(ctx context.Context, token string) error {
	digest, err := sessionrefresh.ParseToken(token)
	if err != nil {
		return identity.ErrUnauthorized
	}
	tx, err := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	f, err := s.lockPersistentFamily(ctx, tx, digest, false)
	if err != nil {
		return err
	}
	if err = revokePersistentFamily(ctx, tx, f.id); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
