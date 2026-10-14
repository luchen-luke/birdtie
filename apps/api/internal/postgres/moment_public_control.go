package postgres

import (
	"context"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

// Public actions preserve the original context. A publication cannot edit an
// unreviewed draft; public editing changes only the displayed title and body.
func momentPublicInputMatches(old content.Moment, in content.MomentControlInput) bool {
	v := in.Input
	if v == nil || old.CityID != v.CityID || old.PlaceID != v.PlaceID || old.TimePrecision != v.TimePrecision || old.LocationPrecision != v.LocationPrecision || v.ActivityID != nil || v.CommunityID != nil || v.OrganizationID != nil {
		return false
	}
	if (old.OccurredAt == nil) != (v.OccurredAt == nil) || old.OccurredAt != nil && !old.OccurredAt.Equal(*v.OccurredAt) {
		return false
	}
	return in.Action != "PUBLISH" || old.Title == v.Title && old.Body == v.Body
}

func applyMomentPublicControl(ctx context.Context, f humanWriteFrame, in content.MomentControlInput) (content.Moment, error) {
	var id string
	var err error
	switch in.Action {
	case "PUBLISH":
		err = f.tx.QueryRow(ctx, `WITH stamp AS MATERIALIZED(SELECT clock_timestamp() at) UPDATE moments m SET visibility='public',status='published',author_confirmed_at=stamp.at,published_at=stamp.at,updated_at=stamp.at,revision=revision+1 FROM stamp WHERE m.id=$1 AND m.author_account_id=$2 AND m.revision=$3 AND m.status='draft' AND m.visibility='private' RETURNING m.id`, in.MomentID, f.a.ID, in.ExpectedRevision).Scan(&id)
	case "UPDATE_PUBLIC":
		// 145 retires the previous public image in this same transaction. The
		// concrete editor warns about this before approval; it never silently
		// carries a photo approved for an older Moment revision into the new one.
		err = f.tx.QueryRow(ctx, `UPDATE moments SET title=$4,body=$5,updated_at=clock_timestamp(),revision=revision+1 WHERE id=$1 AND author_account_id=$2 AND revision=$3 AND status='published' AND visibility='public' RETURNING id`, in.MomentID, f.a.ID, in.ExpectedRevision, in.Input.Title, in.Input.Body).Scan(&id)
	default:
		return content.Moment{}, identity.ErrControlInvalid
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return content.Moment{}, identity.ErrControlChanged
	}
	if err != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	if _, err = f.tx.Exec(ctx, `INSERT INTO audit_events(actor_account_id,action,resource_type,resource_id,decision,purpose) VALUES($1,$2,'moment',$3,'allowed','explicit_public_content')`, f.a.ID, in.Action, in.MomentID); err != nil {
		return content.Moment{}, identity.ErrControlUnavailable
	}
	// No model, Memory, private-context or automatic notification grant.
	return getOwnMomentInTx(ctx, f.tx, f.a.ID, id)
}
