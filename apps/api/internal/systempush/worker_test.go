package systempush

import (
	"context"
	"sync"
	"testing"
	"time"
)

type workerSpy struct {
	mu                               sync.Mutex
	enabled                          bool
	prepared                         bool
	claims, starts, checks, receipts int
	outcome                          Outcome
}

func (s *workerSpy) Enabled() bool { return s.enabled }
func (s *workerSpy) Claim(context.Context, []Kind, int) ([]Claim, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.claims++
	return []Claim{{EventID: "11111111-1111-4111-8111-111111111111", AttemptID: "33333333-3333-4333-8333-333333333333", Kind: FCMToken}}, nil
}
func (s *workerSpy) PrepareSend(context.Context, Claim) (SendRequest, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.starts++
	if !s.prepared {
		return SendRequest{}, ErrUnavailable
	}
	return testSend(FCMToken), nil
}
func (s *workerSpy) CheckSend(context.Context, Claim, SendRequest) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.checks++
	return nil
}
func (s *workerSpy) CompleteSend(_ context.Context, _ Claim, r ProviderResult) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.receipts++
	s.outcome = r.Outcome
	return nil
}

type workerProvider struct{ s *workerSpy }

func (p workerProvider) Prepare(context.Context) error {
	p.s.mu.Lock()
	defer p.s.mu.Unlock()
	p.s.prepared = true
	return nil
}
func (p workerProvider) Send(ctx context.Context, r SendRequest) ProviderResult {
	if r.Check == nil || r.Check(ctx) != nil {
		return ProviderResult{Outcome: Rejected, Code: "SOURCE_UNAVAILABLE"}
	}
	return ProviderResult{Outcome: Unknown, Code: "TRANSPORT_UNKNOWN"}
}
func TestSystemPushWorkerDefaultOffBoundsCredentialBeforeFenceAndUnknown(t *testing.T) {
	off := &workerSpy{}
	out, e := RunOnce(context.Background(), off, nil, RunOptions{})
	if e != nil || !out.Disabled || off.claims != 0 {
		t.Fatal("disabled worker touched store")
	}
	live := &workerSpy{enabled: true}
	out, e = RunOnce(context.Background(), live, Registry{FCMToken: workerProvider{live}}, RunOptions{Limit: 1, Concurrency: 1})
	if e != nil || out.Unknown != 1 || live.starts != 1 || live.checks != 1 || live.receipts != 1 || live.outcome != Unknown {
		t.Fatal("credential/fence/outcome ordering incorrect", e)
	}
	for _, options := range []RunOptions{{Limit: 101}, {Concurrency: 3}, {SendTimeout: time.Second}, {Deadline: 61 * time.Second}} {
		if _, e = RunOnce(context.Background(), live, Registry{FCMToken: workerProvider{live}}, options); e != ErrInvalid {
			t.Fatal("unbounded worker options accepted")
		}
	}
}
