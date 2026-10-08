package modelegressbudget

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/actorref"
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
)

type LiveChargeKind string

const (
	LiveToken LiveChargeKind = "TOKEN"
	LiveCall  LiveChargeKind = "CALL"

	LiveRetentionUnknown = "UNKNOWN"
	LiveTariffEvidence   = "PUBLISHED_TARIFF_SNAPSHOT"
	LiveSnapshotMaxAge   = 24 * time.Hour
	LiveMaxInputTokens   = 1_000_000
	LiveMaxOutputTokens  = 768
	LiveSearchCallMicros = 80_000
	LiveSearchPurpose    = "PUBLIC_SOURCE_RETRIEVAL"
	LiveModelPriceURL    = "https://cloud.tencent.com/document/product/1823/130055"
	LiveSearchPriceURL   = "https://cloud.tencent.com/document/product/1806/121798"
)

// These keys name selected routes and local wire contracts, not immutable
// provider weights, observed serving regions or installed provider grants.
func LiveHY3Destination() modelcapability.Key {
	return modelcapability.Key{Provider: "tencent_tokenhub", Model: "hy3", Version: "hy3", WireContract: "tokenhub.chat-completions.v1"}
}
func LiveDeepSeek0813Destination() modelcapability.Key {
	return modelcapability.Key{Provider: "tencent_tokenhub", Model: modelgateway.TencentTokenHubDeepSeekModel, Version: modelgateway.TencentTokenHubDeepSeekModel, WireContract: modelgateway.TencentLiveWireContract}
}
func LiveWSADestination() modelcapability.Key {
	return modelcapability.Key{Provider: "tencent_wsa", Model: "searchpro", Version: "searchpro", WireContract: "wsa.search-pro.v1"}
}

// LiveSnapshot records a server-selected public tariff artifact. A matching
// URL/hash format does not authenticate that artifact or approve any egress.
// The producer must supply the actual artifact hash and its observation time.
type LiveSnapshot struct {
	SourceURL, ArtifactSHA256 string
	DocumentUpdatedAt         time.Time
	ObservedAt, ExpiresAt     time.Time
}

// LivePrice wraps the original 062 Price without changing its validation or
// digest. APAC is only a selected routing class. Provider retention is UNKNOWN.
// Cached input is conservatively charged at the full published input rate;
// the published 0.25 micros cache rate is not an integer cash receipt here.
type LivePrice struct {
	Base           Price
	Kind           LiveChargeKind
	RequestCeiling int64
	CallMicros     int64
	Snapshot       LiveSnapshot
}

func (LivePrice) MarshalJSON() ([]byte, error) { return nil, ErrServerOnly }
func (p *LivePrice) UnmarshalJSON([]byte) error {
	if p != nil {
		*p = LivePrice{}
	}
	return ErrServerOnly
}

func liveTime(t time.Time) bool {
	return !t.IsZero() && t.UTC().Year() >= 1 && t.UTC().Year() <= 9999 && t.Equal(t.UTC().Truncate(time.Microsecond))
}
func liveHash(v string) bool {
	if len(v) != 64 || v == strings.Repeat("0", 64) {
		return false
	}
	for _, c := range v {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return false
		}
	}
	return true
}
func liveUUID(v string) bool {
	ref, err := actorref.Parse("PERSON", v)
	return err == nil && ref.ID == v && v != "00000000-0000-0000-0000-000000000000"
}

// ValidateLivePrice is arithmetic/provenance validation only. It does not
// establish funding, free quota, retention consent, source ACL or dispatch.
func ValidateLivePrice(p LivePrice, now time.Time) error {
	b, s := p.Base, p.Snapshot
	if now.IsZero() || now.UTC().Year() < 1 || now.UTC().Year() > 9999 || !identifier.MatchString(b.Version) || b.Version == "latest" || b.Version == "default" || b.Version == "auto" ||
		b.Currency != "CNY" || b.Retention != LiveRetentionUnknown || b.Evidence != LiveTariffEvidence || b.Region != modelcapability.APAC || p.RequestCeiling != 1 ||
		!liveHash(s.ArtifactSHA256) || !liveTime(s.DocumentUpdatedAt) || !liveTime(s.ObservedAt) || !liveTime(s.ExpiresAt) || !liveTime(b.ExpiresAt) ||
		s.DocumentUpdatedAt.After(s.ObservedAt) || s.ObservedAt.After(now) || !s.ExpiresAt.After(now) || !s.ExpiresAt.After(s.ObservedAt) ||
		s.ExpiresAt.After(s.ObservedAt.Add(LiveSnapshotMaxAge)) || !b.ExpiresAt.Equal(s.ExpiresAt) {
		return ErrInvalid
	}
	switch p.Kind {
	case LiveToken:
		if !liveTokenTariffShape(p) {
			return ErrInvalid
		}
	case LiveCall:
		if b.Destination != LiveWSADestination() || s.SourceURL != LiveSearchPriceURL || p.CallMicros != LiveSearchCallMicros ||
			b.InputMicrosPerToken != 0 || b.OutputMicrosPerToken != 0 || b.InputTokenCeiling != 0 || b.OutputTokenCeiling != 0 {
			return ErrInvalid
		}
	default:
		return ErrInvalid
	}
	return nil
}

