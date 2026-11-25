// Package rdsevent polls AWS RDS DescribeEvents and sends every event as a
// TagCountPack in the shape the WhaTap agency (cloud monitoring collector)
// produced for RDS events (category aws_rds_event), so alert rules written for
// the agency keep working.
package rdsevent

import (
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// SectionName is the key of the settings under features.openAgent in
// scrape_config.yaml.
const SectionName = "awsRdsEvents"

const (
	// DefaultInterval is the time between two DescribeEvents rounds (the
	// agency task interval was also 60 seconds).
	DefaultInterval = 60 * time.Second
	// DefaultOverlap re-reads the end of the previous window because RDS can
	// list an event a little after its date.
	DefaultOverlap = 5 * time.Minute
	// DefaultInitialLookback is how far the very first round looks back. It
	// is short on purpose: an old restart must not raise a new alert.
	DefaultInitialLookback = 10 * time.Minute
	// DefaultMaxCatchUp bounds the window after a long stop: older events are
	// skipped (and logged) instead of being sent hours late.
	DefaultMaxCatchUp = 24 * time.Hour
	// DefaultAgencyName is the value of the agency tag. The agency itself put
	// its service id there.
	DefaultAgencyName = "openagent"
	// DefaultStateFileName is created in the OpenAgent home directory.
	DefaultStateFileName = "rds_events_state.json"

	minInterval = 10 * time.Second
	maxOverlap  = time.Hour
	// rdsEventRetention is how far back DescribeEvents can return events.
	rdsEventRetention = 14 * 24 * time.Hour
)

// Config is the parsed features.openAgent.awsRdsEvents section.
type Config struct {
	Enabled         bool
	RoleARN         string
	ExternalID      string
	Regions         []string
	Interval        time.Duration
	AgencyName      string
	Overlap         time.Duration
	InitialLookback time.Duration
	MaxCatchUp      time.Duration
	StateFile       string
}

var (
	regionPattern  = regexp.MustCompile(`^[a-z]{2}(-[a-z]+)+-[0-9]+$`)
	roleARNPattern = regexp.MustCompile(`^arn:aws(-[a-z]+)*:iam::[0-9]{12}:role/[A-Za-z0-9+=,.@_/-]+$`)
	externalIDChar = regexp.MustCompile(`^[A-Za-z0-9+=,.@:/_-]+$`)
	accountPattern = regexp.MustCompile(`\b[0-9]{12}\b`)
)

var knownKeys = map[string]bool{
	"enabled":         true,
	"roleArn":         true,
	"externalId":      true,
	"regions":         true,
	"interval":        true,
	"agencyName":      true,
	"overlap":         true,
	"initialLookback": true,
	"maxCatchUp":      true,
	"stateFile":       true,
}

// DefaultConfig returns the disabled configuration with every default set.
// home is the OpenAgent home directory ($WHATAP_OPEN_HOME, "." when empty).
func DefaultConfig(home string) Config {
	if home == "" {
		home = "."
	}
	return Config{
		Interval:        DefaultInterval,
		AgencyName:      DefaultAgencyName,
		Overlap:         DefaultOverlap,
		InitialLookback: DefaultInitialLookback,
		MaxCatchUp:      DefaultMaxCatchUp,
		StateFile:       filepath.Join(home, DefaultStateFileName),
	}
}

// ParseConfig reads the awsRdsEvents section. A missing section, or one whose
// enabled is not true, gives a disabled configuration and the other keys are
// not checked. An enabled section is fully validated.
func ParseConfig(section map[string]interface{}, home string) (Config, error) {
	cfg := DefaultConfig(home)
	if section == nil {
		return cfg, nil
	}
	enabled, err := boolValue(section["enabled"])
	if err != nil {
		return cfg, fmt.Errorf("enabled: %v", err)
	}
	if !enabled {
		return cfg, nil
	}
	cfg, err = parseFields(section, home)
	if err != nil {
		return DefaultConfig(home), err
	}
	cfg.Enabled = true
	if err := cfg.Validate(); err != nil {
		cfg.Enabled = false
		return cfg, err
	}
	return cfg, nil
}

// parseFields converts every known key without the enabled gate and without
// validation, so the dry run can apply command-line overrides first.
func parseFields(section map[string]interface{}, home string) (Config, error) {
	cfg := DefaultConfig(home)
	if section == nil {
		return cfg, nil
	}
	var unknown []string
	for key := range section {
		if !knownKeys[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return cfg, fmt.Errorf("unknown keys %s (allowed: enabled, roleArn, externalId, regions, interval, agencyName, overlap, initialLookback, maxCatchUp, stateFile)", strings.Join(unknown, ", "))
	}
	var err error
	if cfg.Enabled, err = boolValue(section["enabled"]); err != nil {
		return cfg, fmt.Errorf("enabled: %v", err)
	}
	if cfg.RoleARN, err = stringValue(section["roleArn"]); err != nil {
		return cfg, fmt.Errorf("roleArn: %v", err)
	}
	if cfg.ExternalID, err = stringValue(section["externalId"]); err != nil {
		return cfg, fmt.Errorf("externalId: %v", err)
	}
	if cfg.Regions, err = regionsValue(section["regions"]); err != nil {
		return cfg, fmt.Errorf("regions: %v", err)
	}
	durations := []struct {
		key    string
		target *time.Duration
	}{
		{"interval", &cfg.Interval},
		{"overlap", &cfg.Overlap},
		{"initialLookback", &cfg.InitialLookback},
		{"maxCatchUp", &cfg.MaxCatchUp},
	}
	for _, d := range durations {
		raw, ok := section[d.key]
		if !ok || raw == nil {
			continue
		}
		if *d.target, err = durationValue(raw); err != nil {
			return cfg, fmt.Errorf("%s: %v", d.key, err)
		}
	}
	if raw, ok := section["agencyName"]; ok && raw != nil {
		if cfg.AgencyName, err = stringValue(raw); err != nil {
			return cfg, fmt.Errorf("agencyName: %v", err)
		}
	}
	if raw, ok := section["stateFile"]; ok && raw != nil {
		path, err := stringValue(raw)
		if err != nil {
			return cfg, fmt.Errorf("stateFile: %v", err)
		}
		if path != "" {
			cfg.StateFile = resolvePath(home, path)
		}
	}
	return cfg, nil
}

// Validate checks the values an enabled collector needs. Messages never echo
// the role ARN or the external ID.
func (c Config) Validate() error {
	if len(c.Regions) == 0 {
		return fmt.Errorf("regions: at least one region is required (for example [ap-northeast-2])")
	}
	for _, region := range c.Regions {
		if !regionPattern.MatchString(region) {
			return fmt.Errorf("regions: %q is not an AWS region name", region)
		}
	}
	if c.RoleARN != "" && !roleARNPattern.MatchString(c.RoleARN) {
		return fmt.Errorf("roleArn: expected arn:aws:iam::<account>:role/<name>")
	}
	if c.ExternalID != "" {
		if c.RoleARN == "" {
			return fmt.Errorf("externalId: needs roleArn (it is only sent with AssumeRole)")
		}
		if len(c.ExternalID) < 2 || len(c.ExternalID) > 1224 || !externalIDChar.MatchString(c.ExternalID) {
			return fmt.Errorf("externalId: must be 2-1224 characters of letters, digits and +=,.@:/-_")
		}
	}
	if c.Interval < minInterval {
		return fmt.Errorf("interval: must be at least %s", minInterval)
	}
	if c.Overlap < 0 || c.Overlap > maxOverlap {
		return fmt.Errorf("overlap: must be between 0s and %s", maxOverlap)
	}
	if c.InitialLookback <= 0 || c.InitialLookback > rdsEventRetention {
		return fmt.Errorf("initialLookback: must be greater than 0s and at most 336h (RDS keeps events for 14 days)")
	}
	if c.MaxCatchUp < c.Interval || c.MaxCatchUp > rdsEventRetention {
		return fmt.Errorf("maxCatchUp: must be between interval and 336h (RDS keeps events for 14 days)")
	}
	if strings.TrimSpace(c.AgencyName) == "" {
		return fmt.Errorf("agencyName: must not be empty")
	}
	if c.StateFile == "" {
		return fmt.Errorf("stateFile: must not be empty")
	}
	return nil
}

// summary describes the effective configuration for the log. The account in
// the role ARN and the external ID are not printed.
func (c Config) summary() string {
	role := "default credential chain"
	if c.RoleARN != "" {
		role = "assume " + maskAccounts(c.RoleARN)
		if c.ExternalID != "" {
			role += " (externalId set)"
		}
	}
	return fmt.Sprintf("regions=%s credentials=%s interval=%s overlap=%s initialLookback=%s maxCatchUp=%s agencyName=%s stateFile=%s",
		strings.Join(c.Regions, ","), role, c.Interval, c.Overlap, c.InitialLookback, c.MaxCatchUp, c.AgencyName, c.StateFile)
}

// maskAccounts hides 12-digit AWS account numbers, for example in a role ARN
// or in an AccessDenied message, before they reach a log line.
func maskAccounts(s string) string {
	return accountPattern.ReplaceAllString(s, "<account>")
}

func resolvePath(home, path string) string {
	if filepath.IsAbs(path) {
		return path
	}
	if home == "" {
		home = "."
	}
	return filepath.Join(home, path)
}

func boolValue(raw interface{}) (bool, error) {
	switch v := raw.(type) {
	case nil:
		return false, nil
	case bool:
		return v, nil
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true":
			return true, nil
		case "false", "":
			return false, nil
		}
	}
	return false, fmt.Errorf("expected true or false, got %v", raw)
}

func stringValue(raw interface{}) (string, error) {
	switch v := raw.(type) {
	case nil:
		return "", nil
	case string:
		return strings.TrimSpace(v), nil
	}
	return "", fmt.Errorf("expected a string, got %T", raw)
}

// regionsValue accepts a YAML list or a comma-separated string, drops empty
// entries and duplicates, and keeps the configured order.
func regionsValue(raw interface{}) ([]string, error) {
	var items []string
	switch v := raw.(type) {
	case nil:
		return nil, nil
	case string:
		items = strings.Split(v, ",")
	case []string:
		items = v
	case []interface{}:
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("expected region names, got %T", item)
			}
			items = append(items, s)
		}
	default:
		return nil, fmt.Errorf("expected a list of region names, got %T", raw)
	}
	seen := make(map[string]bool)
	var regions []string
	for _, item := range items {
		region := strings.TrimSpace(item)
		if region == "" || seen[region] {
			continue
		}
		seen[region] = true
		regions = append(regions, region)
	}
	return regions, nil
}

// durationValue accepts a Go duration string ("60s", "5m", "24h") or a number
// of seconds.
func durationValue(raw interface{}) (time.Duration, error) {
	switch v := raw.(type) {
	case string:
		s := strings.TrimSpace(v)
		if seconds, err := strconv.ParseInt(s, 10, 64); err == nil {
			return time.Duration(seconds) * time.Second, nil
		}
		d, err := time.ParseDuration(s)
		if err != nil {
			return 0, fmt.Errorf("expected a duration such as 60s, 5m or 24h, got %q", v)
		}
		return d, nil
	case int:
		return time.Duration(v) * time.Second, nil
	case int64:
		return time.Duration(v) * time.Second, nil
	case float64:
		return time.Duration(v * float64(time.Second)), nil
	}
	return 0, fmt.Errorf("expected a duration such as 60s, got %T", raw)
}
