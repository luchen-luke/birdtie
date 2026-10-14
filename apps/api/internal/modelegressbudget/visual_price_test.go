package modelegressbudget

import (
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"strings"
	"testing"
	"time"
)

func visualPriceFixture() LivePrice {
	now := time.Now().UTC().Truncate(time.Microsecond)
	expiry := now.Add(time.Hour)
	return LivePrice{Base: Price{Version: "visual-test-v1", Destination: VisualDestination(), Region: modelcapability.APAC, Retention: LiveRetentionUnknown, Currency: "CNY", InputMicrosPerToken: 8, OutputMicrosPerToken: 18, InputTokenCeiling: 24576, OutputTokenCeiling: 768, Evidence: LiveTariffEvidence, ExpiresAt: expiry}, Kind: LiveToken, RequestCeiling: 1, Snapshot: LiveSnapshot{SourceURL: LiveModelPriceURL, ArtifactSHA256: strings.Repeat("a", 64), DocumentUpdatedAt: now.Add(-time.Hour), ObservedAt: now, ExpiresAt: expiry}}
}
func TestVisualPriceConservativeBoundAndIndependentTextContracts(t *testing.T) {
	p := visualPriceFixture()
	a, e := BoundVisual(p, time.Now())
	if e != nil || a.CostMicros != 210432 || a.InputTokens != 24576 || a.OutputTokens != 768 {
		t.Fatal(a, e)
	}
	if ValidateLivePrice(p, time.Now()) == nil {
		t.Fatal("visual price borrowed old TEXT contract")
	}
	for _, mutate := range []func(*LivePrice){func(p *LivePrice) { p.Base.InputMicrosPerToken = 7 }, func(p *LivePrice) { p.Base.InputTokenCeiling = 16384 }, func(p *LivePrice) { p.Base.Destination = LiveHY3Destination() }, func(p *LivePrice) {
		p.Snapshot.ExpiresAt = p.Snapshot.ObservedAt.Add(25 * time.Hour)
		p.Base.ExpiresAt = p.Snapshot.ExpiresAt
	}, func(p *LivePrice) { p.Snapshot.SourceURL = "https://example.invalid" }} {
		bad := p
		mutate(&bad)
		if ValidateVisualPrice(bad, time.Now()) == nil {
			t.Fatal("unsafe tariff accepted")
		}
	}
}
