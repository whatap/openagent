package rdsevent

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const dryRunConfig = `features:
  openAgent:
    enabled: true
    targets: []
    awsRdsEvents:
      enabled: false
      regions: [ap-northeast-2]
      agencyName: aws.rdsevent
`

func TestDryRunPrintsPacksAndLeavesStateAlone(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHATAP_OPEN_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "scrape_config.yaml"), []byte(dryRunConfig), 0o600); err != nil {
		t.Fatal(err)
	}
	api := &fakeRDS{}
	api.add(rdsEvent("orders-db", "DB instance restarted", t0.Add(-2*time.Hour), "availability"), time.Time{})
	api.add(rdsEvent("orders-db", "Multi-AZ instance failover completed.", t0.Add(-time.Hour), "failover"), time.Time{})
	api.add(rdsEvent("orders-db", "older than the lookback", t0.Add(-30*time.Hour), "availability"), time.Time{})
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": api}}
	var stdout, stderr bytes.Buffer

	code := dryRun([]string{"-lookback", "24h"}, &stdout, &stderr, factory, func() time.Time { return t0 })
	if code != 0 {
		t.Fatalf("exit %d\n%s", code, stderr.String())
	}
	var packs []struct {
		Category string            `json:"category"`
		Time     int64             `json:"time"`
		TimeUTC  string            `json:"timeUTC"`
		Tags     map[string]string `json:"tags"`
		Fields   map[string]string `json:"fields"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &packs); err != nil {
		t.Fatalf("stdout is not JSON: %v\n%s", err, stdout.String())
	}
	if len(packs) != 2 {
		t.Fatalf("got %d packs:\n%s", len(packs), stdout.String())
	}
	first := packs[0]
	if first.Category != "aws_rds_event" || first.Time != t0.Add(-2*time.Hour).UnixMilli() || first.TimeUTC != "2026-11-25T01:00:00.000Z" {
		t.Fatalf("first pack %+v", first)
	}
	if first.Tags["agency"] != "aws.rdsevent" || first.Tags["category0"] != "availability" || first.Fields["message"] != "DB instance restarted" {
		t.Fatalf("first pack tags %v fields %v", first.Tags, first.Fields)
	}
	// Tags are printed in wire order.
	out := stdout.String()
	order := []string{`"category0"`, `"sourceArn"`, `"sourceIdentifier"`, `"sourceType"`, `"agency"`}
	last := -1
	for _, key := range order {
		i := strings.Index(out, key)
		if i <= last {
			t.Fatalf("tag %s out of order in\n%s", key, out)
		}
		last = i
	}
	if call := api.lastCall(); !call.StartTime.Equal(t0.Add(-24*time.Hour)) || !call.EndTime.Equal(t0) {
		t.Fatalf("window %v..%v", call.StartTime, call.EndTime)
	}
	if !strings.Contains(stderr.String(), "nothing is sent to WhaTap") || !strings.Contains(stderr.String(), "ap-northeast-2: window") {
		t.Fatalf("stderr:\n%s", stderr.String())
	}
	if _, err := os.Stat(filepath.Join(home, DefaultStateFileName)); !os.IsNotExist(err) {
		t.Fatalf("dry run touched the state file (stat err %v)", err)
	}
}

func TestDryRunFlagsOverrideAndFailures(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHATAP_OPEN_HOME", home) // no scrape_config.yaml there
	denied := &fakeRDS{errs: []error{apiError("AccessDenied")}}
	ok := &fakeRDS{}
	factory := &fakeFactory{apis: map[string]EventsAPI{"ap-northeast-2": denied, "us-east-1": ok}}
	var stdout, stderr bytes.Buffer
	code := dryRun([]string{"-regions", "ap-northeast-2,us-east-1", "-role-arn", "arn:aws:iam::111122223333:role/R", "-external-id", "example-id"},
		&stdout, &stderr, factory, func() time.Time { return t0 })
	if code != 1 {
		t.Fatalf("exit %d, want 1 for a failed region\n%s", code, stderr.String())
	}
	if strings.TrimSpace(stdout.String()) != "[]" {
		t.Fatalf("stdout %q", stdout.String())
	}
	errOut := stderr.String()
	if !strings.Contains(errOut, "ap-northeast-2: FAILED [access-denied]") || !strings.Contains(errOut, "us-east-1: window") {
		t.Fatalf("stderr:\n%s", errOut)
	}
	if strings.Contains(errOut, "111122223333") || strings.Contains(errOut, "example-id") {
		t.Fatalf("stderr leaks the account or external ID:\n%s", errOut)
	}
	if call := ok.lastCall(); !call.StartTime.Equal(t0.Add(-DefaultInitialLookback)) {
		t.Fatalf("default lookback window starts at %v", call.StartTime)
	}
}

func TestDryRunUsageErrors(t *testing.T) {
	t.Setenv("WHATAP_OPEN_HOME", t.TempDir())
	cases := map[string][]string{
		"no region":       {},
		"bad flag":        {"-nope"},
		"extra argument":  {"-regions", "ap-northeast-2", "extra"},
		"missing config":  {"-config", filepath.Join(t.TempDir(), "absent.yaml"), "-regions", "ap-northeast-2"},
		"negative window": {"-regions", "ap-northeast-2", "-lookback", "-1h"},
	}
	for name, args := range cases {
		t.Run(name, func(t *testing.T) {
			factory := &fakeFactory{}
			var stdout, stderr bytes.Buffer
			if code := dryRun(args, &stdout, &stderr, factory, func() time.Time { return t0 }); code != 2 {
				t.Fatalf("exit %d, want 2\n%s", code, stderr.String())
			}
			if factory.calls != 0 || stdout.Len() != 0 {
				t.Fatalf("usage error reached AWS (%d calls) or stdout %q", factory.calls, stdout.String())
			}
		})
	}
}
