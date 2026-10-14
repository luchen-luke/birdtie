package modelegressbudget

import (
	"github.com/birdtie/birdtie/apps/api/internal/modelcapability"
	"github.com/birdtie/birdtie/apps/api/internal/modelgateway"
	"time"
)

func VisualDestination() modelcapability.Key {
	return modelcapability.Key{Provider: "tencent_tokenhub", Model: modelgateway.TencentVisionModel, Version: modelgateway.TencentVisionModel, WireContract: modelgateway.TencentVisionWireContract}
}

// Published 7.5/17.5 micros per token are rounded upward to 8/18 for the
// conservative integer hold. A tariff snapshot is never a cash receipt.
func ValidateVisualPrice(p LivePrice, now time.Time) error {
	b, s := p.Base, p.Snapshot
	if now.IsZero() || !identifier.MatchString(b.Version) || b.Version == "latest" || b.Version == "default" || b.Version == "auto" ||
		b.Destination != VisualDestination() || b.Region != modelcapability.APAC || b.Currency != "CNY" || b.Retention != LiveRetentionUnknown || b.Evidence != LiveTariffEvidence ||
		p.Kind != LiveToken || p.RequestCeiling != 1 || p.CallMicros != 0 || b.InputMicrosPerToken != 8 || b.OutputMicrosPerToken != 18 || b.InputTokenCeiling != modelgateway.TencentVisionMaxInputTokens || b.OutputTokenCeiling != 768 ||
		s.SourceURL != LiveModelPriceURL || !liveHash(s.ArtifactSHA256) || !liveTime(s.DocumentUpdatedAt) || !liveTime(s.ObservedAt) || !liveTime(s.ExpiresAt) || !liveTime(b.ExpiresAt) ||
		s.DocumentUpdatedAt.After(s.ObservedAt) || s.ObservedAt.After(now) || !s.ExpiresAt.After(now) || !s.ExpiresAt.After(s.ObservedAt) || s.ExpiresAt.After(s.ObservedAt.Add(LiveSnapshotMaxAge)) || !b.ExpiresAt.Equal(s.ExpiresAt) {
		return ErrInvalid
	}
	return nil
}
func BoundVisual(p LivePrice, now time.Time) (Amount, error) {
	if ValidateVisualPrice(p, now) != nil {
		return Amount{}, ErrInvalid
	}
	return Amount{InputTokens: 24576, OutputTokens: 768, CostMicros: 210432}, nil
}
