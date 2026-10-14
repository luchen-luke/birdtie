package postgres

import (
	"time"

	arp "github.com/birdtie/birdtie/apps/api/internal/agentresultprojection"
	"github.com/birdtie/birdtie/apps/api/internal/foundation"
	pp "github.com/birdtie/birdtie/apps/api/internal/placephoto"
)

// New observation may preserve an unexpired old lease, never renew it. The
// original seal/source comparator continues to check every other field.
func preservePlacePhotoProjectionLeases(old, current arp.Receipt) (arp.Receipt, bool) {
	if len(old.Items) != len(current.Items) || len(old.Places) != len(current.Places) {
		return current, false
	}
	current.Items = append(current.Items[:0:0], current.Items...)
	current.Places = append(current.Places[:0:0], current.Places...)
	for i := range current.Items {
		if current.Items[i].PlacePhoto != nil {
			c := *current.Items[i].PlacePhoto
			current.Items[i].PlacePhoto = &c
		}
		if !pp.PreserveLease(old.Items[i].PlacePhoto, current.Items[i].PlacePhoto, time.Now().UTC()) {
			return current, false
		}
	}
	for i := range current.Places {
		if current.Places[i].PlacePhoto != nil {
			c := *current.Places[i].PlacePhoto
			current.Places[i].PlacePhoto = &c
		}
		if !pp.PreserveLease(old.Places[i].PlacePhoto, current.Places[i].PlacePhoto, time.Now().UTC()) {
			return current, false
		}
	}
	return current, true
}

func photoFreeMachinePlace(place foundation.Place) foundation.Place {
	place.PlacePhoto = nil
	place.PhotoSourceProof = ""
	return place
}
