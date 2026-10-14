package systempush

import (
	"context"
	"sync"
	"time"
)

type Claim struct {
	EventID   string
	AttemptID string
	Kind      Kind
}
type RunnerStore interface {
	Enabled() bool
	Claim(context.Context, []Kind, int) ([]Claim, error)
	PrepareSend(context.Context, Claim) (SendRequest, error)
	CheckSend(context.Context, Claim, SendRequest) error
	CompleteSend(context.Context, Claim, ProviderResult) error
}
type RunOptions struct {
	Limit       int
	Concurrency int
	SendTimeout time.Duration
	Deadline    time.Duration
}
type RunResult struct {
	Disabled  bool `json:"disabled"`
	Claimed   int  `json:"claimed"`
	Accepted  int  `json:"accepted"`
	Retried   int  `json:"retryScheduled"`
	Rejected  int  `json:"rejected"`
	Unknown   int  `json:"unknown"`
	Cancelled int  `json:"cancelled"`
}

func RunOnce(ctx context.Context, store RunnerStore, registry Registry, options RunOptions) (RunResult, error) {
	out := RunResult{}
	if ctx == nil || store == nil {
		return out, ErrUnavailable
	}
	if !store.Enabled() {
		out.Disabled = true
		return out, nil
	}
	if options.Limit == 0 {
		options.Limit = 25
	}
	if options.Concurrency == 0 {
		options.Concurrency = 2
	}
	if options.SendTimeout == 0 {
		options.SendTimeout = 15 * time.Second
	}
	if options.Deadline == 0 {
		options.Deadline = 60 * time.Second
	}
	if options.Limit < 1 || options.Limit > 100 || options.Concurrency < 1 || options.Concurrency > 2 || options.SendTimeout < 10*time.Second || options.SendTimeout > 30*time.Second || options.Deadline < options.SendTimeout || options.Deadline > 60*time.Second {
		return out, ErrInvalid
	}
	kinds := []Kind{}
	for _, k := range []Kind{FCMToken, APNSToken} {
		if registry[k] != nil {
			kinds = append(kinds, k)
		}
	}
	if len(kinds) == 0 {
		return out, ErrConfig
	}
	run, cancel := context.WithTimeout(ctx, options.Deadline)
	defer cancel()
	claims, e := store.Claim(run, kinds, options.Limit)
	if e != nil {
		return out, e
	}
	out.Claimed = len(claims)
	jobs := make(chan Claim)
	var wg sync.WaitGroup
	var mu sync.Mutex
	var failure error
	for n := 0; n < options.Concurrency; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for c := range jobs {
				if run.Err() != nil {
					mu.Lock()
					out.Cancelled++
					mu.Unlock()
					continue
				}
				send, stop := context.WithTimeout(run, options.SendTimeout)
				provider := registry[c.Kind]
				e := ErrUnavailable
				if provider != nil {
					e = provider.Prepare(send)
				}
				var request SendRequest
				if e == nil {
					request, e = store.PrepareSend(send, c)
				}
				result := ProviderResult{Outcome: Rejected, Code: "SOURCE_UNAVAILABLE"}
				if e == nil {
					request.Check = func(ctx context.Context) error { return store.CheckSend(ctx, c, request) }
					if p := registry[c.Kind]; p != nil {
						result = p.Send(send, request)
					} else {
						result = ProviderResult{Outcome: Unknown, Code: "PROVIDER_UNAVAILABLE"}
					}
				}
				stop()
				// Receipt recording has its own bounded context even if the send timed out.
				// A lost receipt leaves SENDING, recovered only to UNKNOWN, never re-sent.
				if e == nil {
					receipt, finish := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
					record := store.CompleteSend(receipt, c, result)
					finish()
					if record != nil {
						mu.Lock()
						failure = ErrUnavailable
						mu.Unlock()
						result = ProviderResult{Outcome: Unknown, Code: "RECEIPT_UNKNOWN"}
					}
				}
				mu.Lock()
				switch {
				case e != nil:
					out.Cancelled++
				case result.Outcome == Accepted:
					out.Accepted++
				case result.Outcome == Retry:
					out.Retried++
				case result.Outcome == Unknown:
					out.Unknown++
				default:
					out.Rejected++
				}
				mu.Unlock()
			}
		}()
	}
	for _, c := range claims {
		jobs <- c
	}
	close(jobs)
	wg.Wait()
	return out, failure
}
