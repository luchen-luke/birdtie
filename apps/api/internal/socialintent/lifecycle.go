package socialintent

import "time"

const (
	Draft     = "DRAFT"
	Active    = "ACTIVE"
	Matched   = "MATCHED"
	Converted = "CONVERTED"
	Expired   = "EXPIRED"
	Cancelled = "CANCELLED"
)

// EffectiveStatus is a read-time expiry rule. It does not rely on a scheduler
// and never reopens a cancelled or converted intent.
func EffectiveStatus(status string, expiresAt, now time.Time) string {
	if !expiresAt.After(now) && (status == Draft || status == Active || status == Matched) {
		return Expired
	}
	return status
}

func CanTransition(from, to string) bool {
	switch from {
	case Draft:
		return to == Active || to == Cancelled
	case Active:
		return to == Matched || to == Cancelled
	case Matched:
		return to == Converted || to == Cancelled
	default:
		return false
	}
}
