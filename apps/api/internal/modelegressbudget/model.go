// Package modelegressbudget owns local native model-scope approval and budget
// accounting. It does not activate providers, bill money or create AgentRuns.
package modelegressbudget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"regexp"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

const Scope = "SELF_TASK_QUERY"
const Purpose = "MODEL_CONTEXT_EGRESS"
const LocalPrice = "LOCAL_SYNTHETIC"
const Retention = "NO_STATE_NO_STORAGE"
const SchemaVersion = "air.model_egress_budget.v1"

var (
	ErrInvalid     = errors.New("模型出口或预算配置无效")
	ErrDenied      = errors.New("当前身份、来源或批准不允许模型请求")
	ErrConflict    = errors.New("模型批准或预算记录已变化")
	ErrBudget      = errors.New("模型请求预算不足")
	ErrUnavailable = errors.New("真实模型出口与收费当前不可用")
	ErrServerOnly  = errors.New("模型预算控制对象仅供服务端使用")
)
var identifier = regexp.MustCompile(`^[a-z][a-z0-9_.-]{1,79}$`)
var currency = regexp.MustCompile(`^[A-Z]{3}$`)

// Price is an immutable, explicitly synthetic local price assumption. It is
// never a live tariff or a provider/retention approval. Missing prices fail.
type Price struct {
	Version              string
	Destination          modelcapability.Key
	Region               modelcapability.Region
	Retention            string
	Currency             string
	InputMicrosPerToken  int64
	OutputMicrosPerToken int64
	InputTokenCeiling    int64
	OutputTokenCeiling   int64
	Evidence             string
	ExpiresAt            time.Time
}

func (Price) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (p *Price) UnmarshalJSON([]byte) error { *p = Price{}; return ErrServerOnly }
func ValidatePrice(p Price, now time.Time) error {
	if !identifier.MatchString(p.Version) || !currency.MatchString(p.Currency) || p.Retention != Retention || p.Evidence != LocalPrice ||
		p.InputMicrosPerToken < 1 || p.InputMicrosPerToken > 1_000_000 || p.OutputMicrosPerToken < 1 || p.OutputMicrosPerToken > 1_000_000 ||
		p.InputTokenCeiling < 1 || p.InputTokenCeiling > 1_000_000 || p.OutputTokenCeiling < 1 || p.OutputTokenCeiling > 4096 ||
		now.IsZero() || p.ExpiresAt.IsZero() || !p.ExpiresAt.After(now) || p.ExpiresAt.UTC().Year() > 9999 || p.ExpiresAt.After(now.Add(30*24*time.Hour)) {
		return ErrInvalid
	}
	for _, v := range []string{p.Destination.Provider, p.Destination.Model, p.Destination.Version, p.Destination.WireContract} {
		if !identifier.MatchString(v) || v == "latest" || v == "default" || v == "auto" {
			return ErrInvalid
		}
	}
	if p.Region != modelcapability.US && p.Region != modelcapability.EU && p.Region != modelcapability.UK && p.Region != modelcapability.APAC {
		return ErrInvalid
	}
	return nil
}

type Limits struct{ Requests, InputTokens, OutputTokens, CostMicros int64 }

func ValidateLimits(l Limits) error {
	if l.Requests < 1 || l.Requests > 1000 || l.InputTokens < 1 || l.InputTokens > 1_000_000_000 || l.OutputTokens < 1 || l.OutputTokens > 1_000_000_000 || l.CostMicros < 1 || l.CostMicros > 1_000_000_000_000 {
		return ErrInvalid
	}
	return nil
}

type Amount struct{ InputTokens, OutputTokens, CostMicros int64 }

func Bound(p Price, maxOutput int) (Amount, error) {
	if maxOutput < 1 || int64(maxOutput) > p.OutputTokenCeiling || p.InputTokenCeiling < 1 || p.InputTokenCeiling > 1_000_000 ||
		p.InputMicrosPerToken < 1 || p.InputMicrosPerToken > 1_000_000 || p.OutputMicrosPerToken < 1 || p.OutputMicrosPerToken > 1_000_000 {
		return Amount{}, ErrInvalid
	}
	a := Amount{InputTokens: p.InputTokenCeiling, OutputTokens: int64(maxOutput)}
	a.CostMicros = a.InputTokens*p.InputMicrosPerToken + a.OutputTokens*p.OutputMicrosPerToken
	if a.CostMicros > 1_000_000_000_000 {
		return Amount{}, ErrInvalid
	}
	return a, nil
}
func Fits(limit, used Limits, a Amount) bool {
	return ValidateLimits(limit) == nil && used.Requests >= 0 && used.InputTokens >= 0 && used.OutputTokens >= 0 && used.CostMicros >= 0 &&
		used.Requests < limit.Requests && a.InputTokens > 0 && a.OutputTokens > 0 && a.CostMicros > 0 &&
		used.InputTokens <= limit.InputTokens-a.InputTokens && used.OutputTokens <= limit.OutputTokens-a.OutputTokens && used.CostMicros <= limit.CostMicros-a.CostMicros
}

