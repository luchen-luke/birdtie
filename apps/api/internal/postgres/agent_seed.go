package postgres

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/agentprofile"
	"github.com/birdtie/birdtie/apps/api/internal/agentseed"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/jackc/pgx/v5"
	"reflect"
)

var _ agentseed.HumanStore = (*Store)(nil)

type seedBinding struct {
	private  agentPrivateBinding
	metadata agentprofile.Record
	digest   [32]byte
}
type seedSource struct {
	record    agentseed.Record
	profile   identity.Profile
	private   agentprofile.PrivateFields
	contextID string
	raw       []byte
}

// A source snapshot is an optimistic comparison token, never authorization.
// Account -> Agent -> metadata -> Profile -> context -> Session lock order.
func (s *Store) beginSeed(ctx context.Context, digest [32]byte, initial identity.Actor, write bool) (pgx.Tx, seedBinding, error) {
	if ctx == nil || ctx.Err() != nil || s == nil || s.pool == nil {
		return nil, seedBinding{}, agentseed.ErrUnavailable
	}
	principal, e := actorref.ParsePrincipal(initial.AccountType, initial.ID)
	if e != nil || principal.Type != actorref.Person || initial.AccountType != "person" || principal.ID != initial.ID || digest == ([32]byte{}) {
		return nil, seedBinding{}, agentseed.ErrForbidden
	}
	tx, e := s.pool.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.ReadCommitted})
	if e != nil {
		return nil, seedBinding{}, agentseed.ErrUnavailable
	}
	fail := func(e error) (pgx.Tx, seedBinding, error) {
		tx.Rollback(context.Background())
		return nil, seedBinding{}, e
	}
	if _, e = tx.Exec(ctx, `SET LOCAL TIME ZONE 'UTC'`); e != nil {
		return fail(agentseed.ErrUnavailable)
	}
	// Account is only an authority source; compatible SHARE protects its current
	// state without excluding other existing ordinary owner operations.
	lock := " FOR SHARE"
	b := seedBinding{digest: digest}
	e = tx.QueryRow(ctx, `SELECT id FROM accounts WHERE id=$1 AND account_type='person' AND status='active'`+lock, principal.ID).Scan(&b.private.accountID)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(agentseed.ErrForbidden)
	}
	if e != nil {
		return fail(agentseed.ErrUnavailable)
	}
	e = tx.QueryRow(ctx, `SELECT id FROM agents WHERE principal_account_id=$1 AND agent_type='personal' AND status='active' FOR SHARE`, principal.ID).Scan(&b.private.agentID)
	if errors.Is(e, pgx.ErrNoRows) {
		return fail(agentseed.ErrForbidden)
	}
	if e != nil {
		return fail(agentseed.ErrUnavailable)
	}
	b.metadata, e = lockAgentPrivateMetadata(ctx, tx, b.private, write)
	if e != nil {
		return fail(seedError(e))
	}
	// Authenticate again without locking Session before resource waits.
	var current bool
	e = tx.QueryRow(ctx, seedCurrentSQL, digest[:], principal.ID, s.devPhoneEnabled).Scan(&current)
	if e != nil {
		return fail(agentseed.ErrUnavailable)
	}
	if !current {
		return fail(agentseed.ErrForbidden)
	}
	return tx, b, nil
}

const seedCurrentSQL = `WITH n AS MATERIALIZED(SELECT clock_timestamp() t) SELECT EXISTS(SELECT 1 FROM sessions s JOIN accounts a ON a.id=s.account_id,n WHERE s.token_sha256=$1 AND a.id=$2 AND a.account_type='person' AND a.status='active' AND s.revoked_at IS NULL AND s.expires_at>n.t AND s.idle_expires_at>n.t AND ($3::boolean OR s.authentication_method<>'dev_phone'))`

