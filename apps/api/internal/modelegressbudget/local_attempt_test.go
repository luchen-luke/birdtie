package modelegressbudget

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/birdtie/birdtie/apps/api/internal/agentevent"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"testing"
	"time"
)

func TestModelEgressLocalAttemptAbsentPortFailsClosed(t *testing.T) {
	if NewLocalAttemptDriver(nil, nil, nil) != nil {
		t.Fatal("missing native port/harness must not create a dispatcher")
	}
}

func TestModelEgressLocalAttemptCheckpointIsBoundAndExpiresMonotonically(t *testing.T) {
	req := modelgateway.Request{RunID: "original"}
	anchor := time.Now()
	pg := time.Date(2040, 1, 1, 0, 0, 0, 0, time.UTC)
	cp := NewLocalReleaseCheckpoint("op", req, pg, pg.Add(90*time.Millisecond), anchor)
	if !cp.ValidFor("op", req, anchor) || cp.ValidFor("other", req, anchor) || cp.ValidAt(anchor.Add(91*time.Millisecond)) {
		t.Fatal("PG-wallclock skew or operation revived checkpoint")
	}
	changed := req
	changed.RunID = "other"
	if cp.ValidFor("op", changed, anchor) {
		t.Fatal("checkpoint reused on other request")
	}
	if _, e := json.Marshal(cp); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var rebuilt LocalReleaseCheckpoint
	if e := json.Unmarshal([]byte(`{"validUntil":"2099-01-01T00:00:00Z"}`), &rebuilt); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	if rebuilt.ValidAt(anchor) {
		t.Fatal("JSON checkpoint restored")
	}
}

type localTestPort struct {
	LocalAttemptPort
	reserveErr, beginErr    error
	state                   string
	reserves, begins, reads int
	onBegin                 func()
}

func (p *localTestPort) ReserveOwnModelAttempt(_ context.Context, _ agentevent.Access, in ReserveInput, _ *agentfeature.Controller) (Reservation, error) {
	p.reserves++
	return Reservation{OperationID: in.OperationID, PreviewID: in.PreviewID, RootTraceID: in.RootTraceID, TaskID: in.TaskID, State: p.state}, p.reserveErr
}
func (p *localTestPort) BeginOwnLocalModelAttempt(context.Context, agentevent.Access, string, *agentfeature.Controller) (modelgateway.Request, error) {
	p.begins++
	if p.onBegin != nil {
		p.onBegin()
	}
	return modelgateway.Request{}, p.beginErr
}

func TestModelEgressLocalAttemptBufferCannotChangeOperationRequestOrBytes(t *testing.T) {
	req := modelgateway.Request{RunID: "original", Messages: []modelgateway.Message{{Role: "user", Content: "本人原查询"}}}
	b := localResultBuffer("original-operation", req, []byte(`{"text":"private"}`))
	wire, e := b.EncodedFor("original-operation", req)
	if e != nil {
		t.Fatal(e)
	}
	wire[0] = '!'
	again, e := b.EncodedFor("original-operation", req)
	if e != nil || again[0] != '{' {
		t.Fatal("returned bytes changed retained buffer")
	}
	changed := req
	changed.RunID = "other"
	if _, e = b.EncodedFor("original-operation", changed); !errors.Is(e, ErrDenied) {
		t.Fatal("request changed", e)
	}
	if _, e = b.EncodedFor("other-operation", req); !errors.Is(e, ErrDenied) {
		t.Fatal("operation changed", e)
	}
	if _, e := json.Marshal(b); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var fake LocalResultBuffer
	if e = json.Unmarshal([]byte(`{"operation":"original-operation"}`), &fake); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	if _, e = fake.EncodedFor("original-operation", req); !errors.Is(e, ErrDenied) {
		t.Fatal(e)
	}
}
func TestModelEgressLocalAttemptTypedNilAndCapturedGateABA(t *testing.T) {
	a := &localTestAdapter{}
	gate := localTestGate(t, true)
	var typedNil *localTestPort
	if NewLocalAttemptDriver(typedNil, a, gate) != nil {
		t.Fatal("typed nil port accepted")
	}
	p := &localTestPort{state: "RESERVED"}
	p.onBegin = func() {
		if e := gate.Disable(agentfeature.Enrichment); e != nil {
			t.Fatal(e)
		}
		cfgBytes, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": true, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
		cfg, e := agentfeature.ParseConfig(cfgBytes)
		if e != nil {
			t.Fatal(e)
		}
		if e = gate.Replace(gate.Revision(), cfg); e != nil {
			t.Fatal(e)
		}
	}
	d := NewLocalAttemptDriver(p, a, gate)
	o, e := d.Once(context.Background(), agentevent.Access{}, ReserveInput{OperationID: "original", PreviewID: "p", RootTraceID: "r", TaskID: "t"})
	if !errors.Is(e, ErrUnavailable) || a.calls != 0 || len(o.EncodedResult) != 0 {
		t.Fatal("old ticket revived after OFF/ON", e)
	}
}
func (p *localTestPort) ReadOwnLocalModelAttempt(_ context.Context, _ agentevent.Access, id string) (AttemptControl, error) {
	p.reads++
	return AttemptControl{OperationID: id, State: p.state, ExecutionStatus: "UNAVAILABLE", ModelAccess: "UNAVAILABLE"}, nil
}

type localTestAdapter struct{ calls int }

