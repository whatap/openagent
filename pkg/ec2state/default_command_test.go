package ec2state

import (
	"testing"

	"github.com/whatap/golib/lang/pack"
)

func TestForwarderDefaultsToOwnerAllocatedCommand(t *testing.T) {
	for _, tc := range []struct {
		name  string
		value string
		omit  bool
		want  int32
	}{
		{"missing", "", true, 56},
		{"empty", "", false, 56},
		{"whitespace", " \t ", false, 56},
		{"explicit allocated ID", "56", false, 56},
		{"explicit fixture override", "190001", false, 190001},
	} {
		t.Run(tc.name, func(t *testing.T) {
			settings := fixtureSettings()
			settings["ec2_state_param_id"] = tc.value
			if tc.omit {
				delete(settings, "ec2_state_param_id")
			}
			delete(settings, "ec2_state_resend_interval")
			for _, p := range requireForwardCount(t, new(Forwarder), settings, fixtureScrape(validStateBody), 2) {
				decoded := pack.ToPack(pack.ToBytesPack(p)).(*pack.ParamPack)
				if decoded.Id != tc.want || decoded.Response != 0 || decoded.Request != 0 {
					t.Fatalf("command=%d want=%d request=%d response=%d", decoded.Id, tc.want, decoded.Request, decoded.Response)
				}
			}
		})
	}
}
