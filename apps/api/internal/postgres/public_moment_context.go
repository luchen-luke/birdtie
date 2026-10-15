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

var _ content.PublicMomentWithContextStore = (*Store)(nil)

func publicMomentContextReadSQL(author bool) string {
	authorColumn, authorBinding := "NULL::jsonb", ""
	if author {
		authorColumn = publicMomentAuthorProfileSQL("a")
		authorBinding = "," + publicMomentAuthorBindingSQL("a")
	}
	return `WITH cl AS MATERIALIZED(SELECT clock_timestamp() at)
SELECT m.id,m.title,m.body,coalesce(p.id::text,''),coalesce(p.name,''),c.id,c.name,m.location_precision,m.revision,m.published_at,
 jsonb_build_array(m.xmin::text,a.xmin::text,p.xmin::text,c.xmin::text,` + retainedPlaceReadBindingSQL("p") + authorBinding + `,
 (SELECT viewer.xmin::text FROM accounts viewer WHERE viewer.id=$2),
 (SELECT coalesce(jsonb_agg(jsonb_build_array(ep.owner_account_id,ep.target_account_id,ep.version,ep.xmin::text) ORDER BY ep.owner_account_id,ep.target_account_id),'[]'::jsonb)
 FROM person_block_control_epochs ep WHERE $2::uuid IS NOT NULL AND((ep.owner_account_id=$2 AND ep.target_account_id=a.id) OR(ep.owner_account_id=a.id AND ep.target_account_id=$2))))::text,
 ` + authorColumn + `,cl.at,
 least(cl.at+interval '60 seconds',c.expires_at,CASE WHEN p.id IS NOT NULL THEN ` + currentRetainedPlaceDeadlineSQL("p", "cl.at") + ` END,
 (SELECT least(se.expires_at,se.idle_expires_at) FROM sessions se WHERE se.account_id=$2 AND se.token_sha256=$3)),
 CASE WHEN $5::boolean AND m.location_precision='place' THEN birdtie_public_moment_photo_metadata(m.id,$2,cl.at) ELSE NULL END,
 CASE WHEN $5::boolean AND m.location_precision='place' THEN birdtie_public_moment_photo_proof(m.id,$2) ELSE '' END
FROM moments m JOIN accounts a ON a.id=m.author_account_id JOIN cities c ON c.id=m.city_id
LEFT JOIN places p ON p.id=m.place_id AND p.city_id=m.city_id CROSS JOIN cl
WHERE m.id=$1 AND m.status='published' AND m.visibility='public'
AND m.author_confirmed_at IS NOT NULL AND m.published_at IS NOT NULL AND m.author_confirmed_at<=m.published_at AND m.published_at<=cl.at
AND a.account_type='person' AND a.status='active' AND c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>cl.at)
AND((m.place_id IS NULL AND m.location_precision='city') OR(m.location_precision='place' AND p.id IS NOT NULL AND p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>cl.at) AND ` + currentRetainedPlaceVisibilitySQL("p", "cl.at") + `))
AND NOT EXISTS(SELECT 1 FROM account_blocks b WHERE $2::uuid IS NOT NULL AND((b.blocker_account_id=$2 AND b.blocked_account_id=a.id) OR(b.blocker_account_id=a.id AND b.blocked_account_id=$2)))
AND($2::uuid IS NULL OR EXISTS(SELECT 1 FROM accounts viewer JOIN sessions ss ON ss.account_id=viewer.id
WHERE viewer.id=$2 AND viewer.account_type='person' AND viewer.status='active' AND ss.token_sha256=$3
AND ss.revoked_at IS NULL AND ss.expires_at>cl.at AND ss.idle_expires_at>cl.at AND($4::boolean OR ss.authentication_method<>'dev_phone')))`
}

