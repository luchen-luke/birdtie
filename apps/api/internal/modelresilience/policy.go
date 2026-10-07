// Package modelresilience adds bounded local model retry decisions. It does
// not own identity, source permission, billing, Run history or tool effects.
package modelresilience

import (
	"errors"
	"time"
)

var (
	ErrInvalid     = errors.New("模型重试配置或请求无效")
	ErrUnavailable = errors.New("模型重试或降级当前不可用")
	ErrDenied      = errors.New("当前来源或许可不允许继续模型请求")
	ErrBudget      = errors.New("模型请求次数或等待预算已耗尽")
	ErrDeadline    = errors.New("模型请求已超过截止时间")
)

const MaxAttempts = 8

// Policy bounds one invocation only. It is not an AIR011 monetary/root-trace
// reservation or an AIR016 persistent Run/Step budget. Copies are immutable.
type Policy struct {
	MaxAttempts         int
	MaxElapsed          time.Duration
	AttemptTimeout      time.Duration
	BaseBackoff         time.Duration
	MaxBackoff          time.Duration
	JitterPermille      int
	SameRouteAttempts   int
	MaxProviderSwitches int
}

func DefaultPolicy() Policy {
	return Policy{4, time.Minute, 20 * time.Second, 250 * time.Millisecond, 5 * time.Second, 200, 2, 1}
}

func ValidatePolicy(p Policy) error {
	if p.MaxAttempts < 1 || p.MaxAttempts > MaxAttempts || p.MaxElapsed <= 0 || p.MaxElapsed > 2*time.Minute ||
		p.AttemptTimeout <= 0 || p.AttemptTimeout > p.MaxElapsed || p.BaseBackoff < time.Millisecond ||
		p.MaxBackoff < p.BaseBackoff || p.MaxBackoff > time.Minute || p.JitterPermille < 0 || p.JitterPermille > 1000 ||
		p.SameRouteAttempts < 1 || p.SameRouteAttempts > p.MaxAttempts || p.MaxProviderSwitches < 0 || p.MaxProviderSwitches >= p.MaxAttempts {
		return ErrInvalid
	}
	return nil
}

// RetryDelay is pure scheduling arithmetic. unit selects a bounded positive
// jitter fraction [0,1000]; a provider hint is a lower bound, never capped down.
func RetryDelay(p Policy, failureNumber int, hint time.Duration, hasHint bool, unit int) (time.Duration, error) {
	if ValidatePolicy(p) != nil || failureNumber < 1 || failureNumber > MaxAttempts || unit < 0 || unit > 1000 || (hasHint && hint < 0) {
		return 0, ErrInvalid
	}
	if hasHint && hint > p.MaxBackoff {
		return 0, ErrBudget
	}
	delay := p.BaseBackoff
	for i := 1; i < failureNumber; i++ {
		if delay > p.MaxBackoff/2 {
			delay = p.MaxBackoff
			break
		}
		delay *= 2
	}
	jitter := delay * time.Duration(p.JitterPermille) / 1000 * time.Duration(unit) / 1000
	if jitter > p.MaxBackoff-delay {
		delay = p.MaxBackoff
	} else {
		delay += jitter
	}
	if hasHint && hint > delay {
		delay = hint
	}
	return delay, nil
}
