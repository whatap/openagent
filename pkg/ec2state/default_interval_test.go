package ec2state

import (
	"strings"
	"testing"
	"time"
)

func TestForwarderDefaultsResendIntervalWithoutBlockingObservations(t *testing.T) {
	for _, tc := range []struct {
		name     string
		value    string
		omit     bool
		interval time.Duration
	}{
		{"missing", "", true, 5 * time.Minute},
		{"empty", "", false, 5 * time.Minute},
		{"whitespace", " \t ", false, 5 * time.Minute},
		{"explicit override", "1m", false, time.Minute},
		{"padded override", " 1m ", false, time.Minute},
	} {
		t.Run(tc.name, func(t *testing.T) {
			now := time.Unix(1700000000, 0)
			f := &Forwarder{now: func() time.Time { return now }}
			settings := fixtureSettings()
			settings["ec2_state_resend_interval"] = tc.value
			if tc.omit {
				delete(settings, "ec2_state_resend_interval")
			}
			running := fixtureScrape(strings.NewReplacer("48", "16", "terminated", "running").Replace(validStateBody))
			requireForwardCount(t, f, settings, running, 2)
			requireForwardCount(t, f, settings, running, 0)
			now = now.Add(time.Second)
			terminated := fixtureScrape(validStateBody)
			requireForwardCount(t, f, settings, terminated, 2)
			now = now.Add(tc.interval - time.Millisecond)
			requireForwardCount(t, f, settings, terminated, 0)
			now = now.Add(time.Millisecond)
			requireForwardCount(t, f, settings, terminated, 2)
			requireForwardCount(t, f, settings, terminated, 0)
		})
	}
}
