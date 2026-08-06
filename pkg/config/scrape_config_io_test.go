package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const validScrapeConfig = `# comment must survive a round trip
features:
  openAgent:
    enabled: true
    targets:
    - targetName: metric-exporter
      type: StaticEndpoints
      endpoints:
        - address: "localhost:9529"
          path: "/metrics"
`

func TestValidateScrapeConfig(t *testing.T) {
	cases := []struct {
		name    string
		input   string
		wantErr string
	}{
		{name: "valid", input: validScrapeConfig},
		{name: "broken yaml", input: "features:\n  openAgent:\n   - bad\n  indent", wantErr: "invalid yaml"},
		{name: "empty document", input: "# only a comment\n", wantErr: "invalid yaml: empty document"},
		{name: "missing features", input: "global:\n  scrape_interval: 15s\n", wantErr: "missing 'features' section"},
		{name: "missing openAgent", input: "features:\n  other: {}\n", wantErr: "missing 'features.openAgent' section"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateScrapeConfig(tc.input)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

// TestValidateRepoScrapeConfig guards against the validator rejecting the
// configuration the agent actually ships with.
func TestValidateRepoScrapeConfig(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", ScrapeConfigKey))
	if err != nil {
		t.Skipf("repo %s not readable: %v", ScrapeConfigKey, err)
	}
	if err := ValidateScrapeConfig(string(data)); err != nil {
		t.Fatalf("repo %s failed validation: %v", ScrapeConfigKey, err)
	}
}

func TestScrapeConfigFileRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHATAP_OPEN_HOME", home)

	prevStandalone := IsForceStandaloneMode()
	SetForceStandaloneMode(true)
	t.Cleanup(func() { SetForceStandaloneMode(prevStandalone) })

	path := ScrapeConfigFilePath()
	if want := filepath.Join(home, ScrapeConfigKey); path != want {
		t.Fatalf("ScrapeConfigFilePath() = %q, want %q", path, want)
	}

	// Seed an initial file so the backup path is exercised.
	if err := os.WriteFile(path, []byte("features:\n  openAgent:\n    enabled: false\n"), 0644); err != nil {
		t.Fatal(err)
	}

	source, err := WriteScrapeConfigRaw(validScrapeConfig)
	if err != nil {
		t.Fatalf("WriteScrapeConfigRaw: %v", err)
	}
	if source != ScrapeConfigSourceFile {
		t.Fatalf("source = %q, want %q", source, ScrapeConfigSourceFile)
	}

	if _, err := os.Stat(path + ".old"); err != nil {
		t.Fatalf("expected backup at %s.old: %v", path, err)
	}

	contents, source, err := ReadScrapeConfigRaw()
	if err != nil {
		t.Fatalf("ReadScrapeConfigRaw: %v", err)
	}
	if source != ScrapeConfigSourceFile {
		t.Fatalf("source = %q, want %q", source, ScrapeConfigSourceFile)
	}
	if contents != validScrapeConfig {
		t.Fatalf("round trip changed the contents:\ngot:\n%s\nwant:\n%s", contents, validScrapeConfig)
	}
}

// TestWriteScrapeConfigRejectsInvalid makes sure a broken payload never reaches
// the file - a bad scrape config drops every target on the next reload.
func TestWriteScrapeConfigRejectsInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("WHATAP_OPEN_HOME", home)

	prevStandalone := IsForceStandaloneMode()
	SetForceStandaloneMode(true)
	t.Cleanup(func() { SetForceStandaloneMode(prevStandalone) })

	path := ScrapeConfigFilePath()
	if err := os.WriteFile(path, []byte(validScrapeConfig), 0644); err != nil {
		t.Fatal(err)
	}

	if _, err := WriteScrapeConfigRaw("features:\n  other: {}\n"); err == nil {
		t.Fatal("expected validation error, got nil")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != validScrapeConfig {
		t.Fatalf("file was modified despite validation failure:\n%s", data)
	}
}
