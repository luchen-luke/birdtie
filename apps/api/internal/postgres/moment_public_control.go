package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Publication approves the concrete draft. Public editing may explicitly
// replace or remove geography; time and unrelated contexts stay unchanged.
func momentPublicInputMatches(old content.Moment, in content.MomentControlInput) bool {
	v := in.Input
	if v == nil || old.TimePrecision != v.TimePrecision || v.ActivityID != nil || v.CommunityID != nil || v.OrganizationID != nil {
		return false
	}
	if (old.OccurredAt == nil) != (v.OccurredAt == nil) || old.OccurredAt != nil && !old.OccurredAt.Equal(*v.OccurredAt) {
		return false
	}
	return in.Action != "PUBLISH" || old.Title == v.Title && old.Body == v.Body
}

func applyMomentPublicControl(ctx context.Context, f humanWriteFrame, in content.MomentControlInput, snapshots ...momentLocationsSnapshot) (content.Moment, error) {
	var captured momentLocationsSnapshot
	if len(snapshots) > 0 {
		captured = snapshots[0]
	} else {
		old, e := getOwnMomentInTx(ctx, f.tx, f.a.ID, in.MomentID)
		if e != nil {
			return content.Moment{}, e
		}
		captured, e = captureMomentLocationsInTx(ctx, f, &old, *in.Input)
		if e != nil {
			return content.Moment{}, e
		}
	}
	city, place, precision := momentLocationLegacyFields(captured.Effective)
	var id string
	var err error
	switch in.Action {
	case "PUBLISH":
		err = f.tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE moments m SET city_id=NULLIF($4,''),place_id=NULLIF($5,'')::uuid,location_precision=$6,visibility='public',status='published',author_confirmed_at=stamp.at,published_at=stamp.at,updated_at=stamp.at,revision=revision+1 FROM stamp WHERE m.id=$1 AND m.author_account_id=$2 AND m.revision=$3 AND m.status='draft' AND m.visibility='private' RETURNING m.id`, in.MomentID, f.a.ID, in.ExpectedRevision, city, place, precision).Scan(&id)
	case "UPDATE_PUBLIC":
		err = f.tx.QueryRow(ctx, `UPDATE moments SET title=$4,body=$5,city_id=NULLIF($6,''),place_id=NULLIF($7,'')::uuid,location_precision=$8,updated_at=clock_timestamp(),revision=revision+1 WHERE id=$1 AND author_account_id=$2 AND revision=$3 AND status='published' AND visibility='public' RETURNING id`, in.MomentID, f.a.ID, in.ExpectedRevision, in.Input.Title, in.Input.Body, city, place, precision).Scan(&id)
	default:
		return content.Moment{}, identity.ErrControlInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, identity.ErrControlChanged
	}
	if err != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	if err = applyMomentLocationsInTx(ctx, f, id, in.ExpectedRevision+1, captured); err != nil {
		return content.Moment{}, err
	}
	if _, err = f.tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'moment',$3,'allowed','explicit_public_content')`, f.a.ID, in.Action, in.MomentID); err != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	// No model, Memory, private-context or automatic notification grant.
	return getOwnMomentInTx(ctx, f.tx, f.a.ID, id)
}