// The HY3 arithmetic helper keeps its original range. Native preparation still
// requires its universal provider bound. The new 0813 route uses one closed
// local tokenizer/template engineering bound, not a provider maximum or an
// authenticated claim about the hosted tokenizer/weight revision.
func liveTokenTariffShape(p LivePrice) bool {
	b := p.Base
	if p.Kind != LiveToken || p.RequestCeiling != 1 || p.CallMicros != 0 || p.Snapshot.SourceURL != LiveModelPriceURL ||
		b.OutputTokenCeiling < 1 || b.OutputTokenCeiling > LiveMaxOutputTokens {
		return false
	}
	switch b.Destination {
	case LiveHY3Destination():
		return b.InputMicrosPerToken == 1 && b.OutputMicrosPerToken == 4 && b.InputTokenCeiling >= 1 && b.InputTokenCeiling <= LiveMaxInputTokens
	case LiveDeepSeek0813Destination():
		return b.InputMicrosPerToken == 9 && b.OutputMicrosPerToken == 27 && b.InputTokenCeiling == modelgateway.TencentDeepSeekMaxInputTokens
	default:
		return false
	}
}

// HasNativeLiveInputBound checks closed route metadata only; the original
// native preparation must additionally match the private exact-wire proof.
func HasNativeLiveInputBound(p LivePrice) bool {
	if !liveTokenTariffShape(p) {
		return false
	}
	return p.Base.Destination == LiveDeepSeek0813Destination() || p.Base.InputTokenCeiling == modelgateway.TencentLiveMaxInputTokens
}

type LiveAmount struct {
	Kind     LiveChargeKind
	Requests int64
	Amount   Amount
}

// BoundLive assumes the caller has independently proved the input ceiling
// against the exact wire payload. A configured ceiling is not a tokenizer or
// a provider input cap. Same-root WSA plus model limits are checked separately.
func BoundLive(p LivePrice, maxOutput int, now time.Time) (LiveAmount, error) {
	if err := ValidateLivePrice(p, now); err != nil {
		return LiveAmount{}, err
	}
	out := LiveAmount{Kind: p.Kind, Requests: 1}
	if p.Kind == LiveCall {
		if maxOutput != 0 {
			return LiveAmount{}, ErrInvalid
		}
		out.Amount.CostMicros = p.CallMicros
		return out, nil
	}
	a, err := Bound(p.Base, maxOutput)
	if err != nil {
		return LiveAmount{}, err
	}
	out.Amount = a
	return out, nil
}

// FitsLive keeps the original Limits semantics. Zero token amounts are valid
// only for the closed single CALL tariff; the original Fits remains unchanged.
func FitsLive(limit, used Limits, a LiveAmount) bool {
	if a.Requests != 1 {
		return false
	}
	switch a.Kind {
	case LiveToken:
		if a.Amount.InputTokens < 1 || a.Amount.InputTokens > LiveMaxInputTokens || a.Amount.OutputTokens < 1 || a.Amount.OutputTokens > LiveMaxOutputTokens ||
			a.Amount.CostMicros != a.Amount.InputTokens+4*a.Amount.OutputTokens {
			return false
		}
		return Fits(limit, used, a.Amount)
	case LiveCall:
		return a.Amount.InputTokens == 0 && a.Amount.OutputTokens == 0 && a.Amount.CostMicros == LiveSearchCallMicros && ValidateLimits(limit) == nil &&
			used.Requests >= 0 && used.Requests < limit.Requests && used.InputTokens >= 0 && used.InputTokens <= limit.InputTokens &&
			used.OutputTokens >= 0 && used.OutputTokens <= limit.OutputTokens && used.CostMicros >= 0 && used.CostMicros <= limit.CostMicros-a.Amount.CostMicros
	default:
		return false
	}
}

// FitsLivePrice binds the amount to one exact closed tariff. It adds no expiry,
// source or egress authority: native callers retain their SQL-clock checks.
// FitsLive remains the original HY3/CALL arithmetic compatibility contract.
func FitsLivePrice(limit, used Limits, p LivePrice, a LiveAmount) bool {
	if a.Requests != 1 || a.Kind != p.Kind || p.Base.Currency != "CNY" {
		return false
	}
	if p.Kind == LiveCall {
		return p.Base.Destination == LiveWSADestination() && p.Snapshot.SourceURL == LiveSearchPriceURL && p.RequestCeiling == 1 &&
			p.CallMicros == LiveSearchCallMicros && p.Base.InputMicrosPerToken == 0 && p.Base.OutputMicrosPerToken == 0 &&
			p.Base.InputTokenCeiling == 0 && p.Base.OutputTokenCeiling == 0 && FitsLive(limit, used, a)
	}
	if !liveTokenTariffShape(p) || a.Amount.OutputTokens < 1 || a.Amount.OutputTokens > p.Base.OutputTokenCeiling {
		return false
	}
	expected, err := Bound(p.Base, int(a.Amount.OutputTokens))
	return err == nil && a.Amount == expected && Fits(limit, used, expected)
}