func readPublicMomentContextTx(ctx context.Context, tx pgx.Tx, a content.PublicMomentAccess, id string, author, photo, dev bool) (content.PublicMomentContextReceipt, error) {
	r := content.PublicMomentContextReceipt{IncludeAuthor: author, IncludePhoto: photo}
	v := &r.View
	var rawAuthor, rawPhoto []byte
	var binding string
	e := tx.QueryRow(ctx, publicMomentContextReadSQL(author), id, momentPhotoViewer(momentPhotoPublicAccess(a)), a.SessionDigest[:], dev, photo).Scan(&v.ID, &v.Title, &v.Body, &v.PlaceID, &v.PlaceName, &v.CityID, &v.CityName, &v.LocationPrecision, &v.Revision, &v.PublishedAt, &binding, &rawAuthor, &v.ObservedAt, &v.ValidUntil, &rawPhoto, &r.PhotoProof)
	if errors.Is(e, pgx.ErrNoRows) {
		return r, content.ErrNotFound
	}
	if e != nil {
		return r, content.ErrUnavailable
	}
	sum := sha256.Sum256([]byte(binding))
	r.BaseProof = hex.EncodeToString(sum[:])
	v.SourceBinding = r.BaseProof
	v.SchemaVersion = content.PublicMomentContextSchemaVersion
	v.PublishedAt, v.ObservedAt, v.ValidUntil = v.PublishedAt.UTC(), v.ObservedAt.UTC(), v.ValidUntil.UTC()
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
	if content.ValidatePublicMomentWithContext(*v, id) != nil {
		return r, content.ErrUnavailable
	}
	return r, nil
}

func publicMomentContextSeal(a content.PublicMomentAccess, r content.PublicMomentContextReceipt) (string, error) {
	humanWriteSigning.Do(func() { _, humanWriteSigning.err = rand.Read(humanWriteSigning.key[:]) })
	if humanWriteSigning.err != nil {
		return "", mp.ErrUnavailable
	}
	raw, e := json.Marshal([]any{"public-moment-context-receipt-v1", a.Actor.ID, a.Actor.AccountType, hex.EncodeToString(a.SessionDigest[:]), r.View, r.IncludeAuthor, r.IncludePhoto, r.BaseProof, r.PhotoProof, r.Frame.ID, r.Frame.Generation})
	if e != nil {
		return "", mp.ErrUnavailable
	}
	h := hmac.New(sha256.New, humanWriteSigning.key[:])
	_, _ = h.Write(raw)
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (s *Store) CapturePublicMomentWithContext(ctx context.Context, a content.PublicMomentAccess, id string, author, photo bool) (content.PublicMomentContextReceipt, error) {
	access := momentPhotoPublicAccess(a)
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, access, id)
	if e != nil {
		return content.PublicMomentContextReceipt{}, e
	}
	defer tx.Rollback(context.Background())
	r, e := readPublicMomentContextTx(ctx, tx, a, id, author, photo, s.devPhoneEnabled)
	if e != nil {
		if finish := s.finishPublicMomentPhoto(ctx, tx, access, frame, nil); finish != nil {
			return r, finish
		}
		return r, e
	}
	if e = s.finishPublicMomentPhoto(ctx, tx, access, frame, func() error {
		fresh, e := readPublicMomentContextTx(ctx, tx, a, id, author, photo, s.devPhoneEnabled)
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
	r.Seal, e = publicMomentContextSeal(a, r)
	return r, e
}

func (s *Store) FinalizePublicMomentWithContext(ctx context.Context, a content.PublicMomentAccess, old content.PublicMomentContextReceipt) (content.PublicMomentWithContext, error) {
	seal, e := publicMomentContextSeal(a, old)
	if e != nil || !hmac.Equal([]byte(seal), []byte(old.Seal)) || content.ValidatePublicMomentWithContext(old.View, old.View.ID) != nil {
		return content.PublicMomentWithContext{}, mp.ErrChanged
	}
	access := momentPhotoPublicAccess(a)
	if access.AccountID != "" {
		if first, valid, present := identity.OriginalHumanControlFrame(ctx, access.SessionDigest, momentPhotoActor(access)); present && (!valid || first != old.Frame) {
			return content.PublicMomentWithContext{}, mp.ErrChanged
		}
		ctx = identity.WithHumanControlFrame(ctx, access.SessionDigest, momentPhotoActor(access), old.Frame)
	} else if old.Frame != (identity.HumanControlFrame{}) {
		return content.PublicMomentWithContext{}, mp.ErrChanged
	}
	ctx, tx, frame, e := s.beginPublicMomentPhoto(ctx, access, old.View.ID)
	if e != nil {
		return content.PublicMomentWithContext{}, e
	}
	defer tx.Rollback(context.Background())
	var out content.PublicMomentWithContext
	e = s.finishPublicMomentPhoto(ctx, tx, access, frame, func() error {
		fresh, e := readPublicMomentContextTx(ctx, tx, a, old.View.ID, old.IncludeAuthor, old.IncludePhoto, s.devPhoneEnabled)
		if e != nil {
			return e
		}
		before, next := old.View, fresh.View
		before.Photo, next.Photo = nil, nil
		next.ObservedAt, next.ValidUntil = before.ObservedAt, before.ValidUntil
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
		return content.PublicMomentWithContext{}, e
	}
	return out, nil
}
