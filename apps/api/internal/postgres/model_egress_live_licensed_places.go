package postgres

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"reflect"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/cityseed"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	"github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
)

const licensedPlaceBatchSchema = "birdtie.native-licensed-place-batch.v1"

type licensedPlaceGeneration struct {
	ID         string `json:"id"`
	Generation string `json:"generation"`
}
type licensedPlaceBatchData struct {
	Schema      string                                     `json:"schema"`
	Context     modelegressbudget.LiveLicensedPlaceContext `json:"context"`
	Facts       []agentworkspace.LocalPlaceFactRef         `json:"facts"`
	Places      []licensedPlaceGeneration                  `json:"places"`
	CityProof   string                                     `json:"cityProof"`
	FieldProof  string                                     `json:"fieldProof"`
	QueryDigest string                                     `json:"queryDigest"`
	DeadlineAt  time.Time                                  `json:"deadlineAt"`
}
type nativeLicensedPlaceBatch struct {
	data   licensedPlaceBatchData
	digest string
	valid  bool
}

func licensedPlaceBatchDigest(d licensedPlaceBatchData) string {
	raw, _ := json.Marshal(d)
	return liveDigestBytes("birdtie.native-licensed-place-batch.v1", raw)
}
func readLicensedPlaceBatch(raw []byte) (licensedPlaceBatchData, error) {
	var d licensedPlaceBatchData
	if len(raw) < 1 || len(raw) > 16*1024 {
		return d, modelegressbudget.ErrDenied
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if dec.Decode(&d) != nil || dec.Decode(new(any)) != io.EOF || d.Schema != licensedPlaceBatchSchema || len(d.Facts) < 1 || len(d.Facts) > 60 || len(d.Places) < 1 || len(d.Places) > 10 || !livePlaceGeneration(d.CityProof) || !livePlaceGeneration(d.FieldProof) || !livePlaceGeneration(d.QueryDigest) || d.DeadlineAt.IsZero() || modelegressbudget.ValidateLiveLicensedPlaceContext(d.Context) != nil {
		return licensedPlaceBatchData{}, modelegressbudget.ErrDenied
	}
	if len(d.Places) != len(d.Context.Places) {
		return licensedPlaceBatchData{}, modelegressbudget.ErrDenied
	}
	seen := map[string]bool{}
	for _, p := range d.Places {
		if !egressUUID(p.ID) || !livePlaceGeneration(p.Generation) || seen[p.ID] {
			return licensedPlaceBatchData{}, modelegressbudget.ErrDenied
		}
		seen[p.ID] = true
	}
	seenFields := map[string]bool{}
	keys := map[string]map[string]bool{}
	for _, f := range d.Facts {
		if !egressUUID(f.EvidenceID) || !seen[f.PlaceID] || !egressUUID(f.PermissionID) || f.Version < 1 || !cityseed.ValidPlaceFieldKey(f.Field) || seenFields[f.EvidenceID] {
			return licensedPlaceBatchData{}, modelegressbudget.ErrDenied
		}
		seenFields[f.EvidenceID] = true
		if keys[f.PlaceID] == nil {
			keys[f.PlaceID] = map[string]bool{}
		}
		keys[f.PlaceID][f.Field] = true
	}
	for _, p := range d.Places {
		if !keys[p.ID]["name"] || !keys[p.ID]["address"] || !keys[p.ID]["coordinates"] {
			return licensedPlaceBatchData{}, modelegressbudget.ErrDenied
		}
	}
	return d, nil
}

// The first source route admits the precisely reviewed OSM/ODbL facts. A
// CONFIRMED retention/publication declaration for an unknown licence does not
// automatically grant model processing. No provider search artefact is used.
func licensedPlaceFieldsAllowed(place foundation.Place, fields []cityseed.PlaceFieldEvidence, at time.Time) bool {
	if !localRecommendationFieldsUsable(place, fields, at) {
		return false
	}
	for _, f := range fields {
		p := f.Permission
		if p.Acquisition != "OPEN_LICENSE" || p.OriginProvider != "OPEN_DATA_DOWNLOAD" || p.LicenseURL != "https://opendatacommons.org/licenses/odbl/1-0/" || p.LicenseVersion != "ODbL-1.0" {
			return false
		}
	}
	return true
}

func licensedPublicPlaceProjection(place foundation.Place, fields []cityseed.PlaceFieldEvidence, sources *[]modelegressbudget.LivePublicSource) (modelegressbudget.LiveLicensedPlace, error) {
	p := modelegressbudget.LiveLicensedPlace{Name: place.Name, SourceNumbers: []int{}}
	by, ok := localFieldsForPlace(place, fields)
	if !ok {
		return p, modelegressbudget.ErrDenied
	}
	if f, ok := by["address"]; ok {
		if json.Unmarshal(f.Value, &p.Address) != nil {
			return p, modelegressbudget.ErrDenied
		}
	}
	if f, ok := by["opening_hours"]; ok {
		if json.Unmarshal(f.Value, &p.OpeningHours) != nil {
			return p, modelegressbudget.ErrDenied
		}
	}
	if f, ok := by["food_seafood"]; ok {
		var v bool
		if json.Unmarshal(f.Value, &v) != nil {
			return p, modelegressbudget.ErrDenied
		}
		p.Seafood = &v
	}
	if f, ok := by["food_spicy"]; ok {
		var v bool
		if json.Unmarshal(f.Value, &v) != nil {
			return p, modelegressbudget.ErrDenied
		}
		p.Spicy = &v
	}
	seen := map[int]bool{}
	for _, f := range fields {
		number := 0
		for i, s := range *sources {
			if s.URL == f.Permission.SourceURL {
				number = i + 1
				break
			}
		}
		if number == 0 {
			if len(*sources) >= 10 {
				return p, modelegressbudget.ErrInvalid
			}
			*sources = append(*sources, modelegressbudget.LivePublicSource{Title: f.Permission.Attribution, URL: f.Permission.SourceURL, Passage: ""})
			number = len(*sources)
		}
		if !seen[number] {
			p.SourceNumbers = append(p.SourceNumbers, number)
			seen[number] = true
		}
	}
	return p, nil
}

// Same original public-place selection: CITY/publication/retained-review,
// literal term, category and bounds. It contains no personal source or Memory.
func (s *Store) nativeLicensedPlacesTx(ctx context.Context, tx pgx.Tx, p PreparedLiveEgress) (nativeLicensedPlaceBatch, error) {
	task, e := scanAgentTask(tx.QueryRow(ctx, `SELECT `+agentTaskColumns+` FROM agent_tasks WHERE id=$1 AND owner_account_id=$2`, p.input.TaskID, p.owner))
	if e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	if task.Intent != agentworkspace.FindPlace || task.Status != agentworkspace.TaskActive || task.PrincipalType != "person" || task.PrincipalID != p.owner || task.ActingUserID != p.owner || task.ContextType != "CITY" {
		return nativeLicensedPlaceBatch{}, modelegressbudget.ErrDenied
	}
	q, e := replyCaptureQuery(task)
	if e != nil || q.Kind != "place" {
		return nativeLicensedPlaceBatch{}, modelegressbudget.ErrDenied
	}
	selected, cityRaw, e := nativeResolvedPublicCityTx(ctx, tx, task.CityID, p.input.DeadlineAt)
	if e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	var cityContext struct {
		Context string `json:"context"`
	}
	if json.Unmarshal(cityRaw, &cityContext) != nil || cityContext.Context != task.ContextID {
		return nativeLicensedPlaceBatch{}, modelegressbudget.ErrDenied
	}
	boundPlace, _, e := nativeResolvedPlaceFromTaskTx(ctx, tx, task, p.input.DeadlineAt)
	if e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	bounds, _ := json.Marshal(q.Bounds)
	rows, e := tx.Query(ctx, `SELECT `+placeColumns+` FROM places p JOIN cities c ON c.id=p.city_id WHERE p.city_id=$1 AND p.publication_status='published' AND c.publication_status='published' AND (p.expires_at IS NULL OR p.expires_at>clock_timestamp()) AND `+currentRetainedPlaceVisibilitySQL("p", "clock_timestamp()")+` AND ($2='' OR strpos(lower(p.name||' '||p.summary),lower($2))>0) AND ($3='' OR p.category_code=$3) AND ($4::jsonb='null'::jsonb OR (p.longitude BETWEEN ($4::jsonb->>'West')::float8 AND ($4::jsonb->>'East')::float8 AND p.latitude BETWEEN ($4::jsonb->>'South')::float8 AND ($4::jsonb->>'North')::float8)) ORDER BY p.name,p.id LIMIT 30 FOR SHARE OF p,c`, task.CityID, q.SearchTerm, q.Category, bounds)
	if e != nil {
		return nativeLicensedPlaceBatch{}, egressError(e)
	}
	places := []foundation.Place{}
	for rows.Next() {
		v, err := scanPlace(rows)
		if err != nil {
			rows.Close()
			return nativeLicensedPlaceBatch{}, err
		}
		places = append(places, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	slots, e := resolvedPublicTaskSlots(task)
	if e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	d := licensedPlaceBatchData{Schema: licensedPlaceBatchSchema, Context: modelegressbudget.LiveLicensedPlaceContext{Schema: modelegressbudget.LiveLicensedPlaceContextSchema, City: selected, Slots: slots, Places: []modelegressbudget.LiveLicensedPlace{}, Sources: []modelegressbudget.LivePublicSource{}}, Facts: []agentworkspace.LocalPlaceFactRef{}, Places: []licensedPlaceGeneration{}, CityProof: liveDigestBytes("birdtie.licensed-native-city.v1", cityRaw), QueryDigest: p.queryDigest, DeadlineAt: p.input.DeadlineAt}
	allFields := []cityseed.PlaceFieldEvidence{}
	for _, place := range places {
		if boundPlace.valid && place.ID != boundPlace.id {
			continue
		}
		fields, err := ReadPlaceFieldEvidenceTx(ctx, tx, place.ID, []string{"name", "address", "coordinates", "food_seafood", "food_spicy", "opening_hours"}, now)
		if err != nil {
			return nativeLicensedPlaceBatch{}, err
		}
		if !licensedPlaceFieldsAllowed(place, fields, now) {
			continue
		}
		current, err := nativePublicFollowupPlaceTx(ctx, tx, place.ID, task.CityID, p.input.DeadlineAt)
		if err != nil {
			return nativeLicensedPlaceBatch{}, err
		}
		next := d.Context
		next.Places = append([]modelegressbudget.LiveLicensedPlace(nil), d.Context.Places...)
		next.Sources = append([]modelegressbudget.LivePublicSource(nil), d.Context.Sources...)
		projection, err := licensedPublicPlaceProjection(place, fields, &next.Sources)
		if err != nil {
			continue
		}
		next.Places = append(next.Places, projection)
		if _, err = modelegressbudget.LiveLicensedPlaceModelPayload(p.query, next); err != nil {
			continue
		}
		d.Context = next
		d.Places = append(d.Places, licensedPlaceGeneration{place.ID, current.generation})
		allFields = append(allFields, fields...)
		for _, f := range fields {
			if p.input.DeadlineAt.After(f.ValidTo) || p.input.DeadlineAt.After(f.RefreshDue) || p.input.DeadlineAt.After(f.Permission.RetentionUntil) {
				return nativeLicensedPlaceBatch{}, modelegressbudget.ErrDenied
			}
			d.Facts = append(d.Facts, agentworkspace.LocalPlaceFactRef{EvidenceID: f.ID, PlaceID: f.PlaceID, Field: f.FieldKey, Version: f.Version, PermissionID: f.PermissionID})
		}
		if len(d.Context.Places) >= 10 {
			break
		}
	}
	if len(d.Places) == 0 {
		return nativeLicensedPlaceBatch{}, agentworkspace.ErrLicensedPlaceFactsUnavailable
	}
	d.FieldProof = localFieldDigest(allFields, nil)
	raw, _ := json.Marshal(d)
	if _, e = readLicensedPlaceBatch(raw); e != nil {
		return nativeLicensedPlaceBatch{}, e
	}
	return nativeLicensedPlaceBatch{data: d, digest: licensedPlaceBatchDigest(d), valid: true}, nil
}

func (s *Store) prepareLicensedPlaceEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, in modelegressbudget.PreviewInput, projector LiveWireProjector) (PreparedLiveEgress, error) {
	p, e := s.prepareLiveEgressTx(ctx, tx, a, in, projector)
	if e != nil {
		return p, e
	}
	if p.price.Kind != modelegressbudget.LiveToken || p.request.PromptVersion != modelegressbudget.LiveLicensedPlacePolicyVersion || len(p.request.Messages) != 2 || p.request.Messages[0].Content != modelegressbudget.LiveLicensedPlacePrompt {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	batch, e := s.nativeLicensedPlacesTx(ctx, tx, p)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	payload, e := modelegressbudget.LiveLicensedPlaceModelPayload(p.query, batch.data.Context)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	p.request.Messages = append([]modelgateway.Message(nil), p.request.Messages...)
	p.request.Messages[1].Content = payload
	provider := liveProviderRequest(p.request)
	wire, e := projector.Prepare(liveProviderRequest(p.request))
	if e != nil || !liveTokenWireMatches(p.price, provider, wire, time.Now()) {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p.modelWire, p.payloadDigest = wire, wire.WireDigest()
	p.licensed = batch
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return PreparedLiveEgress{}, e
	}
	p.digest, e = modelegressbudget.DigestLiveLicensedPlaceExport(liveResolvedDigestInput(p, modelegressbudget.LiveLicensedPlaceScope, modelegressbudget.Purpose), batch.digest, p.price, now)
	return p, e
}

func (s *Store) PreviewOwnLicensedPlaceEgress(ctx context.Context, a agentevent.Access, in modelegressbudget.PreviewInput, projector LiveWireProjector) (LiveEgressPreview, error) {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	defer tx.Rollback(ctx)
	p, e := s.prepareLicensedPlaceEgressTx(ctx, tx, a, in, projector)
	if e != nil {
		return LiveEgressPreview{}, e
	}
	if e = egressOwnerLock(ctx, tx, p.owner); e != nil {
		return LiveEgressPreview{}, e
	}
	raw, _ := json.Marshal(p.licensed.data)
	out := livePreview(p, "", "DRAFT")
	e = tx.QueryRow(ctx, `INSERT INTO model_egress_previews(root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,scope,purpose,billing_kind,current_query_digest,egress_payload_digest,source_batch,source_evidence_digest) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19) RETURNING id`, in.RootTraceID, in.TaskID, p.owner, p.session, p.request.Agent.AgentID, p.binding, p.source, p.authority, p.digest, p.price.Base.Version, in.MaxOutputTokens, in.DeadlineAt, out.Scope, out.Purpose, p.price.Kind, p.queryDigest, p.payloadDigest, raw, p.licensed.digest).Scan(&out.ID)
	if e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	if e = egressAudit(ctx, tx, p.owner, in.RootTraceID, "", "PREVIEW"); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = finishLivePreparation(ctx, tx, p); e != nil {
		return LiveEgressPreview{}, e
	}
	if e = tx.Commit(ctx); e != nil {
		return LiveEgressPreview{}, egressError(e)
	}
	return out, nil
}

func (s *Store) replayLicensedPlaceEgressTx(ctx context.Context, tx pgx.Tx, a agentevent.Access, v storedLiveEgressPreview, projector LiveWireProjector) (PreparedLiveEgress, error) {
	if v.kind != modelegressbudget.LiveToken {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	data, e := readLicensedPlaceBatch(v.sourceBatch)
	if e != nil || licensedPlaceBatchDigest(data) != v.sourceEvidenceDigest {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	p, e := s.prepareLicensedPlaceEgressTx(ctx, tx, a, v.in, projector)
	if e != nil {
		return PreparedLiveEgress{}, e
	}
	if !reflect.DeepEqual(data, p.licensed.data) || p.licensed.digest != v.sourceEvidenceDigest {
		return PreparedLiveEgress{}, modelegressbudget.ErrDenied
	}
	return p, nil
}
