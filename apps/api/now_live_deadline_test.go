package main

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentworkspace"
	"github.com/birdtie/birdtie/apps/api/internal/identity"
)

const deadlineTestOwner = "13000000-0000-4000-8000-000000000001"

type deadlineIdentityFunc func(context.Context, [32]byte) (identity.Actor, error)

func (f deadlineIdentityFunc) Authenticate(ctx context.Context, digest [32]byte) (identity.Actor, error) {
	return f(ctx, digest)
}

type deadlineAnswerSpy struct {
	calls atomic.Int32
	ctx   context.Context
}

func (*deadlineAnswerSpy) Eligible(ctx context.Context, _ [32]byte, _ agentworkspace.Task) bool {
	return ctx.Err() == nil
}

func (s *deadlineAnswerSpy) Execute(ctx context.Context, _ [32]byte, _ agentworkspace.Task) (agentworkspace.LiveReply, error) {
	s.calls.Add(1)
	s.ctx = ctx
	// Mirror the existing native driver's provider window. The caller's
	// earlier deadline must remain authoritative and there is no retry.
	providerCtx, cancel := context.WithTimeout(ctx, 28*time.Second)
	defer cancel()
	<-providerCtx.Done()
	return nil, providerCtx.Err()
}

func deadlineRequest(t *testing.T, method, path string) *http.Request {
	t.Helper()
	token, _, err := identity.NewToken()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(method, path, nil)
	r.Header.Set("Authorization", "Bearer "+token)
	return r
}

func deadlineOwnerIdentity(context.Context, [32]byte) (identity.Actor, error) {
	return identity.Actor{ID: deadlineTestOwner, AccountType: "person"}, nil
}

func waitDeadlineContext(ctx context.Context, duration time.Duration) error {
	timer := time.NewTimer(duration)
	defer timer.Stop()
	select {
	case <-timer.C:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}

func TestNowLiveOverallDeadlineRegisteredEntryIncludesAllPreparation(t *testing.T) {
	const window = 400 * time.Millisecond
	answers := &deadlineAnswerSpy{}
	var authDeadline, providerDeadline time.Time
	var handlerCtx context.Context
	var authCalls int
	access := deadlineIdentityFunc(func(ctx context.Context, d [32]byte) (identity.Actor, error) {
		authCalls++
		authDeadline, _ = ctx.Deadline()
		if d == ([32]byte{}) {
			t.Fatal("server identity received an empty session digest")
		}
		if err := waitDeadlineContext(ctx, 80*time.Millisecond); err != nil {
			return identity.Actor{}, err
		}
		return deadlineOwnerIdentity(ctx, d)
	})
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/cities/{cityID}/agent/tasks", func(w http.ResponseWriter, r *http.Request) {
		handlerCtx = r.Context()
		if r.PathValue("cityID") != "aberdeen-gb" {
			t.Fatal("deadline boundary replaced the registered route")
		}
		if err := waitDeadlineContext(r.Context(), 80*time.Millisecond); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		providerDeadline, _ = r.Context().Deadline()
		if _, err := answers.Execute(r.Context(), [32]byte{1}, agentworkspace.Task{}); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("provider did not stop at the overall deadline")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	boundary := nowLiveDeadlineBoundary{ownerID: deadlineTestOwner, access: access, next: mux, timeout: window}
	started := time.Now()
	w := httptest.NewRecorder()
	boundary.ServeHTTP(w, deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks"))
	elapsed := time.Since(started)
	if authCalls != 1 || answers.calls.Load() != 1 || w.Code != http.StatusServiceUnavailable {
		t.Fatal("expected one identity lookup and one failed provider call, without retry")
	}
	if !authDeadline.Equal(providerDeadline) || providerDeadline.Sub(started) > window+20*time.Millisecond {
		t.Fatal("preparation reset the provider deadline")
	}
	if elapsed < window-30*time.Millisecond || elapsed > 2*time.Second {
		t.Fatalf("overall deadline was not enforced: %v", elapsed)
	}
	if !errors.Is(handlerCtx.Err(), context.DeadlineExceeded) || !errors.Is(answers.ctx.Err(), context.DeadlineExceeded) {
		t.Fatal("late handler/provider contexts stayed current")
	}
}

func TestNowLiveOverallDeadlineProductionWindowAndRetirement(t *testing.T) {
	answers := &nowLiveOwnerBoundAnswers{LiveAnswers: &deadlineAnswerSpy{}, ownerID: deadlineTestOwner}
	var received context.Context
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		received = r.Context()
		d, ok := received.Deadline()
		if !ok || time.Until(d) > nowLiveOverallTimeout || time.Until(d) < 29*time.Second {
			t.Fatal("production boundary did not apply the fixed 30 second ceiling")
		}
		w.WriteHeader(http.StatusNoContent)
	})
	h := withNowLiveDeadline(answers, deadlineIdentityFunc(deadlineOwnerIdentity), next)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks"))
	if w.Code != http.StatusNoContent || received == nil || !errors.Is(received.Err(), context.Canceled) {
		t.Fatal("completed request left its bounded context alive")
	}
}

