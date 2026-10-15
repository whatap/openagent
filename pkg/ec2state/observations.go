package ec2state

import (
	"errors"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	dto "github.com/prometheus/client_model/go"

	"open-agent/pkg/model"
)

const (
	defaultMetricName        = "aws_ec2_instance_state"
	maxObservationAge        = 5 * time.Minute
	maxObservationsPerScrape = 4096
)

var instanceIDPattern = regexp.MustCompile(`^i-([0-9a-f]{8}|[0-9a-f]{17})$`)

var stateNames = map[int64]string{
	0: "pending", 16: "running", 32: "shutting-down",
	48: "terminated", 64: "stopping", 80: "stopped",
}

type observation struct {
	instanceID string
	stateCode  int64
	stateName  string
	observedAt int64
}

func settingOr(settings map[string]string, key, fallback string) string {
	if value := settings[key]; value != "" {
		return value
	}
	return fallback
}

func parseObservations(settings map[string]string, raw *model.ScrapeRawData) ([]observation, error) {
	families, err := decodeFamilies(raw)
	if err != nil {
		return nil, err
	}
	metricName := strings.TrimSpace(settings["ec2_state_metric_name"])
	if metricName == "" {
		metricName = defaultMetricName
	}
	family := families[metricName]
	if family == nil {
		return nil, nil
	}
	if len(family.Metric) > maxObservationsPerScrape {
		return nil, errors.New("EC2_STATE_INPUT: state sample count exceeds per-scrape limit")
	}
	if family.GetType() != dto.MetricType_GAUGE && family.GetType() != dto.MetricType_UNTYPED {
		return nil, errors.New("EC2_STATE_INPUT: state metric must be gauge or untyped")
	}
	instanceDefault, nameDefault, sourceDefault := "instanceId", "stateName", "label"
	if metricName == defaultMetricName {
		instanceDefault, nameDefault, sourceDefault = "instance_id", "state_name", "value"
	}
	instanceLabel := settingOr(settings, "ec2_state_instance_id_label", instanceDefault)
	codeLabel := settingOr(settings, "ec2_state_state_code_label", "stateCode")
	nameLabel := settingOr(settings, "ec2_state_state_name_label", nameDefault)
	codeSource := settingOr(settings, "ec2_state_state_code_source", sourceDefault)
	if codeSource != "label" && codeSource != "value" {
		return nil, errors.New("EC2_STATE_CONFIG: state code source must be label or value")
	}
	observations := make(map[string]observation)
	for _, metric := range family.Metric {
		var sample float64
		if family.GetType() == dto.MetricType_UNTYPED {
			if metric.GetUntyped() == nil || metric.GetUntyped().Value == nil {
				return nil, errors.New("EC2_STATE_INPUT: missing untyped state value")
			}
			sample = metric.GetUntyped().GetValue()
		} else {
			if metric.GetGauge() == nil || metric.GetGauge().Value == nil {
				return nil, errors.New("EC2_STATE_INPUT: missing gauge state value")
			}
			sample = metric.GetGauge().GetValue()
		}
		if math.IsNaN(sample) || math.IsInf(sample, 0) {
			return nil, errors.New("EC2_STATE_INPUT: non-finite state sample")
		}
		if codeSource == "label" {
			if sample == 0 {
				continue // Inactive series are not observations of an instance.
			}
			if sample != 1 {
				return nil, errors.New("EC2_STATE_INPUT: label-based state sample must be 0 or 1")
			}
		}
		labels := make(map[string]string)
		for _, label := range metric.Label {
			if _, duplicate := labels[label.GetName()]; duplicate {
				return nil, errors.New("EC2_STATE_INPUT: duplicate exporter label")
			}
			labels[label.GetName()] = label.GetValue()
		}
		var code int64
		if codeSource == "value" {
			if sample < 0 || sample > 80 || math.Trunc(sample) != sample {
				return nil, errors.New("EC2_STATE_INPUT: invalid numeric state code")
			}
			code = int64(sample)
		} else {
			code, err = strconv.ParseInt(labels[codeLabel], 10, 64)
			if err != nil {
				return nil, errors.New("EC2_STATE_INPUT: invalid state code label")
			}
		}
		instanceID, stateName := labels[instanceLabel], labels[nameLabel]
		expectedName, knownState := stateNames[code]
		if !instanceIDPattern.MatchString(instanceID) || !knownState || stateName != expectedName {
			return nil, errors.New("EC2_STATE_INPUT: missing or inconsistent instance state")
		}
		observedAt := raw.CollectionTime
		if metric.TimestampMs != nil {
			observedAt = metric.GetTimestampMs()
			if observedAt <= 0 || observedAt > raw.CollectionTime || raw.CollectionTime-observedAt > maxObservationAge.Milliseconds() {
				return nil, errors.New("EC2_STATE_INPUT: stale or future state timestamp")
			}
		}
		current := observation{instanceID: instanceID, stateCode: code, stateName: stateName, observedAt: observedAt}
		if previous, duplicate := observations[instanceID]; duplicate && previous != current {
			return nil, errors.New("EC2_STATE_INPUT: conflicting observations for one instance")
		}
		observations[instanceID] = current
	}
	// Validate the entire snapshot before queueing any destination's observation.
	keys := make([]string, 0, len(observations))
	for key := range observations {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	result := make([]observation, 0, len(keys))
	for _, key := range keys {
		result = append(result, observations[key])
	}
	return result, nil
}
