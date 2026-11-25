package rdsevent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"gopkg.in/yaml.v2"
)

// DryRunCommand is the openagent subcommand that runs one round against AWS
// and prints the packs as JSON instead of sending them.
const DryRunCommand = "rds-events-dryrun"

const dryRunTimeout = 5 * time.Minute

// DryRunMain implements `openagent rds-events-dryrun`. It reads the same
// awsRdsEvents section as the agent (enabled is ignored), applies the flags,
// queries [now-lookback, now] once per region and writes the packs to stdout
// as a JSON array in send order. Progress and errors go to stderr. It neither
// reads nor writes the state file and never connects to WhaTap. The exit code
// is 0 on success, 1 when a region failed and 2 for usage or config errors.
func DryRunMain(args []string, stdout, stderr io.Writer) int {
	return dryRun(args, stdout, stderr, NewAWSClientFactory(), time.Now)
}

func dryRun(args []string, stdout, stderr io.Writer, factory ClientFactory, now func() time.Time) int {
	home := os.Getenv("WHATAP_OPEN_HOME")
	if home == "" {
		home = "."
	}
	fs := flag.NewFlagSet(DryRunCommand, flag.ContinueOnError)
	fs.SetOutput(stderr)
	configPath := fs.String("config", "", "scrape_config.yaml with features.openAgent."+SectionName+" (default $WHATAP_OPEN_HOME/scrape_config.yaml; skipped when that file is missing)")
	regions := fs.String("regions", "", "comma-separated regions, overriding the config")
	roleARN := fs.String("role-arn", "", "IAM role to assume, overriding the config")
	externalID := fs.String("external-id", "", "external ID for the role, overriding the config")
	noRole := fs.Bool("no-role", false, "ignore roleArn and externalId from the config and use the default credential chain")
	lookback := fs.Duration("lookback", 0, "how far back to read (default: initialLookback from the config, 10m)")
	agencyName := fs.String("agency-name", "", "agency tag value (default: agencyName from the config, "+DefaultAgencyName+")")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: openagent %s [flags]\n\nRuns one RDS DescribeEvents round and prints the %s packs as JSON. Nothing is sent to WhaTap.\n\n", DryRunCommand, Category)
		fs.PrintDefaults()
	}
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if fs.NArg() > 0 {
		fmt.Fprintf(stderr, "unexpected arguments: %s\n", strings.Join(fs.Args(), " "))
		return 2
	}

	path := *configPath
	if path == "" {
		path = filepath.Join(home, "scrape_config.yaml")
	}
	section, err := readSection(path)
	if err != nil {
		if *configPath != "" || !os.IsNotExist(err) {
			fmt.Fprintf(stderr, "read %s: %v\n", path, err)
			return 2
		}
		section = nil
	}
	cfg, err := parseFields(section, home)
	if err != nil {
		fmt.Fprintf(stderr, "features.openAgent.%s in %s: %v\n", SectionName, path, err)
		return 2
	}
	if *regions != "" {
		cfg.Regions, _ = regionsValue(*regions)
	}
	if *noRole {
		cfg.RoleARN, cfg.ExternalID = "", ""
	}
	if *roleARN != "" {
		cfg.RoleARN = strings.TrimSpace(*roleARN)
	}
	if *externalID != "" {
		cfg.ExternalID = strings.TrimSpace(*externalID)
	}
	if *agencyName != "" {
		cfg.AgencyName = strings.TrimSpace(*agencyName)
	}
	if *lookback < 0 {
		fmt.Fprintln(stderr, "-lookback must be positive")
		return 2
	}
	if *lookback > 0 {
		cfg.InitialLookback = *lookback
	}
	if err := cfg.Validate(); err != nil {
		fmt.Fprintf(stderr, "invalid settings: %v\n", err)
		return 2
	}

	var packs []*pack.TagCountPack
	collector := NewCollector(factory, func(p *pack.TagCountPack) error {
		packs = append(packs, p)
		return nil
	})
	collector.persist = false
	collector.now = now
	stderrLog := func(level string) func(id, format string, v ...interface{}) {
		return func(id, format string, v ...interface{}) {
			fmt.Fprintf(stderr, "[%s] %s\n", level, fmt.Sprintf(format, v...))
		}
	}
	collector.infof, collector.warnf, collector.errorf = stderrLog("INFO"), stderrLog("WARN"), stderrLog("ERROR")

	fmt.Fprintf(stderr, "dry run (nothing is sent to WhaTap; the state file is neither read nor written): %s\n", cfg.summary())
	ctx, cancel := context.WithTimeout(context.Background(), dryRunTimeout)
	defer cancel()
	res := collector.RunCycle(ctx, cfg)
	failed := res.Panic != nil
	for _, rr := range res.Regions {
		if rr.Err != nil {
			failed = true
			fmt.Fprintf(stderr, "%s: FAILED [%s] window %s..%s\n", rr.Region, rr.Class, formatTime(rr.Start), formatTime(rr.End))
			continue
		}
		fmt.Fprintf(stderr, "%s: window %s..%s, %d page(s), %d event(s), %d pack(s)\n",
			rr.Region, formatTime(rr.Start), formatTime(rr.End), rr.Pages, rr.Fetched, rr.Queued)
	}
	if err := writePacksJSON(stdout, packs); err != nil {
		fmt.Fprintf(stderr, "write output: %v\n", err)
		return 1
	}
	if failed {
		return 1
	}
	return 0
}

