package ec2state

import (
	"fmt"
	"testing"
	"time"

	"github.com/whatap/golib/lang/value"
)

// Yard resolves all matching Server OIDs within each destination project.
// OpenAgent reports the instance once per project, not once per Server OID.
func TestForwarderPreservesYardTerminationContract(t *testing.T) {
	settings := map[string]string{
		"ec2_state_forwarding_enabled": "true",
		"ec2_state_licenses":           fixtureLicense(101) + "," + fixtureLicense(202),
	}
	start := time.Unix(1700000000, 0)
	now := start
	f := &Forwarder{now: func() time.Time { return now }}
	for _, step := range []struct {
		name, state string
		code        int64
		after       time.Duration
		want        int
	}{
		{"shutting down", "shutting-down", 32, 0, 2},
		{"unchanged shutdown", "shutting-down", 32, 30 * time.Second, 0},
		{"terminated before refresh", "terminated", 48, time.Minute, 2},
		{"terminated before refresh boundary", "terminated", 48, 6*time.Minute - time.Millisecond, 0},
		{"terminated refresh", "terminated", 48, 6 * time.Minute, 2},
	} {
		t.Run(step.name, func(t *testing.T) {
			now = start.Add(step.after)
			sample := fmt.Sprintf("aws_ec2_instance_state{instance_id=\"i-0123456789abcdef0\",state_name=\"%s\",region=\"ap-northeast-2\"} %d\n", step.state, step.code)
			// Duplicate exporter observations are distinct from multiple Server
			// registrations in Yard; neither requires an agent-side OID filter.
			raw := fixtureScrape("# TYPE aws_ec2_instance_state gauge\n" + sample + sample)
			raw.CollectionTime = now.UnixMilli()
			seen := make(map[int64]bool)
			for _, p := range requireForwardCount(t, f, settings, raw, step.want) {
				if (p.Pcode != 101 && p.Pcode != 202) || seen[p.Pcode] {
					t.Fatal("expected one instance observation per configured project")
				}
				seen[p.Pcode] = true
				if p.Id != 1002 || p.Oid != -123 || p.Time != raw.CollectionTime || p.Request != 0 || p.Response != 0 {
					t.Fatal("termination observation lost command1002, sender identity or oneway semantics")
				}
				id, idOK := p.Get("instanceId").(*value.TextValue)
				name, nameOK := p.Get("stateName").(*value.TextValue)
				code, codeOK := p.Get("stateCode").(*value.DecimalValue)
				if !idOK || !nameOK || !codeOK || id.Val != "i-0123456789abcdef0" || name.Val != step.state || code.Val != step.code {
					t.Fatal("termination observation lost its typed instance state")
				}
				keys, count := p.Keys(), 0
				for keys.HasMoreElements() {
					key := keys.NextString()
					if key != "instanceId" && key != "stateCode" && key != "stateName" {
						t.Fatalf("unexpected payload field %q; Server mapping belongs to Yard", key)
					}
					count++
				}
				if count != 3 {
					t.Fatalf("payload fields=%d want=3", count)
				}
			}
		})
	}
	// The exporter can eventually stop listing a terminated instance. Do not
	// synthesize another termination from its absence, even after refresh is due.
	now = start.Add(12 * time.Minute)
	missing := fixtureScrape("ordinary_metric 7\n")
	missing.CollectionTime = now.UnixMilli()
	requireForwardCount(t, f, settings, missing, 0)
}
