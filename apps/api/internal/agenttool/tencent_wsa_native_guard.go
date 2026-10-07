package agenttool

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// SearchWithNativeGuard adds the actual native revalidation dependency at the
// wire boundary. It is not a grant or an approval and does not reserve funds.
// before must re-read the original current identity/task/egress/run boundary;
// no state is inferred from the transport configuration or a successful read.
// The ordinary transport Search keeps its original behavior.
func (a *TencentWSAAdapter) SearchWithNativeGuard(ctx context.Context, query string, deadline time.Time, before func(context.Context) error) (TencentWSAResult, error) {
	if a == nil || a.client == nil || a.client.Transport == nil || a.now == nil || ctx == nil || before == nil {
		return TencentWSAResult{}, ErrTencentWSARequest
	}
	if err := ctx.Err(); err != nil {
		return TencentWSAResult{}, err
	}
	if !wsaText(query, 240, false) || strings.TrimSpace(query) != query || !deadline.After(a.now()) || deadline.After(a.now().Add(TencentWSAMaxDeadline)) {
		return TencentWSAResult{}, ErrTencentWSARequest
	}
	body, err := json.Marshal(struct {
		Query string `json:"Query"`
	}{query})
	if err != nil || len(body) > TencentWSAMaxRequestBytes {
		return TencentWSAResult{}, ErrTencentWSARequest
	}
	// Converting to a private immutable string prevents response cleanup from
	// clearing a buffer that a transport is still reading after an early return.
	expected := string(body)
	clear(body)
	copyAdapter, copyClient := *a, *a.client
	copyClient.Transport = &tencentWSANativeGuardTransport{next: a.client.Transport, expected: expected, before: before, now: a.now, deadline: deadline}
	copyAdapter.client = &copyClient
	return copyAdapter.Search(ctx, query, deadline)
}

type tencentWSANativeGuardTransport struct {
	next      http.RoundTripper
	expected  string
	before    func(context.Context) error
	now       func() time.Time
	deadline  time.Time
	attempted atomic.Bool
}

func (*tencentWSANativeGuardTransport) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "tencentWSANativeGuardTransport{redacted}")
}

func wsaNativeGuardCurrent(ctx context.Context, deadline time.Time, now func() time.Time, before func(context.Context) error) error {
	if ctx == nil || now == nil || before == nil {
		return ErrTencentWSARequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !deadline.After(now()) {
		return context.DeadlineExceeded
	}
	// Never expose a callback error containing native IDs, query or secrets.
	if err := before(ctx); err != nil {
		return ErrTencentWSARequest
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if !deadline.After(now()) {
		return context.DeadlineExceeded
	}
	return nil
}

func (t *tencentWSANativeGuardTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t == nil || req == nil || req.URL == nil || req.Body == nil || t.next == nil || t.before == nil || t.now == nil || len(t.expected) == 0 || len(t.expected) > TencentWSAMaxRequestBytes || req.URL.String() != TencentWSAEndpoint || req.Method != http.MethodPost || req.GetBody != nil || !req.Close || req.ProtoMajor != 1 || req.ContentLength != int64(len(t.expected)) || !t.attempted.CompareAndSwap(false, true) {
		return nil, ErrTencentWSARequest
	}
	// Only validate the bounded caller body locally. The actual network body
	// is replaced with a reader that calls the native check on every Read.
	raw, err := io.ReadAll(io.LimitReader(req.Body, TencentWSAMaxRequestBytes+1))
	_ = req.Body.Close()
	matched := err == nil && string(raw) == t.expected
	clear(raw)
	if !matched {
		return nil, ErrTencentWSARequest
	}
	if err := wsaNativeGuardCurrent(req.Context(), t.deadline, t.now, t.before); err != nil {
		return nil, err
	}
	req.Body = &tencentWSANativeGuardBody{ctx: req.Context(), deadline: t.deadline, now: t.now, before: t.before, payload: t.expected}
	// Do not defer Close here: HTTP transports may finish reading Body after
	// returning early. Search's context cancellation makes late Reads fail.
	return t.next.RoundTrip(req)
}

type tencentWSANativeGuardBody struct {
	mu       sync.Mutex
	ctx      context.Context
	deadline time.Time
	now      func() time.Time
	before   func(context.Context) error
	payload  string
	offset   int
	closed   bool
}

func (*tencentWSANativeGuardBody) Format(s fmt.State, _ rune) {
	_, _ = io.WriteString(s, "tencentWSANativeGuardBody{redacted}")
}

func (b *tencentWSANativeGuardBody) Read(dst []byte) (int, error) {
	if b == nil {
		return 0, ErrTencentWSARequest
	}
	b.mu.Lock()
	closed := b.closed
	b.mu.Unlock()
	if closed {
		return 0, io.ErrClosedPipe
	}
	// Do not hold mu while invoking the native callback: a concurrent Close
	// must be able to retire the reader while revalidation waits on native SQL.
	if err := wsaNativeGuardCurrent(b.ctx, b.deadline, b.now, b.before); err != nil {
		return 0, err
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return 0, io.ErrClosedPipe
	}
	if err := b.ctx.Err(); err != nil {
		return 0, err
	}
	if !b.deadline.After(b.now()) {
		return 0, context.DeadlineExceeded
	}
	if len(dst) == 0 {
		return 0, nil
	}
	if b.offset >= len(b.payload) {
		return 0, io.EOF
	}
	n := copy(dst, b.payload[b.offset:])
	b.offset += n
	return n, nil
}

func (b *tencentWSANativeGuardBody) Close() error {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	b.closed = true
	b.payload = ""
	b.offset = 0
	b.mu.Unlock()
	return nil
}