func formatTime(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

// readSection returns features.openAgent.awsRdsEvents from a scrape config
// file, or nil when the file has no such section.
func readSection(path string) (map[string]interface{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	features, _ := doc["features"].(map[interface{}]interface{})
	openAgent, _ := features["openAgent"].(map[interface{}]interface{})
	section, _ := stringKeys(openAgent[SectionName]).(map[string]interface{})
	return section, nil
}

// stringKeys turns the map[interface{}]interface{} values of yaml.v2 into
// map[string]interface{} recursively.
func stringKeys(v interface{}) interface{} {
	switch t := v.(type) {
	case map[interface{}]interface{}:
		out := make(map[string]interface{}, len(t))
		for k, val := range t {
			if key, ok := k.(string); ok {
				out[key] = stringKeys(val)
			}
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(t))
		for i, val := range t {
			out[i] = stringKeys(val)
		}
		return out
	}
	return v
}

// writePacksJSON prints packs with tags and fields in wire order. timeUTC is
// a reading aid and is not part of the pack.
func writePacksJSON(w io.Writer, packs []*pack.TagCountPack) error {
	var buf bytes.Buffer
	buf.WriteString("[")
	for i, p := range packs {
		if i > 0 {
			buf.WriteString(",")
		}
		buf.WriteString("\n  {\"category\": ")
		writeJSONString(&buf, p.Category)
		buf.WriteString(", \"time\": ")
		buf.WriteString(strconv.FormatInt(p.Time, 10))
		buf.WriteString(", \"timeUTC\": ")
		writeJSONString(&buf, time.UnixMilli(p.Time).UTC().Format("2006-01-02T15:04:05.000Z"))
		buf.WriteString(",\n   \"tags\": ")
		writeMapValue(&buf, p.Tags)
		buf.WriteString(",\n   \"fields\": ")
		writeMapValue(&buf, p.Data)
		buf.WriteString("}")
	}
	if len(packs) > 0 {
		buf.WriteString("\n")
	}
	buf.WriteString("]\n")
	_, err := w.Write(buf.Bytes())
	return err
}

func writeMapValue(buf *bytes.Buffer, m *value.MapValue) {
	buf.WriteString("{")
	keys := m.Keys()
	first := true
	for keys.HasMoreElements() {
		key := keys.NextString()
		if !first {
			buf.WriteString(", ")
		}
		first = false
		writeJSONString(buf, key)
		buf.WriteString(": ")
		switch v := m.Get(key).(type) {
		case *value.TextValue:
			writeJSONString(buf, v.Val)
		default:
			writeJSONString(buf, fmt.Sprint(v))
		}
	}
	buf.WriteString("}")
}

func writeJSONString(buf *bytes.Buffer, s string) {
	b, _ := json.Marshal(s)
	buf.Write(b)
}