func (*localTestAdapter) Descriptor() modelgateway.ProviderDescriptor {
	return modelgateway.ProviderDescriptor{ProviderID: "fake", ModelID: "local", ModelVersion: "v1", Mode: modelgateway.OfflineContract}
}
func (p *localTestAdapter) Complete(context.Context, modelgateway.ProviderRequest) ([]byte, error) {
	p.calls++
	return nil, errors.New("must not be called")
}
func localTestGate(t *testing.T, on bool) *agentfeature.Controller {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"schemaVersion": agentfeature.SchemaVersion, "flags": map[string]bool{"agent_enrichment": on, "agent_memory": false, "agent_attention_policy": false, "agent_social_policy": false, "life_map": false}, "pilot": map[string]any{"memory": "basic", "inference": "conservative", "autonomousAction": false, "sensitiveInference": false}})
	cfg, e := agentfeature.ParseConfig(b)
	if e != nil {
		t.Fatal(e)
	}
	c, e := agentfeature.NewController(cfg)
	if e != nil {
		t.Fatal(e)
	}
	return c
}
func TestModelEgressLocalAttemptStopsAtUnknownCommitAndRecoveryNeverDispatches(t *testing.T) {
	for _, which := range []string{"reserveUnknown", "beginUnknown", "IN_FLIGHT", "UNKNOWN", "SETTLED", "CANCELLED_BEFORE_SEND", "off"} {
		t.Run(which, func(t *testing.T) {
			p := &localTestPort{state: "RESERVED", beginErr: ErrUnavailable}
			a := &localTestAdapter{}
			if which == "reserveUnknown" {
				p.reserveErr = ErrUnavailable
			}
			if which != "reserveUnknown" && which != "beginUnknown" && which != "off" {
				p.state = which
			}
			d := NewLocalAttemptDriver(p, a, localTestGate(t, which != "off"))
			in := ReserveInput{OperationID: "original-op", PreviewID: "preview", RootTraceID: "root", TaskID: "task"}
			out, e := d.Once(context.Background(), agentevent.Access{}, in)
			if e == nil || len(out.EncodedResult) != 0 || a.calls != 0 {
				t.Fatal("unknown/off/recovered operation dispatched", e, a.calls)
			}
			if which == "off" && p.reserves != 0 {
				t.Fatal("OFF reached native mutation")
			}
			if which == "reserveUnknown" && p.begins != 0 {
				t.Fatal("unknown Reserve called Begin")
			}
			r, e := d.Recover(context.Background(), agentevent.Access{}, in.OperationID)
			if e != nil || r.OperationID != in.OperationID || a.calls != 0 {
				t.Fatal("recovery sent request", e)
			}
		})
	}
}
func TestModelEgressLocalAttemptResultIsNotTransferableAuthority(t *testing.T) {
	if _, e := json.Marshal(LocalAttemptOutcome{EncodedResult: []byte(`{"answer":"private"}`)}); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
	var out LocalAttemptOutcome
	if e := json.Unmarshal([]byte(`{"control":{"state":"SETTLED"}}`), &out); !errors.Is(e, ErrServerOnly) {
		t.Fatal(e)
	}
}

func (*localTestAdapter) LocalDestination() LocalAttemptDestination {
	return LocalAttemptDestination{Key: modelcapability.Key{Provider: "fake", Model: "local", Version: "v1", WireContract: "offline.v1"}, Region: modelcapability.UK, Retention: Retention}
}

type localChangingAdapter struct {
	localTestAdapter
	descriptors int
	changeAt    int
}

func (a *localChangingAdapter) Descriptor() modelgateway.ProviderDescriptor {
	a.descriptors++
	d := a.localTestAdapter.Descriptor()
	if a.descriptors == a.changeAt {
		d.ModelID = "changed"
	}
	return d
}
func TestModelEgressLocalAttemptConstructionDescriptorABAFailsClosed(t *testing.T) {
	a := &localChangingAdapter{changeAt: 2}
	if NewLocalAttemptDriver(&localTestPort{}, a, localTestGate(t, true)) != nil || a.calls != 0 {
		t.Fatal("construction captured different descriptors")
	}
	if a.Descriptor().ModelID != "local" {
		t.Fatal("fixture did not restore declaration")
	}
}
func TestModelEgressLocalAttemptWrapperChecksExpiryAfterDescriptorWait(t *testing.T) {
	a := &localTestAdapter{}
	captured := &capturedLocalAdapter{original: a, descriptor: a.Descriptor(), destination: a.LocalDestination(), beforeCall: func() error { return ErrDenied }}
	_, e := captured.Complete(context.Background(), modelgateway.ProviderRequest{})
	if !errors.Is(e, ErrDenied) || a.calls != 0 {
		t.Fatal("last original checkpoint was not checked", e)
	}
}

type localNilAdapter map[string]string

func (localNilAdapter) Descriptor() modelgateway.ProviderDescriptor {
	panic("nil adapter descriptor called")
}
func (localNilAdapter) LocalDestination() LocalAttemptDestination {
	panic("nil adapter destination called")
}
func (localNilAdapter) Complete(context.Context, modelgateway.ProviderRequest) ([]byte, error) {
	panic("nil adapter called")
}
func TestModelEgressLocalAttemptNamedNilAdapterDeniedBeforeDescriptor(t *testing.T) {
	var a localNilAdapter
	if NewLocalAttemptDriver(&localTestPort{}, a, localTestGate(t, true)) != nil {
		t.Fatal("typed nil map adapter accepted")
	}
}
