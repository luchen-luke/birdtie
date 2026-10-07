package postgres

import (
	"testing"
	"time"
)

func TestFormatActivityScheduleUsesCityLocalTimeAndChinese(t *testing.T) {
	location, err := time.LoadLocation("Europe/London")
	if err != nil {
		t.Fatal(err)
	}
	got := formatActivitySchedule(time.Date(2026, 10, 3, 10, 0, 0, 0, time.UTC).In(location))
	if got != "10月3日（周六）11:00" {
		t.Fatalf("schedule = %q", got)
	}
}