func TestNowLiveOverallDeadlinePreservesEarlierCallerDeadline(t *testing.T) {
	parent, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	parentDeadline, _ := parent.Deadline()
	answers := &deadlineAnswerSpy{}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		d, ok := r.Context().Deadline()
		if !ok || !d.Equal(parentDeadline) {
			t.Fatal("shorter caller deadline was extended")
		}
		_, err := answers.Execute(r.Context(), [32]byte{1}, agentworkspace.Task{})
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("shorter caller deadline did not reach the provider")
		}
		w.WriteHeader(http.StatusServiceUnavailable)
	})
	h := withNowLiveDeadline(&nowLiveOwnerBoundAnswers{LiveAnswers: answers, ownerID: deadlineTestOwner}, deadlineIdentityFunc(deadlineOwnerIdentity), next)
	h.ServeHTTP(httptest.NewRecorder(), deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks").WithContext(parent))
	if answers.calls.Load() != 1 {
		t.Fatal("shorter deadline caused a retry")
	}
}

func TestNowLiveOverallDeadlineCancellationDoesNotStartOrRetryProvider(t *testing.T) {
	for _, stage := range []string{"before_identity", "during_preparation", "during_provider"} {
		t.Run(stage, func(t *testing.T) {
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			answers := &deadlineAnswerSpy{}
			access := deadlineIdentityFunc(func(ctx context.Context, d [32]byte) (identity.Actor, error) {
				if ctx.Err() != nil {
					return identity.Actor{}, ctx.Err()
				}
				return deadlineOwnerIdentity(ctx, d)
			})
			if stage == "before_identity" {
				cancel()
			}
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if stage == "during_preparation" {
					cancel()
				}
				if r.Context().Err() == nil {
					if stage == "during_provider" {
						timer := time.AfterFunc(20*time.Millisecond, cancel)
						defer timer.Stop()
					}
					_, err := answers.Execute(r.Context(), [32]byte{1}, agentworkspace.Task{})
					if !errors.Is(err, context.Canceled) {
						t.Fatal("cancellation did not reach the provider")
					}
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			h := withNowLiveDeadline(&nowLiveOwnerBoundAnswers{LiveAnswers: answers, ownerID: deadlineTestOwner}, access, next)
			h.ServeHTTP(httptest.NewRecorder(), deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks").WithContext(parent))
			want := int32(0)
			if stage == "during_provider" {
				want = 1
			}
			if answers.calls.Load() != want {
				t.Fatal("cancellation started or retried provider work")
			}
		})
	}
}

type deadlineUnreadBody struct{ reads int }

func (b *deadlineUnreadBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}
func (*deadlineUnreadBody) Close() error { return nil }

func TestNowLiveOverallDeadlineScopeLeavesOtherRequestsUnchanged(t *testing.T) {
	for _, name := range []string{"default_nil", "unbound_driver", "get", "delete", "history", "city_agent_other_route", "organization", "empty_organization_header", "guest", "invalid_bearer", "duplicate_bearer", "other_owner", "non_person"} {
		t.Run(name, func(t *testing.T) {
			r := deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks")
			original := r.Context()
			body := &deadlineUnreadBody{}
			r.Body = body
			answers := agentworkspace.LiveAnswers(&nowLiveOwnerBoundAnswers{LiveAnswers: &deadlineAnswerSpy{}, ownerID: deadlineTestOwner})
			actor := identity.Actor{ID: deadlineTestOwner, AccountType: "person"}
			wantAuth := 0
			switch name {
			case "default_nil":
				answers = nil
			case "unbound_driver":
				answers = &deadlineAnswerSpy{}
			case "get":
				r.Method = http.MethodGet
			case "delete":
				r.Method = http.MethodDelete
			case "history":
				r.URL.Path = "/v1/me/agent/tasks/task-id"
			case "city_agent_other_route":
				r.URL.Path = "/v1/cities/aberdeen-gb/agent/tasks/task-id"
			case "organization":
				r.Header.Set("X-Birdtie-Organization-Workspace", "organization-id")
			case "empty_organization_header":
				r.Header["X-Birdtie-Organization-Workspace"] = []string{""}
			case "guest":
				r.Header.Del("Authorization")
			case "invalid_bearer":
				r.Header.Set("Authorization", "invalid")
			case "duplicate_bearer":
				r.Header.Add("Authorization", r.Header.Get("Authorization"))
			case "other_owner":
				actor.ID = "13000000-0000-4000-8000-000000000002"
				wantAuth = 1
			case "non_person":
				actor.AccountType = "organization"
				wantAuth = 1
			}
			authCalls, handlerCalls := 0, 0
			access := deadlineIdentityFunc(func(context.Context, [32]byte) (identity.Actor, error) {
				authCalls++
				if body.reads != 0 {
					t.Fatal("deadline identity lookup read the request body")
				}
				return actor, nil
			})
			next := http.HandlerFunc(func(w http.ResponseWriter, received *http.Request) {
				handlerCalls++
				if received.Context() != original || received.Body != body || body.reads != 0 {
					t.Fatal("non-live request context or body changed")
				}
				w.WriteHeader(http.StatusNoContent)
			})
			withNowLiveDeadline(answers, access, next).ServeHTTP(httptest.NewRecorder(), r)
			if authCalls != wantAuth || handlerCalls != 1 {
				t.Fatal("non-live scope invoked extra handlers or identity work")
			}
		})
	}
}

func TestNowLiveOverallDeadlineIdentityFailureCannotResetWindow(t *testing.T) {
	for _, err := range []error{identity.ErrUnauthorized, errors.New("identity store unavailable")} {
		t.Run(err.Error(), func(t *testing.T) {
			var lookupCtx context.Context
			access := deadlineIdentityFunc(func(ctx context.Context, _ [32]byte) (identity.Actor, error) {
				lookupCtx = ctx
				return identity.Actor{}, err
			})
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Context() != lookupCtx {
					t.Fatal("failed identity lookup reset the deadline")
				}
				if _, ok := r.Context().Deadline(); !ok {
					t.Fatal("unavailable identity admitted unbounded work")
				}
				w.WriteHeader(http.StatusServiceUnavailable)
			})
			answers := &nowLiveOwnerBoundAnswers{LiveAnswers: &deadlineAnswerSpy{}, ownerID: deadlineTestOwner}
			withNowLiveDeadline(answers, access, next).ServeHTTP(httptest.NewRecorder(), deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks"))
		})
	}
}

func TestNowLiveOverallDeadlineOwnerReferenceDoesNotConfigureADriver(t *testing.T) {
	for _, answers := range []agentworkspace.LiveAnswers{(*nowLiveOwnerBoundAnswers)(nil), &nowLiveOwnerBoundAnswers{ownerID: deadlineTestOwner}} {
		called := 0
		next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			called++
			if _, ok := r.Context().Deadline(); ok {
				t.Fatal("owner reference alone configured a live boundary")
			}
		})
		access := deadlineIdentityFunc(func(context.Context, [32]byte) (identity.Actor, error) {
			t.Fatal("owner reference alone authenticated a request")
			return identity.Actor{}, identity.ErrUnauthorized
		})
		withNowLiveDeadline(answers, access, next).ServeHTTP(httptest.NewRecorder(), deadlineRequest(t, http.MethodPost, "/v1/cities/aberdeen-gb/agent/tasks"))
		if called != 1 {
			t.Fatal("original handler was replaced")
		}
	}
}
