package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/content"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
)

var _ content.HumanCollectionControlStore = (*Store)(nil)

type collectionControlFrame struct {
	frame   humanWriteFrame
	account string
	until   time.Time
}

func collectionControlScope(in content.CollectionControlInput) string {
	// Digest intentionally describes the payload separately. The signature
	// also binds the original operation namespace so STOP cannot be bypassed
	// by attaching the same approval to a newly allocated operation ID.
	raw, _ := json.Marshal([]string{content.CollectionControlSchemaVersion, in.OperationID, in.Kind, in.CollectionID})
	return string(raw)
}

// Capture both the original Session and principal epoch before any account or
// operation lock wait. The two typed contexts must refer to that same Session.
func (s *Store) collectionControlBegin(ctx context.Context, d [32]byte, a identity.Actor, op string) (context.Context, collectionControlFrame, error) {
	v := collectionControlFrame{frame: humanWriteFrame{d: d, a: a, op: op, family: "COLLECTION_EDIT"}}
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return ctx, v, identity.ErrControlUnavailable
	}
	if a.AccountType != "person" || !identity.HumanControlUUID(a.ID) || d == ([32]byte{}) || !content.ValidCollectionID(op) {
		return ctx, v, identity.ErrUnauthorized
	}
	first, valid, present := identity.OriginalHumanControlFrame(ctx, d, a)
	if present && (!valid || !identity.HumanControlUUID(first.ID) || first.Generation < 1) {
		return ctx, v, identity.ErrControlChanged
	}
	var e error
	v.frame.tx, e = s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return ctx, v, identity.ErrControlUnavailable
	}
	fail := func(e error) (context.Context, collectionControlFrame, error) {
		_ = v.frame.tx.Rollback(context.Background())
		return ctx, v, e
	}
	if !present {
		first, _, e = s.humanWriteSession(ctx, v.frame.tx, d, a, false)
		if e != nil {
			return fail(e)
		}
	}
	v.frame.session = first
	ctx = identity.WithHumanControlFrame(ctx, d, a, first)
	ctx = content.WithCollectionSession(ctx, d, a, content.CollectionSessionFrame{ID: first.ID, Generation: first.Generation})
	if _, valid, present = content.OriginalCollectionSession(ctx, d, a); !valid || !present {
		return fail(identity.ErrControlChanged)
	}
	if e = v.frame.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, a.ID).Scan(&v.account); e != nil {
		return fail(identity.ErrUnauthorized)
	}
	// Read before waiting; returning to active does not restore the old epoch.
	var current string
	if e = v.frame.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active' FOR SHARE`, a.ID).Scan(&current); e != nil || current != v.account {
		return fail(identity.ErrControlChanged)
	}
	if _, e = v.frame.tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1,147))`, a.ID+":"+op); e != nil {
		return fail(identity.ErrControlUnavailable)
	}
	return ctx, v, nil
}

func (s *Store) collectionControlFinish(ctx context.Context, v *collectionControlFrame, gates ...func() error) error {
	now, until, e := s.humanWriteNow(ctx, v.frame, nil)
	if e != nil {
		return e
	}
	duration := until.Sub(now)
	if duration <= 0 {
		return identity.ErrControlChanged
	}
	v.until = time.Now().Add(duration)
	all := []func() error{func() error {
		var current string
		if e := v.frame.tx.QueryRow(ctx, `SELECT xmin::text FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`, v.frame.a.ID).Scan(&current); e != nil || current != v.account {
			return identity.ErrControlChanged
		}
		return nil
	}}
	all = append(all, gates...)
	return s.humanWriteFinish(ctx, v.frame, all...)
}

func collectionControlLegacy(in content.CollectionControlInput) content.CollectionInput {
	if in.Input == nil {
		return content.CollectionInput{}
	}
	p := in.Input
	out := content.CollectionInput{Kind: p.Kind, CityID: p.CityID, Title: p.Title, Summary: p.Summary, Entries: []content.CollectionEntry{}, JourneyDetails: p.JourneyDetails}
	for _, entry := range p.Entries {
		out.Entries = append(out.Entries, content.CollectionEntry{PlaceID: entry.PlaceID, Note: entry.Note, LocationVisibility: entry.LocationVisibility})
	}
	return out
}

// Only owned resource epochs are in this target token. WITHDRAW needs no new
// Place, copied-label, Agent or model permission to terminate a private draft.
func collectionControlTarget(ctx context.Context, tx pgx.Tx, owner string, in content.CollectionControlInput, lock bool) (string, error) {
	table, entries, key, e := collectionTables(in.Kind)
	if e != nil {
		return "", e
	}
	var before string
	details := ""
	if in.Kind == "journey" {
		details = `,(SELECT coalesce(jsonb_agg(jsonb_build_array(it.xmin::text,it.entry_id,it.material) ORDER BY it.sequence_no),'[]'::jsonb) FROM journey_itinerary_entries it WHERE it.journey_id=t.id)`
	}
	query := `SELECT jsonb_build_array(t.xmin::text,t.id,t.revision,t.status,t.visibility,
 (SELECT coalesce(jsonb_agg(jsonb_build_array(e.xmin::text,e.sequence_no,e.place_id,e.note,e.location_visibility) ORDER BY e.sequence_no),'[]'::jsonb) FROM ` + entries + ` e WHERE e.` + key + `=t.id)` + details + `)::text
 FROM ` + table + ` t WHERE t.id=$1 AND t.owner_account_id=$2 AND t.revision=$3 AND t.status='draft' AND t.visibility='private'`
	if e = tx.QueryRow(ctx, query, in.CollectionID, owner, in.ExpectedRevision).Scan(&before); errors.Is(e, pgx.ErrNoRows) {
		return "", content.ErrConflict
	} else if e != nil {
		return "", content.ErrUnavailable
	}
	if lock {
		var id string
		if e = tx.QueryRow(ctx, `SELECT id FROM `+table+` WHERE id=$1 AND owner_account_id=$2 AND revision=$3 AND status='draft' AND visibility='private' FOR UPDATE`, in.CollectionID, owner, in.ExpectedRevision).Scan(&id); e != nil {
			return "", content.ErrConflict
		}
		current, e := collectionControlTarget(ctx, tx, owner, in, false)
		if e != nil || current != before {
			return "", content.ErrConflict
		}
	}
	return before, nil
}

func collectionControlAuthority(ctx context.Context, tx pgx.Tx, owner string, in content.CollectionControlInput, lock bool) (string, *time.Time, error) {
	if in.Action == "WITHDRAW" {
		return "withdraw-owned-private", nil, nil
	}
	p := collectionControlLegacy(in)
	if handled, authority, deadline, e := journeyDetailsAuthorityTx(ctx, tx, owner, in.CollectionID, p); handled || e != nil {
		return authority, deadline, e
	}
	if handled, authority, deadline, e := legacyCollectionRetainedAuthorityTx(ctx, tx, owner, in.CollectionID, p); handled || e != nil {
		return authority, deadline, e
	}
	if lock {
		if e := collectionSourcesTx(ctx, tx, p); e != nil {
			return "", nil, e
		}
	}
	ids := []string{}
	for _, entry := range p.Entries {
		ids = append(ids, entry.PlaceID)
	}
	var token string
	var deadline *time.Time
	var current bool
	// Include provider retention epochs, copied-origin consent generations and
	// original sidecars for kept IDs. Removed own entries are intentionally not
	// part of the authority gate that runs after this operation's own effect.
	query := `SELECT jsonb_build_array(c.xmin::text,c.id,
 (SELECT jsonb_agg(jsonb_build_array(p.id,p.xmin::text,` + retainedPlaceReadBindingSQL("p") + `,borrowed.xmin::text,copy_origin.xmin::text,copy_origin.origin_authority,` + collectionCopyAuthoritySQL(in.Kind, "copy_origin.source_id", "$2") + `) ORDER BY p.id)
 FROM places p LEFT JOIN personal_collection_copy_borrowed_places borrowed ON borrowed.destination_kind=$3 AND borrowed.destination_id=$4 AND borrowed.place_id=p.id
 LEFT JOIN personal_collection_copy_origins copy_origin ON copy_origin.destination_kind=$3 AND copy_origin.destination_id=$4 AND copy_origin.receiver_account_id=$2
 WHERE p.id=ANY($5::uuid[]) AND p.city_id=c.id))::text,
 (SELECT min(least(c.expires_at,p.expires_at,` + currentRetainedPlaceDeadlineSQL("p", "clock_timestamp()") + `)) FROM places p WHERE p.id=ANY($5::uuid[]) AND p.city_id=c.id),
 c.publication_status='published' AND(c.expires_at IS NULL OR c.expires_at>clock_timestamp()) AND
 (SELECT count(*) FROM places p WHERE p.id=ANY($5::uuid[]) AND p.city_id=c.id AND p.publication_status='published' AND(p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND ` + currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()") + `)=$6
 FROM cities c WHERE c.id=$1`
	if e := tx.QueryRow(ctx, query, p.CityID, owner, in.Kind, in.CollectionID, ids, len(ids)).Scan(&token, &deadline, &current); e != nil {
		return "", nil, content.ErrUnavailable
	}
	if !current {
		return "", nil, content.ErrConflict
	}
	var actualCity string
	table, _, _, _ := collectionTables(in.Kind)
	if e := tx.QueryRow(ctx, `SELECT city_id FROM `+table+` WHERE id=$1 AND owner_account_id=$2`, in.CollectionID, owner).Scan(&actualCity); e != nil || actualCity != p.CityID {
		return "", nil, content.ErrConflict
	}
	return token, deadline, nil
}

func collectionControlReadReceipt(ctx context.Context, tx pgx.Tx, owner, op string) (content.CollectionControlReceipt, error) {
	r := content.CollectionControlReceipt{SchemaVersion: content.CollectionControlSchemaVersion, OperationID: op}
	var action, kind, id, digest *string
	var expected *int64
	var committed time.Time
	e := tx.QueryRow(ctx, `SELECT status,action,kind,target_id::text,expected_revision,applied_revision,payload_digest,committed_at FROM personal_collection_control_operations WHERE owner_account_id=$1 AND operation_id=$2 FOR SHARE`, owner, op).Scan(&r.Status, &action, &kind, &id, &expected, &r.AppliedRevision, &digest, &committed)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	if r.Status == "SUCCEEDED" {
		if action == nil || kind == nil || id == nil || digest == nil || expected == nil {
			return content.CollectionControlReceipt{}, content.ErrUnavailable
		}
		r.Action = *action
		r.Kind = *kind
		r.CollectionID = *id
		r.ExpectedRevision = expected
		r.PayloadDigest = *digest
		r.CommittedAt = &committed
	}
	if !content.ValidCollectionControlReceipt(r, op) {
		return content.CollectionControlReceipt{}, content.ErrUnavailable
	}
	return r, nil
}

func collectionControlPayload(v any) [32]byte { raw, _ := json.Marshal(v); return sha256.Sum256(raw) }

type collectionControlProof struct {
	s                                    *Store
	d                                    [32]byte
	a                                    identity.Actor
	frame                                identity.HumanControlFrame
	account, op, mode, target, authority string
	in                                   content.CollectionControlInput
	payload                              [32]byte
	until                                time.Time
}

func (p *collectionControlProof) RevalidateHumanControl(ctx context.Context) error {
	if p == nil || p.s == nil || ctx == nil || ctx.Err() != nil || !time.Now().Before(p.until) {
		return identity.ErrControlChanged
	}
	ctx = identity.WithHumanControlFrame(ctx, p.d, p.a, p.frame)
	ctx, v, e := p.s.collectionControlBegin(ctx, p.d, p.a, p.op)
	if e != nil {
		return e
	}
	defer v.frame.tx.Rollback(context.Background())
	if v.frame.session != p.frame || v.account != p.account {
		return identity.ErrControlChanged
	}
	check := func() error {
		if !time.Now().Before(p.until) {
			return identity.ErrControlChanged
		}
		if p.mode == "receipt" {
			r, e := collectionControlReadReceipt(ctx, v.frame.tx, p.a.ID, p.op)
			if e != nil || collectionControlPayload(r) != p.payload {
				return identity.ErrControlChanged
			}
			return nil
		}
		target, e := collectionControlTarget(ctx, v.frame.tx, p.a.ID, p.in, true)
		if e != nil || target != p.target {
			return identity.ErrControlChanged
		}
		authority, _, e := collectionControlAuthority(ctx, v.frame.tx, p.a.ID, p.in, true)
		if e != nil || authority != p.authority {
			return identity.ErrControlChanged
		}
		return nil
	}
	if e = check(); e != nil {
		return e
	}
	return p.s.collectionControlFinish(ctx, &v, check)
}

func (s *Store) PreviewHumanCollectionControl(ctx context.Context, d [32]byte, a identity.Actor, in content.CollectionControlInput) (content.CollectionControlPreview, error) {
	in, e := content.NormalizeCollectionControl(in)
	if e != nil {
		return content.CollectionControlPreview{}, e
	}
	ctx, v, e := s.collectionControlBegin(ctx, d, a, in.OperationID)
	if e != nil {
		return content.CollectionControlPreview{}, e
	}
	defer v.frame.tx.Rollback(context.Background())
	if _, e = collectionControlReadReceipt(ctx, v.frame.tx, a.ID, in.OperationID); e == nil {
		return content.CollectionControlPreview{}, content.ErrConflict
	} else if !errors.Is(e, pgx.ErrNoRows) {
		return content.CollectionControlPreview{}, content.ErrUnavailable
	}
	target, e := collectionControlTarget(ctx, v.frame.tx, a.ID, in, true)
	if e != nil {
		return content.CollectionControlPreview{}, e
	}
	authority, deadline, e := collectionControlAuthority(ctx, v.frame.tx, a.ID, in, true)
	if e != nil {
		return content.CollectionControlPreview{}, e
	}
	source := v.account + ":" + target + ":" + authority
	base, e := s.humanWritePreview(ctx, v.frame, collectionControlScope(in), in.Action, content.CollectionControlDigest(in), source, deadline)
	if e != nil {
		return content.CollectionControlPreview{}, e
	}
	out := content.CollectionControlPreview{HumanControlPreview: base, SchemaVersion: content.CollectionControlSchemaVersion, Kind: in.Kind, CollectionID: in.CollectionID, ExpectedRevision: in.ExpectedRevision, NormalizedInput: in.Input}
	proof := &collectionControlProof{s: s, d: d, a: a, frame: v.frame.session, account: v.account, op: in.OperationID, mode: "preview", target: target, authority: authority, in: in, until: time.Now().Add(base.ExpiresAt.Sub(base.ObservedAt))}
	if e = s.collectionControlFinish(ctx, &v, func() error {
		fresh, e := collectionControlTarget(ctx, v.frame.tx, a.ID, in, false)
		if e != nil || fresh != target {
			return identity.ErrControlChanged
		}
		fresh, _, e = collectionControlAuthority(ctx, v.frame.tx, a.ID, in, false)
		if e != nil || fresh != authority {
			return identity.ErrControlChanged
		}
		var nativeNow time.Time
		if e = v.frame.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&nativeNow); e != nil || !nativeNow.Before(base.ExpiresAt) {
			return identity.ErrControlChanged
		}
		return nil
	}); e != nil {
		return content.CollectionControlPreview{}, e
	}
	if v.until.Before(proof.until) {
		proof.until = v.until
	}
	out.ResponseProof = proof
	return out, nil
}

func (s *Store) attachCollectionControlReceipt(v collectionControlFrame, r *content.CollectionControlReceipt) {
	r.ResponseProof = &collectionControlProof{s: s, d: v.frame.d, a: v.frame.a, frame: v.frame.session, account: v.account, op: r.OperationID, mode: "receipt", payload: collectionControlPayload(*r), until: v.until}
}

func (s *Store) ApplyHumanCollectionControl(ctx context.Context, d [32]byte, a identity.Actor, in content.CollectionControlSubmission) (content.CollectionControlReceipt, error) {
	in, e := content.NormalizeCollectionControlSubmission(in)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	p := in.CollectionControlInput
	ctx, v, e := s.collectionControlBegin(ctx, d, a, p.OperationID)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	defer v.frame.tx.Rollback(context.Background())
	old, e := collectionControlReadReceipt(ctx, v.frame.tx, a.ID, p.OperationID)
	if e == nil {
		if old.Status == "SUCCEEDED" && (old.Action != p.Action || old.Kind != p.Kind || old.CollectionID != p.CollectionID || old.ExpectedRevision == nil || *old.ExpectedRevision != p.ExpectedRevision || old.PayloadDigest != content.CollectionControlDigest(p)) {
			return content.CollectionControlReceipt{}, content.ErrConflict
		}
		if e = s.collectionControlFinish(ctx, &v); e != nil {
			return content.CollectionControlReceipt{}, e
		}
		s.attachCollectionControlReceipt(v, &old)
		return old, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return content.CollectionControlReceipt{}, content.ErrUnavailable
	}
	target, e := collectionControlTarget(ctx, v.frame.tx, a.ID, p, true)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	authority, deadline, e := collectionControlAuthority(ctx, v.frame.tx, a.ID, p, true)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	expires, e := s.humanWriteApprove(ctx, v.frame, collectionControlScope(p), p.Action, content.CollectionControlDigest(p), v.account+":"+target+":"+authority, in.Approval, deadline)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	var applied int64
	if p.Action == "UPDATE" {
		applied, e = updateHumanCollectionTx(ctx, v.frame.tx, a.ID, p.CollectionID, p.ExpectedRevision, collectionControlLegacy(p))
	} else {
		applied, e = withdrawHumanCollectionTx(ctx, v.frame.tx, a.ID, p.Kind, p.CollectionID, p.ExpectedRevision)
	}
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	if _, e = v.frame.tx.Exec(ctx, `INSERT INTO personal_collection_control_operations(owner_account_id,operation_id,status,action,kind,target_id,expected_revision,applied_revision,payload_digest)VALUES($1,$2,'SUCCEEDED',$3,$4,$5,$6,$7,$8)`, a.ID, p.OperationID, p.Action, p.Kind, p.CollectionID, p.ExpectedRevision, applied, content.CollectionControlDigest(p)); e != nil {
		return content.CollectionControlReceipt{}, content.ErrUnavailable
	}
	out, e := collectionControlReadReceipt(ctx, v.frame.tx, a.ID, p.OperationID)
	if e != nil {
		return content.CollectionControlReceipt{}, content.ErrUnavailable
	}
	if e = s.collectionControlFinish(ctx, &v, func() error {
		fresh, _, e := collectionControlAuthority(ctx, v.frame.tx, a.ID, p, false)
		if e != nil || fresh != authority {
			return identity.ErrControlChanged
		}
		var now time.Time
		if e = v.frame.tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil || !now.Before(expires) {
			return identity.ErrControlChanged
		}
		return nil
	}); e != nil {
		return content.CollectionControlReceipt{}, e
	}
	s.attachCollectionControlReceipt(v, &out)
	return out, nil
}

func (s *Store) collectionControlHistory(ctx context.Context, d [32]byte, a identity.Actor, op string, stop bool) (content.CollectionControlReceipt, error) {
	if !content.ValidCollectionID(op) {
		return content.CollectionControlReceipt{}, content.ErrInvalid
	}
	ctx, v, e := s.collectionControlBegin(ctx, d, a, op)
	if e != nil {
		return content.CollectionControlReceipt{}, e
	}
	defer v.frame.tx.Rollback(context.Background())
	r, e := collectionControlReadReceipt(ctx, v.frame.tx, a.ID, op)
	if errors.Is(e, pgx.ErrNoRows) {
		if !stop {
			if e = s.collectionControlFinish(ctx, &v); e != nil {
				return content.CollectionControlReceipt{}, e
			}
			return content.CollectionControlReceipt{}, content.ErrNotFound
		}
		if _, e = v.frame.tx.Exec(ctx, `INSERT INTO personal_collection_control_operations(owner_account_id,operation_id,status,applied_revision)VALUES($1,$2,'STOPPED',0)`, a.ID, op); e != nil {
			return content.CollectionControlReceipt{}, content.ErrUnavailable
		}
		r, e = collectionControlReadReceipt(ctx, v.frame.tx, a.ID, op)
	}
	if e != nil {
		return content.CollectionControlReceipt{}, content.ErrUnavailable
	}
	if e = s.collectionControlFinish(ctx, &v); e != nil {
		return content.CollectionControlReceipt{}, e
	}
	s.attachCollectionControlReceipt(v, &r)
	return r, nil
}

func (s *Store) ReadHumanCollectionControl(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionControlReceipt, error) {
	return s.collectionControlHistory(ctx, d, a, op, false)
}
func (s *Store) StopHumanCollectionControl(ctx context.Context, d [32]byte, a identity.Actor, op string) (content.CollectionControlReceipt, error) {
	return s.collectionControlHistory(ctx, d, a, op, true)
}
func (s *Store) FinalizeHumanCollectionControlReceipt(ctx context.Context, d [32]byte, a identity.Actor, r content.CollectionControlReceipt) error {
	proof, ok := r.ResponseProof.(*collectionControlProof)
	if !ok || proof == nil || proof.s != s || proof.d != d || proof.a.ID != a.ID || proof.a.AccountType != a.AccountType || proof.mode != "receipt" || proof.op != r.OperationID || !content.ValidCollectionControlReceipt(r, r.OperationID) || collectionControlPayload(r) != proof.payload {
		return identity.ErrControlChanged
	}
	return proof.RevalidateHumanControl(ctx)
}
