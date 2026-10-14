package modelgateway

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type visualTestGate struct{ enabled bool }

func (g *visualTestGate) InferenceEnabled(context.Context) bool { return g != nil && g.enabled }

type visualTestNative struct {
	checks, settles            int
	deniedAfter                int
	releaseError, repeat, fake bool
}

func (n *visualTestNative) CheckCurrent(context.Context, PreparedVisualWire) error {
	n.checks++
	if n.deniedAfter > 0 && n.checks >= n.deniedAfter {
		return ErrUnavailable
	}
	return nil
}
func (n *visualTestNative) ReleaseOnce(ctx context.Context, p PreparedVisualWire, callback func(context.Context) (VisualResult, error)) (VisualResult, error) {
	if n.releaseError {
		return VisualResult{}, errors.New("commit unknown; no release")
	}
	if n.fake {
		return VisualResult{Status: Completed}, nil
	}
	result, e := callback(ctx)
	if n.repeat {
		_, _ = callback(ctx)
	}
	return result, e
}
func (n *visualTestNative) SettleUnknown(context.Context, PreparedVisualWire) error {
	n.settles++
	return nil
}
func TestNativeVisualGatewayNoEgressWithoutGateCurrentAndCommittedRelease(t *testing.T) {
	for _, mode := range []string{"gate off", "current retired", "commit unknown", "fake result", "repeated release", "body retired", "success"} {
		t.Run(mode, func(t *testing.T) {
			calls := 0
			gate := &visualTestGate{true}
			native := &visualTestNative{}
			switch mode {
			case "gate off":
				gate.enabled = false
			case "current retired":
				native.deniedAfter = 1
			case "commit unknown":
				native.releaseError = true
			case "fake result":
				native.fake = true
			case "repeated release":
				native.repeat = true
			case "body retired":
				native.deniedAfter = 5
			}
			adapter, _ := NewTencentVisionAdapter(visualTestConfig(t), visualRoundTripper(func(r *http.Request) (*http.Response, error) {
				calls++
				b := make([]byte, 8)
				for {
					_, e := r.Body.Read(b)
					if e == io.EOF {
						break
					}
					if e != nil {
						return nil, e
					}
				}
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Body: io.NopCloser(strings.NewReader(string(visualProviderResponse(t))))}, nil
			}))
			wire, _ := adapter.Prepare(visualTestRequest(t))
			gateway, _ := NewNativeVisualGateway(gate, adapter, native)
			result, e := gateway.Complete(context.Background(), wire)
			if mode == "success" {
				if e != nil || result.Status != Completed || calls != 1 {
					t.Fatal("authorized result missing")
				}
				return
			}
			if e == nil || result.Report != nil || calls > 1 {
				t.Fatal("unsafe native release")
			}
			if mode == "gate off" || mode == "current retired" || mode == "commit unknown" || mode == "fake result" {
				if calls != 0 {
					t.Fatal("unapproved egress")
				}
			}
			if (mode == "body retired" || mode == "repeated release") && native.settles != 1 {
				t.Fatal("uncertain budget not held")
			}
		})
	}
}
func TestNativeVisualGatewayRejectsTypedNilDependencies(t *testing.T) {
	adapter, _ := NewTencentVisionAdapter(visualTestConfig(t), nil)
	var nilGate *visualTestGate
	var nilNative *visualTestNative
	if _, e := NewNativeVisualGateway(nilGate, adapter, &visualTestNative{}); e == nil {
		t.Fatal("typed nil gate")
	}
	if _, e := NewNativeVisualGateway(&visualTestGate{true}, adapter, nilNative); e == nil {
		t.Fatal("typed nil native")
	}
}
