package postgres

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"reflect"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mp "github.com/birdtie/birdtie/apps/api/internal/momentphoto"
	"github.com/jackc/pgx/v5"
)

var _ content.PublicMomentWithPhotoStore = (*Store)(nil)

func momentPhotoViewer(a mp.Access) any {
	if a.AccountID == "" {
		return nil
	}
	return a.AccountID
}
func momentPhotoPublicAccess(a content.PublicMomentAccess) mp.Access {
	return mp.Access{AccountID: a.Actor.ID, AccountType: a.Actor.AccountType, SessionDigest: a.SessionDigest}
}
func (s *Store) beginPublicMomentPhoto(ctx context.Context, a mp.Access, moment string) (context.Context, pgx.Tx, identity.HumanControlFrame, error) {
	var first identity.HumanControlFrame
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil || !identity.HumanControlUUID(moment) {
		return ctx, nil, first, mp.ErrInvalid
	}
	if a.AccountID == "" {
		if a.AccountType != "" || a.SessionDigest != ([32]byte{}) {
			return ctx, nil, first, identity.ErrUnauthorized
		}
		if _, _, present := identity.OriginalHumanControlFrame(ctx, a.SessionDigest, momentPhotoActor(a)); present {
			return ctx, nil, first, mp.ErrChanged
		}
	} else {
		var e error
		ctx, first, e = s.humanWriteBound(ctx, a.SessionDigest, momentPhotoActor(a))
		if e != nil {
			return ctx, nil, first, e
		}
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ctx, nil, first, mp.ErrUnavailable
	}
	fail := func(e error) (context.Context, pgx.Tx, identity.HumanControlFrame, error) {
		_ = tx.Rollback(context.Background())
		return ctx, nil, first, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(mp.ErrUnavailable)
	}
	if a.AccountID != "" {
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.AccountID).Scan(&id); e != nil {
			return fail(mp.ErrChanged)
		}
	}
	var id string
	if e = tx.QueryRow(ctx, `SELECT id FROM moments WHERE id=$1 FOR SHARE`, moment).Scan(&id); errors.Is(e, pgx.ErrNoRows) {
		if e = s.finishPublicMomentPhoto(ctx, tx, a, first, nil); e != nil {
			return fail(e)
		}
		return fail(mp.ErrNotFound)
	} else if e != nil {
		return fail(mp.ErrUnavailable)
	}
	rows, e := tx.Query(ctx, `SELECT moment_id FROM public_moment_photos WHERE moment_id=$1 FOR SHARE`, moment)
	if e != nil {
		return fail(mp.ErrUnavailable)
	}
	rows.Close()
	return ctx, tx, first, nil
}
func (s *Store) finishPublicMomentPhoto(ctx context.Context, tx pgx.Tx, a mp.Access, first identity.HumanControlFrame, gate func() error) error {
	if a.AccountID != "" {
		current, _, e := s.humanWriteSession(ctx, tx, a.SessionDigest, momentPhotoActor(a), true)
		if errors.Is(e, identity.ErrUnauthorized) {
			return mp.ErrChanged
		}
		if e != nil {
			return e
		}
		if current != first {
			return mp.ErrChanged
		}
	}
	if gate != nil {
		if e := gate(); e != nil {
			return e
		}
	}
	if a.AccountID != "" {
		if e := checkHumanMomentSession(ctx, tx, a.SessionDigest, a.AccountID, s.devPhoneEnabled); e != nil {
			return e
		}
	}
	if ctx.Err() != nil {
		return mp.ErrUnavailable
	}
	if e := tx.Commit(ctx); e != nil {
		return mp.ErrUnavailable
	}
	return nil
}
func publicMomentPhotoSeal(a content.PublicMomentAccess, r content.PublicMomentPhotoReceipt) (string, error) {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return "", mp.ErrUnavailable
	}
	raw, e := json.Marshal([]any{"public-moment-photo-receipt-v1", a.Actor.ID, a.Actor.AccountType, hex.EncodeToString(a.SessionDigest[:]), r.View, r.IncludeAuthor, r.BaseProof, r.PhotoProof, r.Frame.ID, r.Frame.Generation})
	if e != nil {
		return "", mp.ErrUnavailable
	}
	h := hmac.New(sha256.New, humanWriteSigning.key[:])
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}
func publicMomentPhotoReadSQL(author bool) string {
	columns := "id,title,body,place_id,place_name,city_id,revision,published_at,binding"
	authorColumn := "NULL::jsonb"
	if author {
		columns += ",author_profile"
		authorColumn = "base.author_profile"
	}
	return `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at),base(` + columns + `) AS MATERIALIZED(` + publicMomentReadSQL(author) + `)
 SELECT base.id,base.title,base.body,base.place_id,base.place_name,base.city_id,base.revision,base.published_at,base.binding,` + authorColumn + `,
 stamp.at,least(stamp.at+interval '60 seconds',source.deadline,(SELECT least(se.expires_at,se.idle_expires_at) FROM sessions se WHERE se.account_id=$2 AND se.token_sha256=$3)),
 birdtie_public_moment_photo_metadata(base.id,$2,stamp.at),birdtie_public_moment_photo_proof(base.id,$2)
 FROM base CROSS JOIN stamp LEFT JOIN LATERAL birdtie_public_moment_photo_source(base.id,stamp.at) source ON true`
}
func readPublicMomentPhotoTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, id string, author bool, dev bool) (content.PublicMomentPhotoReceipt, error) {
	var r content.PublicMomentPhotoReceipt
	v := &r.View
	r.IncludeAuthor = author
	var rawAuthor, rawPhoto []byte
	var binding string
	e := tx.QueryRow(ctx, publicMomentPhotoReadSQL(author), id, momentPhotoViewer(momentPhotoPublicAccess(a)), a.SessionDigest[:], dev).Scan(&v.ID, &v.Title, &v.Body, &v.PlaceID, &v.PlaceName, &v.CityID, &v.Revision, &v.PublishedAt, &binding, &rawAuthor, &v.ObservedAt, &v.ValidUntil, &rawPhoto, &r.PhotoProof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, content.ErrNotFound
	}
	if e != nil {
		return r, content.ErrUnavailable
	}
	sum := sha256.Sum256([]byte(binding))
	r.BaseProof = hex.EncodeToString(sum[:])
	v.SourceBinding = r.BaseProof
	v.SchemaVersion = content.PublicMomentPhotoSchemaVersion
	v.PublishedAt = v.PublishedAt.UTC()
	v.ObservedAt = v.ObservedAt.UTC()
	v.ValidUntil = v.ValidUntil.UTC()
	if author && len(rawAuthor) > 0 {
		if json.Unmarshal(rawAuthor, &v.AuthorPublicProfile) != nil {
			return r, content.ErrUnavailable
		}
		if v.AuthorPublicProfile != nil {
			id := v.AuthorPublicProfile.AccountID
			v.AuthorID = &id
		}
	}
	if len(rawPhoto) > 0 && json.Unmarshal(rawPhoto, &v.Photo) != nil {
		return r, content.ErrUnavailable
	}
	if v.Photo != nil {
		if v.Photo.ExpiresAt.After(v.ValidUntil) {
			v.Photo.ExpiresAt = v.ValidUntil
		}
		if !v.Photo.Valid(v.ObservedAt) {
			v.Photo = nil
		}
	}
	if content.ValidatePublicMomentWithPhoto(*v, id) != nil {
		return r, content.ErrUnavailable
	}
	return r, nil
}
func (s *Store) CapturePublicMomentWithPhoto(ctx context.Context, a content.PublicMomentAccess, id string, author bool) (content.PublicMomentPhotoReceipt, error) {
	access := momentPhotoPublicAccess(a)
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, access, id)
	if e != nil {
		return content.PublicMomentPhotoReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := readPublicMomentPhotoTx(ctx, tx, a, id, author, s.devPhoneEnabled)
	if e != nil {
		if finish := s.finishPublicMomentPhoto(ctx, tx, access, frame, nil); finish != nil {
			return r, finish
		}
		return r, e
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, access, frame, func() error {
		fresh, e := readPublicMomentPhotoTx(ctx, tx, a, id, author, s.devPhoneEnabled)
		if e != nil {
			return e
		}
		if fresh.BaseProof != r.BaseProof || fresh.PhotoProof != r.PhotoProof || !r.View.ValidUntil.After(fresh.View.ObservedAt) || fresh.View.ValidUntil.Before(r.View.ValidUntil) {
			return mp.ErrChanged
		}
		return nil
	}); e != nil {
		return r, e
	}
	r.Frame = frame
	r.Seal, e = publicMomentPhotoSeal(a, r)
	return r, e
}
func (s *Store) FinalizePublicMomentWithPhoto(ctx context.Context, a content.PublicMomentAccess, old content.PublicMomentPhotoReceipt) (content.PublicMomentWithPhoto, error) {
	seal, e := publicMomentPhotoSeal(a, old)
	if e != nil || !hmac.Equal([]byte(seal), []byte(old.Seal)) || content.ValidatePublicMomentWithPhoto(old.View, old.View.ID) != nil {
		return content.PublicMomentWithPhoto{}, mp.ErrChanged
	}
	access := momentPhotoPublicAccess(a)
	if access.AccountID != "" {
		if first, valid, present := identity.OriginalHumanControlFrame(ctx, access.SessionDigest, momentPhotoActor(access)); present && (!valid || first != old.Frame) {
			return content.PublicMomentWithPhoto{}, mp.ErrChanged
		}
		ctx = identity.WithHumanControlFrame(ctx, access.SessionDigest, momentPhotoActor(access), old.Frame)
	} else if old.Frame != (identity.HumanControlFrame{}) {
		return content.PublicMomentWithPhoto{}, mp.ErrChanged
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, access, old.View.ID)
	if e != nil {
		return content.PublicMomentWithPhoto{}, e
	}
	defer tx.Rollback(context.Background())
	var out content.PublicMomentWithPhoto
	e = s.finishPublicMomentPhoto(ctx, tx, access, frame, func() error {
		fresh, e := readPublicMomentPhotoTx(ctx, tx, a, old.View.ID, old.IncludeAuthor, s.devPhoneEnabled)
		if e != nil {
			return e
		}
		before, next := old.View, fresh.View
		before.Photo, next.Photo = nil, nil
		next.ObservedAt = before.ObservedAt
		next.ValidUntil = before.ValidUntil
		if fresh.BaseProof != old.BaseProof || !old.View.ValidUntil.After(fresh.View.ObservedAt) || fresh.View.ValidUntil.Before(old.View.ValidUntil) || !reflect.DeepEqual(before, next) {
			return mp.ErrChanged
		}
		out = old.View
		if out.Photo != nil {
			copy := *out.Photo
			out.Photo = &copy
			var current *mp.Metadata
			if fresh.View.Photo != nil {
				copy := *fresh.View.Photo
				current = &copy
			}
			if old.PhotoProof != fresh.PhotoProof || !mp.PreserveLease(out.Photo, current, fresh.View.ObservedAt) {
				out.Photo = nil
			}
		}
		return nil
	})
	if e != nil {
		return content.PublicMomentWithPhoto{}, e
	}
	return out, nil
}