// LiveDigestInput is an explicit data-only description of the current query
// and exact egress payload. No history, secret, source rows or transferable
// authorization is carried here. Native storage must re-read these facts.
type LiveDigestInput struct {
	Scope, Purpose                                  string
	TaskID, RootTraceID, BindingID                  string
	SourceToken, AuthorityToken                     string
	CurrentQueryEvidenceDigest, EgressPayloadDigest string
	MaxOutputTokens                                 int
	DeadlineAt                                      time.Time
}

// DigestLive deliberately uses a new domain. Digest's original v1 byte shape
// is untouched; a v2 digest is neither an approval nor a native release receipt.
func DigestLive(in LiveDigestInput, p LivePrice, now time.Time) (string, error) {
	if _, err := BoundLive(p, in.MaxOutputTokens, now); err != nil {
		return "", err
	}
	purpose := Purpose
	if p.Kind == LiveCall {
		purpose = LiveSearchPurpose
	}
	if in.Scope != Scope || in.Purpose != purpose || !liveUUID(in.TaskID) || !liveUUID(in.RootTraceID) || !liveUUID(in.BindingID) ||
		!liveHash(in.SourceToken) || !liveHash(in.AuthorityToken) || !liveHash(in.CurrentQueryEvidenceDigest) || !liveHash(in.EgressPayloadDigest) ||
		!liveTime(in.DeadlineAt) || !in.DeadlineAt.After(now) || in.DeadlineAt.After(now.Add(modelgateway.MaxDeadline)) || in.DeadlineAt.After(p.Snapshot.ExpiresAt) {
		return "", ErrInvalid
	}
	// Explicit wire copy: Base Price denies JSON reconstruction and stays v1.
	in.DeadlineAt = in.DeadlineAt.UTC()
	snapshot := p.Snapshot
	snapshot.DocumentUpdatedAt = snapshot.DocumentUpdatedAt.UTC()
	snapshot.ObservedAt = snapshot.ObservedAt.UTC()
	snapshot.ExpiresAt = snapshot.ExpiresAt.UTC()
	payload, err := json.Marshal(struct {
		Input                         LiveDigestInput
		Kind                          LiveChargeKind
		PriceVersion                  string
		Destination                   modelcapability.Key
		Region                        modelcapability.Region
		Retention, Currency, Evidence string
		InputRate, OutputRate         int64
		InputCeiling, OutputCeiling   int64
		RequestCeiling, CallMicros    int64
		Snapshot                      LiveSnapshot
	}{in, p.Kind, p.Base.Version, p.Base.Destination, p.Base.Region, p.Base.Retention, p.Base.Currency, p.Base.Evidence,
		p.Base.InputMicrosPerToken, p.Base.OutputMicrosPerToken, p.Base.InputTokenCeiling, p.Base.OutputTokenCeiling, p.RequestCeiling, p.CallMicros, snapshot})
	if err != nil {
		return "", ErrInvalid
	}
	h := sha256.Sum256(append([]byte("birdtie.model-egress.current-query.v2\x00"), payload...))
	return hex.EncodeToString(h[:]), nil
}

type LiveHoldObservation struct {
	Upper, Held                   LiveAmount
	UsageStatus, CashStatus       string
	ReportedInput, ReportedOutput *int64
}

// HoldLive records bounded token observations without settling cash or
// releasing any original hold. A WSA call has no model token usage. Provider
// errors, free-resource screenshots, cache usage and tariffs are not receipts.
func HoldLive(p LivePrice, maxOutput int, usage modelgateway.Usage, now time.Time) (LiveHoldObservation, error) {
	upper, err := BoundLive(p, maxOutput, now)
	if err != nil {
		return LiveHoldObservation{}, err
	}
	if usage.CostStatus != "UNKNOWN" {
		return LiveHoldObservation{}, ErrInvalid
	}
	out := LiveHoldObservation{Upper: upper, Held: upper, UsageStatus: usage.Status, CashStatus: "UNKNOWN"}
	if usage.Status == "UNKNOWN" && usage.InputTokens == nil && usage.OutputTokens == nil {
		return out, nil
	}
	if p.Kind != LiveToken || usage.Status != "KNOWN" || usage.InputTokens == nil || usage.OutputTokens == nil ||
		*usage.InputTokens < 0 || *usage.InputTokens > upper.Amount.InputTokens || *usage.OutputTokens < 0 || *usage.OutputTokens > upper.Amount.OutputTokens {
		return LiveHoldObservation{}, ErrInvalid
	}
	i, o := *usage.InputTokens, *usage.OutputTokens
	out.ReportedInput, out.ReportedOutput = &i, &o
	return out, nil
}