func seedError(e error) error {
	if errors.Is(e, agentprofile.ErrConflict) {
		return agentseed.ErrConflict
	}
	if errors.Is(e, agentprofile.ErrForbidden) || errors.Is(e, agentprofile.ErrNotFound) {
		return agentseed.ErrForbidden
	}
	return agentseed.ErrUnavailable
}
func (s *Store) lockSeedSession(ctx context.Context, tx pgx.Tx, b *seedBinding) error {
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() t) SELECT s.id FROM sessions s,n WHERE s.token_sha256=$1 AND s.account_id=$2 AND s.revoked_at IS NULL AND s.expires_at>n.t AND s.idle_expires_at>n.t AND ($3::boolean OR s.authentication_method<>'dev_phone') FOR SHARE OF s`, b.digest[:], b.private.accountID, s.devPhoneEnabled).Scan(&b.private.sessionID)
	if errors.Is(e, pgx.ErrNoRows) {
		return agentseed.ErrForbidden
	}
	if e != nil {
		return agentseed.ErrUnavailable
	}
	return s.finalSeed(ctx, tx, *b)
}
func (s *Store) finalSeed(ctx context.Context, tx pgx.Tx, b seedBinding) error {
	var current bool
	e := tx.QueryRow(ctx, seedCurrentSQL, b.digest[:], b.private.accountID, s.devPhoneEnabled).Scan(&current)
	if e != nil || ctx.Err() != nil {
		return agentseed.ErrUnavailable
	}
	if !current {
		return agentseed.ErrForbidden
	}
	return nil
}

// loadSeed locks each native source before computing the opaque comparison.
// Source serialization is transaction-local UTC, independent of pool timezone.
func loadSeed(ctx context.Context, tx pgx.Tx, b seedBinding, write bool) (seedSource, error) {
	v := seedSource{record: agentseed.Record{SchemaVersion: agentseed.Schema, OwnerID: b.private.accountID, AgentID: b.private.agentID, PrivateProfileVersion: b.metadata.ProfileVersion, Intent: agentseed.UserIntent{Progress: "UNSET", InterestChoice: "KEEP"}, Cities: []agentseed.City{}}}
	lock := " FOR SHARE"
	if write {
		lock = " FOR UPDATE"
	}
	var pRaw, privateRaw, metaRaw, intentRaw, contextRaw []byte
	currentCityRaw := []byte(`null`)
	e := tx.QueryRow(ctx, `SELECT to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) FROM user_profiles p WHERE account_id=$1`+lock, b.private.accountID).Scan(&pRaw)
	if e != nil {
		return v, agentseed.ErrUnavailable
	}
	var p struct {
		DisplayName string `json:"display_name"`
		Bio         string `json:"bio"`
		Visibility  string `json:"visibility"`
	}
	if json.Unmarshal(pRaw, &p) != nil {
		return v, agentseed.ErrUnavailable
	}
	v.profile = identity.Profile{AccountID: b.private.accountID, DisplayName: p.DisplayName, Bio: p.Bio, Visibility: p.Visibility}
	v.record.DisplayName = p.DisplayName
	v.record.ProfileVisibility = p.Visibility
	e = tx.QueryRow(ctx, `SELECT to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) FROM agent_profiles p WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`, b.private.agentID, b.private.accountID).Scan(&metaRaw)
	if e != nil {
		return v, agentseed.ErrUnavailable
	}
	var rawFields []byte
	e = tx.QueryRow(ctx, `SELECT fields,to_jsonb(p)||jsonb_build_object('_xmin',p.xmin::text) FROM agent_private_profiles p WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`+lock, b.private.agentID, b.private.accountID).Scan(&rawFields, &privateRaw)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, agentseed.ErrUnavailable
	}
	if e == nil {
		v.private, e = agentprofile.DecodePrivateFields(rawFields)
	} else {
		v.private, e = agentprofile.NormalizePrivateFields(agentprofile.PrivateFields{})
		privateRaw = []byte(`null`)
	}
	if e != nil {
		return v, agentseed.ErrUnavailable
	}
	v.record.LanguagePreferences = v.private.LanguagePreferences
	v.record.Interests = v.private.PersonalPreferences
	e = tx.QueryRow(ctx, `SELECT to_jsonb(i)||jsonb_build_object('_xmin',i.xmin::text) FROM agent_seed_user_intents i WHERE agent_id=$1 AND owner_id=$2 AND owner_type='PERSON'`+lock, b.private.agentID, b.private.accountID).Scan(&intentRaw)
	if e != nil && !errors.Is(e, pgx.ErrNoRows) {
		return v, agentseed.ErrUnavailable
	}
	if errors.Is(e, pgx.ErrNoRows) {
		intentRaw = []byte(`null`)
	} else {
		var x struct {
			Version        int64            `json:"version"`
			BasicIntent    *string          `json:"basic_intent"`
			Progress       string           `json:"progress"`
			InterestChoice string           `json:"interest_choice"`
			CreatedAt      *json.RawMessage `json:"created_at"`
			UpdatedAt      *json.RawMessage `json:"updated_at"`
		}
		if json.Unmarshal(intentRaw, &x) != nil {
			return v, agentseed.ErrUnavailable
		}
		v.record.Intent.Version = x.Version
		v.record.Intent.Progress = x.Progress
		v.record.Intent.InterestChoice = x.InterestChoice
		if x.BasicIntent != nil {
			v.record.Intent.BasicIntent = *x.BasicIntent
		}
		if x.CreatedAt != nil && json.Unmarshal(*x.CreatedAt, &v.record.Intent.CreatedAt) != nil {
			return v, agentseed.ErrUnavailable
		}
		if x.UpdatedAt != nil && json.Unmarshal(*x.UpdatedAt, &v.record.Intent.UpdatedAt) != nil {
			return v, agentseed.ErrUnavailable
		}
	}
	// Lock the actual declaration rows even when its City is no longer published.
	contextLock := " FOR SHARE OF pc,c"
	if write {
		contextLock = " FOR UPDATE OF pc FOR SHARE OF c"
	}
	rows, e := tx.Query(ctx, `SELECT pc.context_id::text,to_jsonb(pc)||jsonb_build_object('_xmin',pc.xmin::text,'_contextSource',to_jsonb(c),'_contextXmin',c.xmin::text) FROM person_contexts pc JOIN contexts c ON c.id=pc.context_id WHERE pc.person_account_id=$1 AND pc.relation='current' AND c.context_type='CITY' ORDER BY pc.context_id`+contextLock, b.private.accountID)
	if e != nil {
		return v, agentseed.ErrUnavailable
	}
	contextParts := []json.RawMessage{}
	for rows.Next() {
		var id string
		var raw []byte
		if rows.Scan(&id, &raw) != nil {
			rows.Close()
			return v, agentseed.ErrUnavailable
		}
		v.contextID = id
		contextParts = append(contextParts, raw)
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(contextParts) > 1 {
		return v, agentseed.ErrUnavailable
	}
	contextRaw, _ = json.Marshal(contextParts)
	rows, e = tx.Query(ctx, `SELECT city.id,city.name,c.id::text,to_jsonb(city)||jsonb_build_object('_xmin',city.xmin::text,'_context',c.id,'_contextXmin',c.xmin::text,'_cityContext',to_jsonb(cc),'_cityContextXmin',cc.xmin::text) FROM cities city JOIN city_contexts cc ON cc.city_id=city.id AND cc.status='active' JOIN contexts c ON c.context_type='CITY' AND c.city_id=city.id WHERE city.publication_status='published' AND (city.expires_at IS NULL OR (isfinite(city.expires_at) AND city.expires_at>clock_timestamp())) ORDER BY city.id FOR SHARE OF city,c,cc`)
	if e != nil {
		return v, agentseed.ErrUnavailable
	}
	cityParts := []json.RawMessage{}
	for rows.Next() {
		var city agentseed.City
		var cx string
		var raw []byte
		if rows.Scan(&city.ID, &city.Name, &cx, &raw) != nil {
			rows.Close()
			return v, agentseed.ErrUnavailable
		}
		v.record.Cities = append(v.record.Cities, city)
		cityParts = append(cityParts, raw)
		citySum := sha256.Sum256(raw)
		city.Snapshot = hex.EncodeToString(citySum[:])
		v.record.Cities[len(v.record.Cities)-1] = city
		if cx == v.contextID {
			x := city
			v.record.CurrentCity = &x
			currentCityRaw = raw
		}
	}
	e = rows.Err()
	rows.Close()
	if e != nil || len(cityParts) > 200 {
		return v, agentseed.ErrUnavailable
	}
	// Bind the review to this trusted session as well as the current sources.
	// A freshly authenticated replacement session cannot reuse an old review.
	sessionBinding, _ := json.Marshal(hex.EncodeToString(b.digest[:]))
	v.raw, _ = json.Marshal([]json.RawMessage{sessionBinding, metaRaw, pRaw, privateRaw, intentRaw, contextRaw, currentCityRaw})
	sum := sha256.Sum256(v.raw)
	v.record.Snapshot = hex.EncodeToString(sum[:])
	v.record.NeedsPrompt = v.record.Intent.Progress != "DEFERRED" && (v.record.Intent.Progress != "COMPLETED" || v.record.CurrentCity == nil || len(v.record.LanguagePreferences) == 0)
	return v, nil
}
func (s *Store) ReadOwnAgentSeed(ctx context.Context, digest [32]byte, initial identity.Actor) (agentseed.Record, error) {
	tx, b, e := s.beginSeed(ctx, digest, initial, false)
	if e != nil {
		return agentseed.Record{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := loadSeed(ctx, tx, b, false)
	if e != nil {
		return agentseed.Record{}, e
	}
	if e = s.lockSeedSession(ctx, tx, &b); e != nil {
		return agentseed.Record{}, e
	}
	// Current final payload re-read uses the same locked sources after Session.
	v, e = loadSeed(ctx, tx, b, false)
	if e != nil {
		return agentseed.Record{}, e
	}
	if e = s.finalSeed(ctx, tx, b); e != nil {
		return agentseed.Record{}, e
	}
	if v.record.CurrentCity != nil {
		if e = finalSeedCity(ctx, tx, v.record.CurrentCity.ID); e != nil {
			return agentseed.Record{}, e
		}
	}
	if tx.Commit(ctx) != nil {
		return agentseed.Record{}, agentseed.ErrUnavailable
	}
	return v.record, nil
}
func (s *Store) SaveOwnAgentSeed(ctx context.Context, digest [32]byte, initial identity.Actor, input agentseed.Input) (agentseed.Record, error) {
	input, e := agentseed.Normalize(input)
	if e != nil {
		return agentseed.Record{}, e
	}
	tx, b, e := s.beginSeed(ctx, digest, initial, true)
	if e != nil {
		return agentseed.Record{}, e
	}
	defer tx.Rollback(context.Background())
	v, e := loadSeed(ctx, tx, b, true)
	if e != nil {
		return agentseed.Record{}, e
	}
	if v.record.Snapshot != input.ExpectedSnapshot {
		return agentseed.Record{}, agentseed.ErrConflict
	}
	if e = s.lockSeedSession(ctx, tx, &b); e != nil {
		return agentseed.Record{}, e
	}
	progress, choice, basic := "DEFERRED", v.record.Intent.InterestChoice, v.record.Intent.BasicIntent
	if input.Action == "SAVE" {
		selectedCurrent := false
		for _, city := range v.record.Cities {
			if city.ID == input.CurrentCityID && city.Snapshot == input.CurrentCitySnapshot {
				selectedCurrent = true
			}
		}
		if !selectedCurrent {
			return agentseed.Record{}, agentseed.ErrConflict
		}
		var chosenContext string
		e = tx.QueryRow(ctx, `SELECT c.id FROM contexts c JOIN cities city ON city.id=c.city_id WHERE c.context_type='CITY' AND city.id=$1 AND city.publication_status='published'`, input.CurrentCityID).Scan(&chosenContext)
		if errors.Is(e, pgx.ErrNoRows) {
			return agentseed.Record{}, agentseed.ErrInvalid
		}
		if e != nil {
			return agentseed.Record{}, agentseed.ErrUnavailable
		}
		if _, e = updateOwnProfileInTx(ctx, tx, b.private.accountID, identity.ProfileInput{DisplayName: input.DisplayName, Bio: v.profile.Bio, Visibility: v.profile.Visibility}); e != nil {
			return agentseed.Record{}, agentseed.ErrUnavailable
		}
		if chosenContext != v.contextID {
			if _, e = tx.Exec(ctx, `DELETE FROM person_contexts pc USING contexts c WHERE pc.context_id=c.id AND pc.person_account_id=$1 AND pc.relation='current' AND c.context_type='CITY'`, b.private.accountID); e != nil {
				return agentseed.Record{}, agentseed.ErrUnavailable
			}
			if _, e = tx.Exec(ctx, `INSERT INTO person_contexts(person_account_id,context_id,relation,visibility) VALUES($1,$2,'current','private')`, b.private.accountID, chosenContext); e != nil {
				return agentseed.Record{}, agentseed.ErrUnavailable
			}
			if e = insertDomainAudit(ctx, tx, b.private.accountID, "declare", "person_context", chosenContext, "human_seed_current_city", nil); e != nil {
				return agentseed.Record{}, agentseed.ErrUnavailable
			}
		}
		// The seed review explicitly saves a private City declaration, including
		// when it reuses an older current declaration with another visibility.
		changedVisibility, e := tx.Exec(ctx, `UPDATE person_contexts SET visibility='private' WHERE person_account_id=$1 AND context_id=$2 AND relation='current' AND visibility<>'private'`, b.private.accountID, chosenContext)
		if e != nil {
			return agentseed.Record{}, agentseed.ErrUnavailable
		}
		if changedVisibility.RowsAffected() != 0 {
			if e = insertDomainAudit(ctx, tx, b.private.accountID, "privatize", "person_context", chosenContext, "human_seed_current_city", nil); e != nil {
				return agentseed.Record{}, agentseed.ErrUnavailable
			}
		}
		wanted := v.private
		wanted.LanguagePreferences = input.LanguagePreferences
		if input.InterestChoice == "SET" {
			wanted.PersonalPreferences = input.Interests
		}
		if !reflect.DeepEqual(wanted, v.private) {
			p, e := replaceAgentPrivateInTx(ctx, tx, b.private, b.metadata, agentprofile.ReplacePrivateInput{ExpectedVersion: b.metadata.ProfileVersion, Fields: wanted})
			if e != nil {
				return agentseed.Record{}, seedError(e)
			}
			b.metadata = p.Profile
		}
		progress, choice, basic = "COMPLETED", input.InterestChoice, input.BasicIntent
	}
	// A repeat of the same semantic settings is a no-op. An old approved source
	// snapshot is still rejected above, preventing duplicate effects on replay.
	if v.record.Intent.Progress != progress || v.record.Intent.InterestChoice != choice || v.record.Intent.BasicIntent != basic {
		var basicValue any
		if basic != "" {
			basicValue = basic
		}
		_, e = tx.Exec(ctx, `INSERT INTO agent_seed_user_intents(agent_id,owner_id,owner_type,version,basic_intent,progress,interest_choice) VALUES($1,$2,'PERSON',1,$3,$4,$5) ON CONFLICT(agent_id) DO UPDATE SET version=agent_seed_user_intents.version+1,basic_intent=EXCLUDED.basic_intent,progress=EXCLUDED.progress,interest_choice=EXCLUDED.interest_choice`, b.private.agentID, b.private.accountID, basicValue, progress, choice)
		if e != nil {
			return agentseed.Record{}, agentseed.ErrUnavailable
		}
		if e = insertDomainAudit(ctx, tx, b.private.accountID, "replace", "agent_seed_intent", b.private.agentID, "human_seed_completion", nil); e != nil {
			return agentseed.Record{}, agentseed.ErrUnavailable
		}
	}
	v, e = loadSeed(ctx, tx, b, true)
	if e != nil {
		return agentseed.Record{}, e
	}
	if e = s.finalSeed(ctx, tx, b); e != nil {
		return agentseed.Record{}, e
	}
	if input.Action == "SAVE" {
		if e = finalSeedCity(ctx, tx, input.CurrentCityID); e != nil {
			return agentseed.Record{}, e
		}
	}
	if tx.Commit(ctx) != nil {
		return agentseed.Record{}, agentseed.ErrUnavailable
	}
	return v.record, nil
}

func finalSeedCity(ctx context.Context, tx pgx.Tx, id string) error {
	var current bool
	e := tx.QueryRow(ctx, `WITH n AS MATERIALIZED(SELECT clock_timestamp() t) SELECT EXISTS(SELECT 1 FROM cities city JOIN city_contexts cc ON cc.city_id=city.id AND cc.status='active',n WHERE city.id=$1 AND city.publication_status='published' AND(city.expires_at IS NULL OR(isfinite(city.expires_at) AND city.expires_at>n.t)))`, id).Scan(&current)
	if e != nil || ctx.Err() != nil {
		return agentseed.ErrUnavailable
	}
	if !current {
		return agentseed.ErrConflict
	}
	return nil
}