type RootInput struct {
	RootTraceID, TaskID, BindingID, Currency string
	Limits                                   Limits
	ExpiresAt                                time.Time
}
type TaskInput struct {
	RootTraceID, TaskID, BindingID string
	Limits                         Limits
}
type PreviewInput struct {
	RootTraceID, TaskID, PriceVersion string
	MaxOutputTokens                   int
	DeadlineAt                        time.Time
}
type Preview struct {
	ID, RootTraceID, TaskID, BindingID, SourceToken, AuthorityToken, RequestDigest, PriceVersion string
	Request                                                                                      modelgateway.Request
	Price                                                                                        Price
	ExpiresAt                                                                                    time.Time
	Status                                                                                       string
}

// Preview may be rendered by the current owner. JSON cannot reconstruct its
// approval; Approve re-reads the actual stored preview and the current source.
func (p *Preview) UnmarshalJSON([]byte) error { *p = Preview{}; return ErrServerOnly }
func (Preview) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }

// HumanPreview is a read-only presentation of the exact locally assembled
// request and its synthetic cost ceiling. Its fields are never consumed as an
// approval. Approve uses only the stored ID plus exact digest and current self.
type HumanPreview struct {
	ID, RequestDigest, Scope, Purpose, PriceVersion, Evidence, Currency string
	Destination                                                         modelcapability.Key
	Region                                                              modelcapability.Region
	Retention                                                           string
	Request                                                             modelgateway.Request
	Upper                                                               Amount
	ExpiresAt                                                           time.Time
	PriceExpiresAt                                                      time.Time
	InputMicrosPerToken, OutputMicrosPerToken                           int64
}

func (p Preview) Display() (HumanPreview, error) {
	a, err := Bound(p.Price, p.Request.Budget.MaxOutputTokens)
	if err != nil || p.ID == "" || p.RequestDigest != Digest(p.Request, p.Price) {
		return HumanPreview{}, ErrInvalid
	}
	r := p.Request
	r.Messages = append([]modelgateway.Message{}, r.Messages...)
	r.ToolAllowlist = append([]string{}, r.ToolAllowlist...)
	r.CapabilitiesRequired = append([]string{}, r.CapabilitiesRequired...)
	return HumanPreview{ID: p.ID, RequestDigest: p.RequestDigest, Scope: Scope, Purpose: Purpose, PriceVersion: p.Price.Version, Evidence: p.Price.Evidence, Currency: p.Price.Currency, Destination: p.Price.Destination, Region: p.Price.Region, Retention: p.Price.Retention, Request: r, Upper: a, ExpiresAt: p.ExpiresAt, PriceExpiresAt: p.Price.ExpiresAt, InputMicrosPerToken: p.Price.InputMicrosPerToken, OutputMicrosPerToken: p.Price.OutputMicrosPerToken}, nil
}

type ReserveInput struct{ OperationID, PreviewID, RootTraceID, TaskID string }
type Reservation struct {
	OperationID, PreviewID, RootTraceID, TaskID, PriceVersion, RequestDigest, State, Currency string
	Upper                                                                                     Amount
	ReportedInput, ReportedOutput                                                             *int64
	ExecutionStatus                                                                           string
	CreatedAt                                                                                 time.Time
}
type BudgetView struct {
	Scope             string
	Limits, Allocated Limits
	Currency          string
}

// LocalUsage is deliberately not a billing receipt. Only complete normalized
// local usage is accepted; UNKNOWN stays conservative with the original bound.
type LocalUsage struct {
	known         bool
	input, output int64
}

func (LocalUsage) MarshalJSON() ([]byte, error)  { return nil, ErrServerOnly }
func (u *LocalUsage) UnmarshalJSON([]byte) error { *u = LocalUsage{}; return ErrServerOnly }
func NewLocalUsage(u modelgateway.Usage) (LocalUsage, error) {
	if u.CostStatus != "UNKNOWN" {
		return LocalUsage{}, ErrInvalid
	}
	if u.Status == "UNKNOWN" && u.InputTokens == nil && u.OutputTokens == nil {
		return LocalUsage{}, nil
	}
	if u.Status != "KNOWN" || u.InputTokens == nil || u.OutputTokens == nil || *u.InputTokens < 0 || *u.InputTokens > 1_000_000 || *u.OutputTokens < 0 || *u.OutputTokens > 4096 {
		return LocalUsage{}, ErrInvalid
	}
	return LocalUsage{known: true, input: *u.InputTokens, output: *u.OutputTokens}, nil
}
func (u LocalUsage) Values() (bool, int64, int64) { return u.known, u.input, u.output }
func Digest(r modelgateway.Request, p Price) string {
	// Use an explicit local wire copy because Price denies authority JSON.
	payload, _ := json.Marshal(struct {
		Request                                            modelgateway.Request
		Version                                            string
		Destination                                        modelcapability.Key
		Region                                             modelcapability.Region
		Retention, Currency                                string
		InputRate, OutputRate, InputCeiling, OutputCeiling int64
	}{r, p.Version, p.Destination, p.Region, p.Retention, p.Currency, p.InputMicrosPerToken, p.OutputMicrosPerToken, p.InputTokenCeiling, p.OutputTokenCeiling})
	h := sha256.Sum256(append([]byte("birdtie.model-egress.self-query.v1\x00"), payload...))
	return hex.EncodeToString(h[:])
}
