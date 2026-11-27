package rdsevent

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v2"
)

// sectionFromYAML parses a scrape_config.yaml document the way ConfigManager
// does (yaml.v2) and returns features.openAgent.awsRdsEvents.
func sectionFromYAML(t *testing.T, doc string) map[string]interface{} {
	t.Helper()
	var parsed map[string]interface{}
	if err := yaml.Unmarshal([]byte(doc), &parsed); err != nil {
		t.Fatalf("yaml: %v", err)
	}
	features, _ := parsed["features"].(map[interface{}]interface{})
	openAgent, _ := features["openAgent"].(map[interface{}]interface{})
	section, _ := stringKeys(openAgent[SectionName]).(map[string]interface{})
	return section
}

func TestParseConfigIsOffByDefault(t *testing.T) {
	home := t.TempDir()
	cases := map[string]map[string]interface{}{
		"no section":       nil,
		"enabled missing":  {"regions": []interface{}{"ap-northeast-2"}},
		"enabled false":    {"enabled": false, "regions": []interface{}{"ap-northeast-2"}},
		"disabled ignores": {"enabled": false, "regoins": "typo", "interval": "nonsense"},
	}
	for name, section := range cases {
		t.Run(name, func(t *testing.T) {
			cfg, err := ParseConfig(section, home)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if cfg.Enabled {
				t.Fatal("collector enabled without enabled: true")
			}
		})
	}
}

func TestParseConfigDefaults(t *testing.T) {
	home := t.TempDir()
	cfg, err := ParseConfig(sectionFromYAML(t, `
features:
  openAgent:
    enabled: true
    awsRdsEvents:
      enabled: true
      regions: [ap-northeast-2]
`), home)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Enabled:         true,
		Regions:         []string{"ap-northeast-2"},
		Interval:        60 * time.Second,
		AgencyName:      "openagent",
		Overlap:         5 * time.Minute,
		InitialLookback: 10 * time.Minute,
		MaxCatchUp:      24 * time.Hour,
		StateFile:       filepath.Join(home, "rds_events_state.json"),
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("defaults\n got %+v\nwant %+v", cfg, want)
	}
}

func TestParseConfigAllKeys(t *testing.T) {
	home := t.TempDir()
	cfg, err := ParseConfig(sectionFromYAML(t, `
features:
  openAgent:
    awsRdsEvents:
      enabled: true
      roleArn: arn:aws:iam::111122223333:role/WhatapRdsEvents
      externalId: whatap-external-id
      regions:
        - ap-northeast-2
        - us-east-1
        - ap-northeast-2
      interval: 90s
      agencyName: aws.rdsevent
      overlap: 120
      initialLookback: 30m
      maxCatchUp: 6h
      stateFile: state/rds.json
`), home)
	if err != nil {
		t.Fatal(err)
	}
	want := Config{
		Enabled:         true,
		RoleARN:         "arn:aws:iam::111122223333:role/WhatapRdsEvents",
		ExternalID:      "whatap-external-id",
		Regions:         []string{"ap-northeast-2", "us-east-1"},
		Interval:        90 * time.Second,
		AgencyName:      "aws.rdsevent",
		Overlap:         2 * time.Minute,
		InitialLookback: 30 * time.Minute,
		MaxCatchUp:      6 * time.Hour,
		StateFile:       filepath.Join(home, "state", "rds.json"),
	}
	if !reflect.DeepEqual(cfg, want) {
		t.Fatalf("parsed\n got %+v\nwant %+v", cfg, want)
	}

	// A comma-separated string works as well, and an absolute state path is kept.
	cfg, err = ParseConfig(map[string]interface{}{
		"enabled":   "true",
		"regions":   "eu-central-1, us-west-2",
		"stateFile": "/var/lib/openagent/rds.json",
	}, home)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(cfg.Regions, []string{"eu-central-1", "us-west-2"}) || cfg.StateFile != "/var/lib/openagent/rds.json" {
		t.Fatalf("got regions %v stateFile %s", cfg.Regions, cfg.StateFile)
	}
}

func TestParseConfigRejectsInvalidSettings(t *testing.T) {
	base := func() map[string]interface{} {
		return map[string]interface{}{"enabled": true, "regions": []interface{}{"ap-northeast-2"}}
	}
	cases := []struct {
		name   string
		key    string
		value  interface{}
		expect string
	}{
		{"no regions", "regions", []interface{}{}, "at least one region"},
		{"bad region", "regions", []interface{}{"Seoul"}, "not an AWS region"},
		{"bad role", "roleArn", "arn:aws:iam::123:user/someone", "roleArn"},
		{"external id without role", "externalId", "abc123", "needs roleArn"},
		{"interval too short", "interval", "5s", "interval"},
		{"bad duration", "overlap", "five minutes", "overlap"},
		{"overlap too long", "overlap", "2h", "overlap"},
		{"lookback beyond retention", "initialLookback", "400h", "initialLookback"},
		{"lookback at the 14-day boundary", "initialLookback", "336h", "initialLookback"},
		{"catch-up at the 14-day boundary", "maxCatchUp", "336h", "maxCatchUp"},
		{"catch-up below interval", "maxCatchUp", "30s", "maxCatchUp"},
		{"empty agency", "agencyName", " ", "agencyName"},
		{"unknown key", "regoins", "ap-northeast-2", "unknown keys regoins"},
		{"enabled not boolean", "enabled", "maybe", "enabled"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			section := base()
			section[tc.key] = tc.value
			cfg, err := ParseConfig(section, t.TempDir())
			if err == nil || !strings.Contains(err.Error(), tc.expect) {
				t.Fatalf("error = %v, want one mentioning %q", err, tc.expect)
			}
			if cfg.Enabled {
				t.Fatal("an invalid configuration must leave the collector off")
			}
		})
	}
}

func TestValidationAndSummaryHideAccountAndExternalID(t *testing.T) {
	section := map[string]interface{}{
		"enabled":    true,
		"regions":    []interface{}{"ap-northeast-2"},
		"roleArn":    "arn:aws:iam::111122223333:role/Whatap",
		"externalId": "secret-external-id",
	}
	cfg, err := ParseConfig(section, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	summary := cfg.summary()
	if strings.Contains(summary, "111122223333") || strings.Contains(summary, "secret-external-id") {
		t.Fatalf("summary leaks the account or external ID: %s", summary)
	}
	if !strings.Contains(summary, "arn:aws:iam::<account>:role/Whatap") || !strings.Contains(summary, "externalId set") {
		t.Fatalf("summary lost the role name: %s", summary)
	}
	section["roleArn"] = "arn:aws:iam::111122223333:user/not-a-role"
	if _, err := ParseConfig(section, t.TempDir()); err == nil || strings.Contains(err.Error(), "111122223333") {
		t.Fatalf("validation error should not echo the ARN: %v", err)
	}
}
