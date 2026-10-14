package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	av "github.com/birdtie/birdtie/apps/api/internal/agentvisual"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
	mb "github.com/birdtie/birdtie/apps/api/internal/modelegressbudget"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"time"
)

type AgentVisualOptions struct {
	Adapter              *modelgateway.TencentVisionAdapter
	Gate                 modelgateway.LiveGate
	PriceVersion, Region string
}
type agentVisualGateway struct {
	store   *Store
	options AgentVisualOptions
}

func NewAgentVisualGateway(s *Store, o AgentVisualOptions) av.Gateway {
	return &agentVisualGateway{s, o}
}

// Trusted local maintenance only. Expiry is enforced on every read/dispatch;
// this bounded sweep additionally removes abandoned preview bytes at rest.
func (s *Store) PurgeExpiredAgentVisualPixels(ctx context.Context) (int64, error) {
	if s == nil || s.pool == nil {
		return 0, av.ErrUnavailable
	}
	r, e := s.pool.Exec(ctx, `WITH expired AS (SELECT id FROM agent_visual_previews WHERE pixels IS NOT NULL AND expires_at<=clock_timestamp() ORDER BY expires_at,id LIMIT 100 FOR UPDATE SKIP LOCKED) UPDATE agent_visual_previews p SET pixels=NULL FROM expired e WHERE p.id=e.id`)
	if e != nil {
		return 0, av.ErrUnavailable
	}
	return r.RowsAffected(), nil
}
func visualError(e error) error {
	switch {
	case e == nil:
		return nil
	case errors.Is(e, context.Canceled), errors.Is(e, context.DeadlineExceeded):
		return e
	case errors.Is(e, pgx.ErrNoRows), errors.Is(e, identity.ErrUnauthorized), errors.Is(e, mb.ErrDenied):
		return av.ErrDenied
	case errors.Is(e, mb.ErrBudget):
		return av.ErrBudget
	case errors.Is(e, mb.ErrConflict):
		return av.ErrConflict
	default:
		var pe *pgconn.PgError
		if errors.As(e, &pe) {
			return fmt.Errorf("%w: %s (%s)", av.ErrUnavailable, pe.Message, pe.Code)
		}
		return av.ErrUnavailable
	}
}
func (g *agentVisualGateway) ready(ctx context.Context) bool {
	return g != nil && g.store != nil && g.store.pool != nil && g.options.Adapter != nil && g.options.Gate != nil && g.options.Gate.InferenceEnabled(ctx) && g.options.PriceVersion != "" && g.options.Region == "APAC"
}
func (s *Store) RegisterAgentVisualPrice(ctx context.Context, p mb.LivePrice) error {
	tx, e := s.beginEgress(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(context.Background())
	var now time.Time
	if e = tx.QueryRow(ctx, `SELECT clock_timestamp()`).Scan(&now); e != nil {
		return e
	}
	if mb.ValidateVisualPrice(p, now) != nil {
		return mb.ErrInvalid
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_local_price_versions(version,provider_id,model_id,model_version,wire_contract,region,retention,currency,input_rate,output_rate,input_ceiling,output_ceiling,evidence,expires_at,billing_kind,request_ceiling,call_micros,snapshot_source_url,snapshot_sha256,snapshot_document_updated_at,snapshot_observed_at,snapshot_expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20,$21,$22) ON CONFLICT DO NOTHING`, p.Base.Version, p.Base.Destination.Provider, p.Base.Destination.Model, p.Base.Destination.Version, p.Base.Destination.WireContract, p.Base.Region, p.Base.Retention, p.Base.Currency, p.Base.InputMicrosPerToken, p.Base.OutputMicrosPerToken, p.Base.InputTokenCeiling, p.Base.OutputTokenCeiling, p.Base.Evidence, p.Base.ExpiresAt, p.Kind, p.RequestCeiling, p.CallMicros, p.Snapshot.SourceURL, p.Snapshot.ArtifactSHA256, p.Snapshot.DocumentUpdatedAt, p.Snapshot.ObservedAt, p.Snapshot.ExpiresAt)
	if e != nil {
		return e
	}
	stored, e := readLivePrice(ctx, tx, p.Base.Version)
	if e != nil {
		return e
	}
	if !equalLivePrice(stored, p) {
		return mb.ErrConflict
	}
	return tx.Commit(ctx)
}

type visualCurrent struct {
	agent, session, context, source, authority string
	generation                                 int64
	now, expires                               time.Time
}

func (g *agentVisualGateway) begin(ctx context.Context, a av.Access) (pgx.Tx, error) {
	if g == nil || g.store == nil || a.AccountType != "person" || !av.UUID(a.AccountID) || a.SessionDigest == ([32]byte{}) {
		return nil, av.ErrDenied
	}
	tx, e := g.store.beginHumanMomentTx(ctx, a.SessionDigest, identity.Actor{ID: a.AccountID, AccountType: a.AccountType})
	return tx, visualError(e)
}
func (g *agentVisualGateway) current(ctx context.Context, tx pgx.Tx, a av.Access, in av.PreviewInput) (c visualCurrent, e error) {
	var account, agent, profile string
	e = tx.QueryRow(ctx, `SELECT a.xmin::text,ag.id::text,ag.xmin::text,ap.xmin::text FROM accounts a JOIN agents ag ON ag.principal_account_id=a.id AND ag.agent_type='personal' AND ag.status='active' JOIN agent_profiles ap ON ap.agent_id=ag.id AND ap.owner_id=a.id AND ap.owner_type='PERSON' WHERE a.id=$1 AND a.account_type='person' AND a.status='active' FOR SHARE OF a,ag,ap`, a.AccountID).Scan(&account, &c.agent, &agent, &profile)
	if e != nil {
		return c, visualError(e)
	}
	taskToken := ""
	if in.TaskID != "" {
		var updated time.Time
		var taskContext string
		e = tx.QueryRow(ctx, `SELECT xmin::text,context_id::text,updated_at FROM agent_tasks WHERE id=$1 AND owner_account_id=$2 AND acting_user_account_id=$2 AND principal_type='person' AND context_type=$3 AND status='ACTIVE' FOR SHARE`, in.TaskID, a.AccountID, in.SourceContextType).Scan(&taskToken, &taskContext, &updated)
		if e != nil || in.ExpectedTaskUpdatedAt == nil || !updated.Equal(*in.ExpectedTaskUpdatedAt) {
			return c, av.ErrDenied
		}
		c.context = taskContext
	}
	var contextToken string
	if in.SourceContextType == "CITY" {
		e = tx.QueryRow(ctx, `SELECT cx.id::text,cx.xmin::text||':'||cc.xmin::text||':'||ci.xmin::text FROM contexts cx JOIN city_contexts cc ON cc.city_id=cx.city_id AND cc.status='active' JOIN cities ci ON ci.id=cc.city_id AND ci.publication_status='published' AND (ci.expires_at IS NULL OR ci.expires_at>clock_timestamp()) WHERE cx.context_type='CITY' AND (cx.id::text=$1 OR cx.city_id=$1) FOR SHARE OF cx,cc,ci`, in.SourceContextID).Scan(&c.context, &contextToken)
	} else {
		e = tx.QueryRow(ctx, `SELECT cx.id::text,cx.xmin::text||':'||pc.xmin::text||':'||pc.relation FROM contexts cx JOIN person_contexts pc ON pc.context_id=cx.id AND pc.person_account_id=$2 AND pc.visibility='private' AND pc.relation IN('interest','current','affiliation') WHERE cx.id::text=$1 AND cx.context_type='ONLINE' ORDER BY pc.relation LIMIT 1 FOR SHARE OF cx,pc`, in.SourceContextID, a.AccountID).Scan(&c.context, &contextToken)
	}
	if e != nil {
		return c, visualError(e)
	}
	if in.TaskID != "" {
		var valid bool
		e = tx.QueryRow(ctx, `SELECT context_id=$2::uuid FROM agent_tasks WHERE id=$1`, in.TaskID, c.context).Scan(&valid)
		if e != nil || !valid {
			return c, av.ErrDenied
		}
	}
	c.source = liveDigestBytes("birdtie.visual.source.v1", []byte(in.SourceContextType+":"+c.context+":"+contextToken+":"+taskToken))
	c.authority = liveDigestBytes("birdtie.visual.authority.v1", []byte(a.AccountID+":"+account+":"+c.agent+":"+agent+":"+profile))
	e = tx.QueryRow(ctx, `SELECT id::text,authorization_generation,clock_timestamp(),LEAST(expires_at,idle_expires_at) FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone')`, a.SessionDigest[:], a.AccountID, g.store.devPhoneEnabled).Scan(&c.session, &c.generation, &c.now, &c.expires)
	return c, visualError(e)
}
func (g *agentVisualGateway) finish(ctx context.Context, tx pgx.Tx, a av.Access, c visualCurrent, expiry ...time.Time) error {
	var session string
	var generation int64
	e := tx.QueryRow(ctx, `SELECT id::text,authorization_generation FROM sessions WHERE token_sha256=$1 AND account_id=$2 AND revoked_at IS NULL AND expires_at>clock_timestamp() AND idle_expires_at>clock_timestamp() AND ($3::boolean OR authentication_method<>'dev_phone') FOR SHARE`, a.SessionDigest[:], a.AccountID, g.store.devPhoneEnabled).Scan(&session, &generation)
	if e != nil || session != c.session || generation != c.generation {
		return av.ErrDenied
	}
	if e = egressFinish(ctx, tx, session, nil, expiry...); e != nil {
		return visualError(e)
	}
	return nil
}

type visualStored struct {
	preview                                                                           av.Preview
	input                                                                             av.PreviewInput
	pixels                                                                            []byte
	owner, agent, session, root, source, authority, inputDigest, requestDigest, state string
	generation                                                                        int64
	pixelsPresent                                                                     bool
}

func visualRead(ctx context.Context, tx pgx.Tx, id, owner string, lock bool) (v visualStored, e error) {
	return visualReadData(ctx, tx, id, owner, lock, true)
}
func visualMeta(ctx context.Context, tx pgx.Tx, id, owner string) (visualStored, error) {
	return visualReadData(ctx, tx, id, owner, false, false)
}
func visualReadData(ctx context.Context, tx pgx.Tx, id, owner string, lock, pixels bool) (v visualStored, e error) {
	payload := "NULL::bytea"
	if pixels {
		payload = "pixels"
	}
	q := `SELECT owner_id::text,agent_id::text,session_id::text,session_generation,root_trace_id::text,source_token,authority_token,input_digest,request_digest,state,input_json,preview_json,` + payload + `,pixels IS NOT NULL AND octet_length(pixels)=(preview_json->'pixel'->>'byteSize')::integer FROM agent_visual_previews WHERE id=$1 AND owner_id=$2`
	if lock {
		q += ` FOR UPDATE`
	}
	var input, preview []byte
	e = tx.QueryRow(ctx, q, id, owner).Scan(&v.owner, &v.agent, &v.session, &v.generation, &v.root, &v.source, &v.authority, &v.inputDigest, &v.requestDigest, &v.state, &input, &preview, &v.pixels, &v.pixelsPresent)
	if e != nil {
		return v, visualError(e)
	}
	if json.Unmarshal(input, &v.input) != nil || json.Unmarshal(preview, &v.preview) != nil {
		return v, av.ErrUnavailable
	}
	return v, nil
}
func (g *agentVisualGateway) validateStored(ctx context.Context, tx pgx.Tx, a av.Access, v visualStored, dispatch bool) (visualCurrent, error) {
	c, e := g.current(ctx, tx, a, v.input)
	if e != nil {
		return c, e
	}
	if c.agent != v.agent || c.source != v.source || c.authority != v.authority {
		return c, av.ErrDenied
	}
	if dispatch && (c.session != v.session || c.generation != v.generation || !v.preview.ExpiresAt.After(c.now) || !g.ready(ctx) || g.options.PriceVersion != v.preview.Budget.PriceVersion) {
		return c, av.ErrDenied
	}
	return c, nil
}
func visualWire(g *agentVisualGateway, v visualStored) (modelgateway.PreparedVisualWire, error) {
	return g.options.Adapter.Prepare(modelgateway.VisualRequest{UserText: v.input.UserText, Pixels: v.pixels, MIME: v.preview.Pixel.MIME, SHA256: v.preview.Pixel.SHA256, Width: v.preview.Pixel.Width, Height: v.preview.Pixel.Height, DeadlineAt: v.preview.ExpiresAt, MaxOutputTokens: 768})
}
func (g *agentVisualGateway) Preview(ctx context.Context, a av.Access, in av.PreviewInput) (out av.Preview, e error) {
	if av.ValidateInput(in) != nil {
		return out, av.ErrInvalid
	}
	if !g.ready(ctx) {
		return out, av.ErrUnavailable
	}
	original, e := json.Marshal(in)
	if e != nil {
		return out, av.ErrInvalid
	}
	inputDigest := av.Digest(original)
	clear(original)
	pixel, pixels, e := av.DerivePixels(ctx, in.Image)
	if e != nil {
		return out, e
	}
	defer clear(pixels)
	tx, e := g.begin(ctx, a)
	if e != nil {
		return out, e
	}
	defer tx.Rollback(context.Background())
	c, e := g.current(ctx, tx, a, in)
	if e != nil {
		return out, e
	}
	// Serialize draft operation IDs independently of budget reservation IDs.
	if _, e = tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended('birdtie.visual.preview:'||$1,0))`, in.OperationID); e != nil {
		return out, av.ErrUnavailable
	}
	var existing string
	e = tx.QueryRow(ctx, `SELECT id::text FROM agent_visual_previews WHERE operation_id=$1 AND owner_id=$2`, in.OperationID, a.AccountID).Scan(&existing)
	if e == nil {
		v, e := visualRead(ctx, tx, existing, a.AccountID, false)
		if e != nil {
			return out, e
		}
		if v.inputDigest != inputDigest {
			return out, av.ErrConflict
		}
		cur, e := g.validateStored(ctx, tx, a, v, true)
		if e != nil {
			return out, e
		}
		if e = g.finish(ctx, tx, a, cur, v.preview.ExpiresAt); e != nil {
			return out, e
		}
		if e = tx.Commit(ctx); e != nil {
			return out, av.ErrUnavailable
		}
		return v.preview, nil
	}
	if !errors.Is(e, pgx.ErrNoRows) {
		return out, av.ErrUnavailable
	}
	price, e := readLivePrice(ctx, tx, g.options.PriceVersion)
	if e != nil || mb.ValidateVisualPrice(price, c.now) != nil {
		return out, av.ErrUnavailable
	}
	upper, _ := mb.BoundVisual(price, c.now)
	expiry := c.now.Add(110 * time.Second).UTC().Truncate(time.Microsecond)
	if price.Base.ExpiresAt.Before(expiry) {
		expiry = price.Base.ExpiresAt.UTC()
	}
	if c.expires.Before(expiry) {
		expiry = c.expires.UTC().Truncate(time.Microsecond)
	}
	if !expiry.After(c.now.Add(time.Second)) {
		return out, av.ErrDenied
	}
	var id, root, pixelID string
	if e = tx.QueryRow(ctx, `SELECT gen_random_uuid()::text,gen_random_uuid()::text,gen_random_uuid()::text`).Scan(&id, &root, &pixelID); e != nil {
		return out, av.ErrUnavailable
	}
	pixel.ID = pixelID
	pixel.PreviewContentPath = "/v1/me/agent-visual/previews/" + id + "/pixels"
	out = av.Preview{SchemaVersion: 1, PreviewID: id, Version: 1, Status: "PREVIEW", ClientDraftID: in.ClientDraftID, ClientDraftRevision: in.ClientDraftRevision, UserText: in.UserText, Purpose: av.Purpose, EgressPurpose: av.EgressPurpose, Pixel: pixel, Destination: av.Destination{Provider: price.Base.Destination.Provider, Model: price.Base.Destination.Model, WireContract: price.Base.Destination.WireContract, Region: g.options.Region, Retention: "UNKNOWN"}, Budget: av.Budget{Currency: "CNY", PriceVersion: price.Base.Version, InputTokenUpper: upper.InputTokens, OutputTokenUpper: upper.OutputTokens, CostMicrosUpper: upper.CostMicros, PriceExpiresAt: price.Base.ExpiresAt.UTC()}, CreatedAt: c.now.UTC(), ExpiresAt: expiry}
	in.Image.ContentBase64 = ""
	v := visualStored{preview: out, input: in, pixels: pixels}
	wire, e := visualWire(g, v)
	if e != nil {
		return av.Preview{}, visualError(e)
	}
	out.PayloadDigest = wire.WireDigest()
	meta, _ := json.Marshal(struct {
		Input, Payload, Source, Authority, Price string
		Expiry                                   time.Time
	}{inputDigest, out.PayloadDigest, c.source, c.authority, price.Base.Version, expiry})
	requestDigest := liveDigestBytes("birdtie.visual.approval.v1", meta)
	if e = egressOwnerLock(ctx, tx, a.AccountID); e != nil {
		return av.Preview{}, visualError(e)
	}
	// A new preview retires earlier unconsumed pixels for this owner. Receipts
	// and budget metadata remain; an old exact approval cannot revive bytes.
	if _, e = tx.Exec(ctx, `UPDATE agent_visual_previews SET pixels=NULL WHERE owner_id=$1 AND pixels IS NOT NULL AND (state='PREVIEW' OR expires_at<=clock_timestamp())`, a.AccountID); e != nil {
		return av.Preview{}, av.ErrUnavailable
	}
	for _, scope := range []string{"TENANT_PERSON", "SUBJECT_PERSON"} {
		r, e := scanEgressBudget(tx.QueryRow(ctx, `SELECT `+egressBudgetColumns+` FROM model_budget_accounts WHERE owner_id=$1 AND scope=$2 AND currency='CNY' FOR UPDATE`, a.AccountID, scope), scope, "CNY")
		if e != nil || !mb.Fits(r.limit, r.used, upper) {
			return av.Preview{}, av.ErrBudget
		}
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_budget_roots(root_trace_id,owner_id,agent_id,root_task_id,binding_id,source_token,authority_token,currency,max_requests,max_input,max_output,max_cost,expires_at) VALUES($1,$2,$3,$1,$4,$5,$6,'CNY',1,$7,$8,$9,$10)`, root, a.AccountID, c.agent, id, c.source, c.authority, upper.InputTokens, upper.OutputTokens, upper.CostMicros, expiry)
	if e != nil {
		return av.Preview{}, visualError(e)
	}
	limits := mb.Limits{Requests: 1, InputTokens: upper.InputTokens, OutputTokens: upper.OutputTokens, CostMicros: upper.CostMicros}
	if e = insertEgressTask(ctx, tx, root, a.AccountID, root, id, c.source, c.authority, limits); e != nil {
		return av.Preview{}, visualError(e)
	}
	inputJSON, _ := json.Marshal(in)
	previewJSON, _ := json.Marshal(out)
	_, e = tx.Exec(ctx, `INSERT INTO agent_visual_previews(id,operation_id,owner_id,agent_id,session_id,session_generation,context_id,root_trace_id,source_token,authority_token,input_digest,request_digest,payload_digest,input_json,preview_json,pixels,pixel_sha256,pixel_id,created_at,expires_at) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20)`, id, in.OperationID, a.AccountID, c.agent, c.session, c.generation, c.context, root, c.source, c.authority, inputDigest, requestDigest, out.PayloadDigest, inputJSON, previewJSON, pixels, pixel.SHA256, pixelID, c.now, expiry)
	if e != nil {
		return av.Preview{}, visualError(e)
	}
	_, e = tx.Exec(ctx, `INSERT INTO model_egress_previews(id,root_trace_id,task_id,owner_id,session_id,agent_id,binding_id,source_token,authority_token,request_digest,price_version,max_output_tokens,expires_at,scope,purpose,billing_kind,current_query_digest,egress_payload_digest) VALUES($1,$2,$2,$3,$4,$5,$1,$6,$7,$8,$9,768,$10,$11,$12,'TOKEN',$13,$14)`, id, root, a.AccountID, c.session, c.agent, c.source, c.authority, requestDigest, price.Base.Version, expiry, av.Scope, av.EgressPurpose, inputDigest, out.PayloadDigest)
	if e != nil {
		return av.Preview{}, visualError(e)
	}
	if e = egressAudit(ctx, tx, a.AccountID, root, "", "PREVIEW"); e != nil {
		return av.Preview{}, visualError(e)
	}
	if e = g.finish(ctx, tx, a, c, expiry); e != nil {
		return av.Preview{}, e
	}
	if !g.ready(ctx) {
		return av.Preview{}, av.ErrUnavailable
	}
	if e = tx.Commit(ctx); e != nil {
		return av.Preview{}, av.ErrUnavailable
	}
	return out, nil
}
func (g *agentVisualGateway) Pixels(ctx context.Context, a av.Access, id string) (av.Pixel, []byte, error) {
	if !av.UUID(id) {
		return av.Pixel{}, nil, av.ErrInvalid
	}
	tx, e := g.begin(ctx, a)
	if e != nil {
		return av.Pixel{}, nil, e
	}
	defer tx.Rollback(context.Background())
	v, e := visualRead(ctx, tx, id, a.AccountID, false)
	if e != nil {
		return av.Pixel{}, nil, e
	}
	c, e := g.validateStored(ctx, tx, a, v, true)
	if e != nil {
		return av.Pixel{}, nil, e
	}
	if len(v.pixels) != v.preview.Pixel.ByteSize || av.Digest(v.pixels) != v.preview.Pixel.SHA256 {
		return av.Pixel{}, nil, av.ErrDenied
	}
	if e = g.finish(ctx, tx, a, c, v.preview.ExpiresAt); e != nil {
		return av.Pixel{}, nil, e
	}
	if e = tx.Commit(ctx); e != nil {
		return av.Pixel{}, nil, av.ErrUnavailable
	}
	return v.preview.Pixel, v.pixels, nil
}
