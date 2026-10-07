package bookinganalytics

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestBookingAnalyticsClosedClientReport(t *testing.T) {
	id := "84000000-0000-4000-8000-000000000001"
	in := Input{id, EventType, Outcome, strings.Repeat("a", 64), time.Now().UTC().Add(time.Second)}
	if !in.Valid(id) {
		t.Fatal("legitimate report rejected")
	}
	for name, mutate := range map[string]func(*Input){"confirmed": func(i *Input) { i.EventType = "CONFIRMED_BOOKING" }, "outcome": func(i *Input) { i.Outcome = "PROVIDER_CONFIRMED" }, "url": func(i *Input) { i.EventID = "https://example.org/book" }, "version": func(i *Input) { i.SourceVersion = "unknown" }, "deadline": func(i *Input) { i.ValidUntil = time.Time{} }} {
		t.Run(name, func(t *testing.T) {
			x := in
			mutate(&x)
			if x.Valid(id) {
				t.Fatal("not a closed client report")
			}
		})
	}
	r := Report(id, id, time.Now())
	body, _ := json.Marshal(r)
	if r.ConfirmedCapability != "UNAVAILABLE" || r.ConfirmedBooking != "UNKNOWN" || strings.Contains(string(body), "providerReceipt") {
		t.Fatal("client report became verified transaction")
	}
}
