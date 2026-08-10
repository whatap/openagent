package control

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"

	"open-agent/pkg/config"
)

// These tests pin the response contract the collection server depends on
// (status / source / writable / contents / error). The server passes the pack
// through without interpreting it, so a renamed field here silently breaks the
// dashboard rather than failing a build.

const testScrapeConfig = `# comment must survive the round trip
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

// standaloneHome points the config package at a temp directory holding a
// scrape_config.yaml, and forces the file source so the tests do not depend on
// a Kubernetes environment.
func standaloneHome(t *testing.T, contents string) string {
	t.Helper()

	home := t.TempDir()
	t.Setenv("WHATAP_OPEN_HOME", home)

	prev := config.IsForceStandaloneMode()
	config.SetForceStandaloneMode(true)
	t.Cleanup(func() { config.SetForceStandaloneMode(prev) })

	path := filepath.Join(home, config.ScrapeConfigKey)
	if contents != "" {
		if err := os.WriteFile(path, []byte(contents), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return path
}

func TestProcessScrapeConfigGet(t *testing.T) {
	standaloneHome(t, testScrapeConfig)

	p := pack.NewParamPack()
	processScrapeConfigGet(p)

	if got := p.GetString("status"); got != "ok" {
		t.Fatalf("status = %q, want \"ok\" (error=%q)", got, p.GetString("error"))
	}
	if got := p.GetString("source"); got != config.ScrapeConfigSourceFile {
		t.Fatalf("source = %q, want %q", got, config.ScrapeConfigSourceFile)
	}
	if got := p.GetString("contents"); got != testScrapeConfig {
		t.Fatalf("contents changed:\ngot:\n%s\nwant:\n%s", got, testScrapeConfig)
	}
	writable, ok := p.Get("writable").(*value.BoolValue)
	if !ok {
		t.Fatal("writable field missing; the caller cannot tell whether an edit is possible")
	}
	if !writable.Val {
		t.Fatal("writable = false for the file source, but WriteScrapeConfigRaw accepts it")
	}
	if !isAbsent(p, "error") {
		t.Fatalf("error should be absent on success, got %q", p.GetString("error"))
	}
}

// isAbsent reports whether a key carries no value. ParamPack.Get returns a null
// value for missing keys rather than nil, so absence has to be checked by type.
func isAbsent(p *pack.ParamPack, key string) bool {
	return p.Get(key).GetValueType() == value.VALUE_NULL
}

// TestProcessScrapeConfigGetMissingFile covers the failure shape: the server
// relies on contents being present-but-null rather than absent.
func TestProcessScrapeConfigGetMissingFile(t *testing.T) {
	standaloneHome(t, "")

	p := pack.NewParamPack()
	processScrapeConfigGet(p)

	if got := p.GetString("status"); got != "error" {
		t.Fatalf("status = %q, want \"error\"", got)
	}
	if p.GetString("error") == "" {
		t.Fatal("error message missing")
	}
	if p.Get("contents").GetValueType() != value.VALUE_NULL {
		t.Fatalf("contents should be an explicit null on failure, got type %d", p.Get("contents").GetValueType())
	}
}

func TestProcessScrapeConfigSet(t *testing.T) {
	path := standaloneHome(t, "features:\n  openAgent:\n    enabled: false\n")

	p := pack.NewParamPack()
	p.PutString("contents", testScrapeConfig)
	processScrapeConfigSet(p)

	if got := p.GetString("status"); got != "ok" {
		t.Fatalf("status = %q, want \"ok\" (error=%q)", got, p.GetString("error"))
	}
	if got := p.GetString("source"); got != config.ScrapeConfigSourceFile {
		t.Fatalf("source = %q, want %q", got, config.ScrapeConfigSourceFile)
	}
	if !isAbsent(p, "contents") {
		t.Fatal("submitted YAML should not be echoed back in the response")
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != testScrapeConfig {
		t.Fatalf("file not replaced:\n%s", data)
	}
	if _, err := os.Stat(path + ".old"); err != nil {
		t.Fatalf("previous contents should be kept as %s.old: %v", path, err)
	}
}

func TestProcessScrapeConfigSetRejects(t *testing.T) {
	cases := []struct {
		name     string
		contents string
		put      bool
	}{
		{name: "missing contents", put: false},
		{name: "empty contents", contents: "   \n", put: true},
		{name: "invalid yaml", contents: "features:\n  other: {}\n", put: true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := standaloneHome(t, testScrapeConfig)

			p := pack.NewParamPack()
			if tc.put {
				p.PutString("contents", tc.contents)
			}
			processScrapeConfigSet(p)

			if got := p.GetString("status"); got != "error" {
				t.Fatalf("status = %q, want \"error\"", got)
			}
			if p.GetString("error") == "" {
				t.Fatal("error message missing")
			}

			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != testScrapeConfig {
				t.Fatalf("file was modified despite rejection:\n%s", data)
			}
		})
	}
}
