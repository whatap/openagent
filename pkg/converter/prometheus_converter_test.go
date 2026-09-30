package converter

import (
	"fmt"
	"math"
	"reflect"
	"strings"
	"testing"

	"github.com/prometheus/common/expfmt"
	whatapio "github.com/whatap/golib/io"
	"github.com/whatap/golib/lang/pack"
	"github.com/whatap/golib/lang/value"
	"github.com/whatap/golib/util/compressutil"

	"open-agent/pkg/model"
)

func TestConvertTextMetadata_TypeWithoutHelp(t *testing.T) {
	const input = "# TYPE temperature_celsius gauge\ntemperature_celsius{room=\"office\"} 21.5 1700000000123\n"
	var parser expfmt.TextParser
	if _, err := parser.TextToMetricFamilies(strings.NewReader(input)); err != nil {
		t.Fatalf("invalid Prometheus fixture: %v", err)
	}

	result, err := ConvertWithTimestamp(input, testTS)
	if err != nil {
		t.Fatalf("ConvertWithTimestamp error: %v", err)
	}
	metric := mustSingle(t, result.GetOpenMxList(), "temperature_celsius", "room", "office")
	if metric.Value != 21.5 || metric.Timestamp != 1700000000123 || len(metric.Labels) != 1 {
		t.Fatalf("sample changed: %+v", metric)
	}

	help := result.GetOpenMxHelpList()
	if len(help) != 1 {
		t.Fatalf("metadata records = %d, want 1 for TYPE without HELP", len(help))
	}
	if help[0].Metric != "temperature_celsius" || help[0].Get("type") != "gauge" || help[0].Get("help") != "" {
		t.Errorf("metadata = %+v, want temperature_celsius/gauge with no invented HELP", help[0])
	}
}

func TestConvertTextMetadata_SamplesWithoutComments(t *testing.T) {
	const input = "queue_depth{queue=\"fast\"} 3\nqueue_depth{queue=\"slow\"} 7 1700000000123\n"
	var parser expfmt.TextParser
	if _, err := parser.TextToMetricFamilies(strings.NewReader(input)); err != nil {
		t.Fatalf("invalid Prometheus fixture: %v", err)
	}
	result, err := ConvertWithTimestamp(input, testTS)
	if err != nil {
		t.Fatalf("ConvertWithTimestamp error: %v", err)
	}
	if len(result.GetOpenMxList()) != 2 {
		t.Fatalf("sample count = %d, want 2", len(result.GetOpenMxList()))
	}
	fast := mustSingle(t, result.GetOpenMxList(), "queue_depth", "queue", "fast")
	slow := mustSingle(t, result.GetOpenMxList(), "queue_depth", "queue", "slow")
	if fast.Value != 3 || fast.Timestamp != testTS || slow.Value != 7 || slow.Timestamp != 1700000000123 {
		t.Fatalf("samples changed: fast=%+v slow=%+v", fast, slow)
	}
	help := result.GetOpenMxHelpList()
	if len(help) != 1 {
		t.Fatalf("metadata records = %d, want 1 for repeated samples without comments", len(help))
	}
	if help[0].Metric != "queue_depth" || help[0].Get("type") != "untyped" || help[0].Get("help") != "" {
		t.Errorf("metadata = %+v, want queue_depth/untyped with no invented HELP", help[0])
	}
}

type textMetadata struct {
	help     string
	typeName string
}

func assertTextMetadata(t *testing.T, records []*model.OpenMxHelp, want map[string]textMetadata) {
	t.Helper()
	if len(records) != len(want) {
		t.Errorf("metadata records = %d, want %d", len(records), len(want))
	}
	seen := make(map[string]bool)
	for _, record := range records {
		if seen[record.Metric] {
			t.Errorf("duplicate metadata for %q", record.Metric)
		}
		seen[record.Metric] = true
		expected, ok := want[record.Metric]
		if !ok {
			t.Errorf("unexpected metadata: %+v", record)
			continue
		}
		if record.Get("help") != expected.help || record.Get("type") != expected.typeName {
			t.Errorf("metadata for %q = %v, want help=%q type=%q", record.Metric, record.Property, expected.help, expected.typeName)
		}
	}
	for name := range want {
		if !seen[name] {
			t.Errorf("missing metadata for %q", name)
		}
	}
}

func convertValidTextFixture(t *testing.T, input string) *model.ConversionResult {
	t.Helper()
	var parser expfmt.TextParser
	if _, err := parser.TextToMetricFamilies(strings.NewReader(input)); err != nil {
		t.Fatalf("invalid Prometheus fixture: %v", err)
	}
	result, err := ConvertWithTimestamp(input, testTS)
	if err != nil {
		t.Fatalf("ConvertWithTimestamp error: %v", err)
	}
	return result
}

func TestConvertTextMetadata_MergesHelpAndType(t *testing.T) {
	tests := []struct {
		name     string
		comments string
		typeName string
	}{
		{"help_then_type", "# HELP requests_total Total requests\n# TYPE requests_total counter\n", "counter"},
		{"type_then_help", "# TYPE requests_total counter\n# HELP requests_total Total requests\n", "counter"},
		{"help_only", "# HELP requests_total Total requests\n", "untyped"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertValidTextFixture(t, tt.comments+"requests_total 42\n")
			assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
				"requests_total": {help: "Total requests", typeName: tt.typeName},
			})
		})
	}
}

func TestConvertTextMetadata_EmptyHelp(t *testing.T) {
	for _, comments := range []string{
		"# HELP queue_depth \n",
		"# HELP queue_depth\n",
		"# TYPE queue_depth untyped\n# HELP queue_depth \n",
	} {
		t.Run(comments, func(t *testing.T) {
			result := convertValidTextFixture(t, comments+"queue_depth 1\n")
			records := result.GetOpenMxHelpList()
			assertTextMetadata(t, records, map[string]textMetadata{"queue_depth": {typeName: "untyped"}})
			if len(records) != 1 {
				t.Fatal("expected one metadata record")
			}
			if value, ok := records[0].Property["help"]; !ok || value != "" {
				t.Errorf("explicit empty HELP missing: %v", records[0].Property)
			}
		})
	}
}

func TestConvertTextMetadata_KnownFamilies(t *testing.T) {
	tests := []struct {
		name     string
		typeName string
		samples  string
	}{
		{"histogram", "histogram", "latency_seconds_bucket{le=\"0.5\"} 2\nlatency_seconds_bucket{le=\"+Inf\"} 3\nlatency_seconds_sum 1.2\nlatency_seconds_count 3\n"},
		{"summary", "summary", "latency_seconds{quantile=\"0.5\"} 0.2\nlatency_seconds{quantile=\"0.9\"} 0.8\nlatency_seconds_sum 1.2\nlatency_seconds_count 3\n"},
	}
	for _, tt := range tests {
		for _, help := range []string{"", "Latency in seconds"} {
			t.Run(tt.name+"/"+help, func(t *testing.T) {
				input := "# TYPE latency_seconds " + tt.typeName + "\n"
				if help != "" {
					input += "# HELP latency_seconds " + help + "\n"
				}
				result := convertValidTextFixture(t, input+tt.samples)
				if len(result.GetOpenMxList()) != 4 {
					t.Fatalf("sample count = %d, want 4", len(result.GetOpenMxList()))
				}
				assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
					"latency_seconds": {help: help, typeName: tt.typeName},
				})
			})
		}
	}
}

func TestConvertTextMetadata_DoesNotGuessFamilies(t *testing.T) {
	const input = "# TYPE queue gauge\n" +
		"queue_count 1\nqueue_sum 2\nqueue_bucket 3\n" +
		"unknown_count 4\nunknown_sum 5\nunknown_bucket 6\n" +
		"# TYPE latency summary\nlatency_bucket 7\n" +
		"# HELP described Just a description\ndescribed_count 8\n"
	result := convertValidTextFixture(t, input)
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
		"queue":           {typeName: "gauge"},
		"queue_count":     {typeName: "untyped"},
		"queue_sum":       {typeName: "untyped"},
		"queue_bucket":    {typeName: "untyped"},
		"unknown_count":   {typeName: "untyped"},
		"unknown_sum":     {typeName: "untyped"},
		"unknown_bucket":  {typeName: "untyped"},
		"latency":         {typeName: "summary"},
		"latency_bucket":  {typeName: "untyped"},
		"described":       {help: "Just a description", typeName: "untyped"},
		"described_count": {typeName: "untyped"},
	})
}

func TestConvertTextMetadata_IgnoresUnrelatedOrMalformedComments(t *testing.T) {
	for _, comment := range []string{
		"# 123", "# EOF", "# HELPful ghost Not a directive", "# TYPEwriter ghost gauge",
		"# HELP invalid-name Bad name", "# TYPE invalid-name gauge", "# TYPE invalid_type not_a_type",
		"# TYPE extra_type gauge extra", "# HELP", "# TYPE", "# TYPE missing_type",
	} {
		t.Run(comment, func(t *testing.T) {
			result, err := ConvertWithTimestamp(comment+"\nvalid_metric 1\n", testTS)
			if err != nil {
				t.Fatalf("ConvertWithTimestamp error: %v", err)
			}
			assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
				"valid_metric": {typeName: "untyped"},
			})
		})
	}
}

func TestConvertTextMetadata_DirectiveWhitespace(t *testing.T) {
	const input = "	#	TYPE	process:requests_total	counter\n" +
		"#HELP	process:requests_total	Total  requests\nprocess:requests_total 2\n"
	result := convertValidTextFixture(t, input)
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
		"process:requests_total": {help: "Total  requests", typeName: "counter"},
	})
}

func TestConvertTextMetadata_NoFallbackForMalformedSamples(t *testing.T) {
	for _, sample := range []string{
		"invalid-name 1", "123metric 1", "{label=\"x\"} 1", "bad_value nope", "missing_value",
		"bad_brace{label=\"x\" 1", "bad_label{label} 1", "unquoted_label{label=x} 1",
		"bad_timestamp 1 nope", "extra_fields 1 2 3",
	} {
		t.Run(sample, func(t *testing.T) {
			var parser expfmt.TextParser
			if _, err := parser.TextToMetricFamilies(strings.NewReader(sample + "\n")); err == nil {
				t.Fatal("fixture must be malformed Prometheus text")
			}
			result, err := ConvertWithTimestamp(sample+"\nvalid_metric 2\n", testTS)
			if err != nil {
				t.Fatalf("ConvertWithTimestamp error: %v", err)
			}
			assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
				"valid_metric": {typeName: "untyped"},
			})
		})
	}
}

func TestConvertTextMetadata_ValidSampleAfterMalformedSample(t *testing.T) {
	result, err := ConvertWithTimestamp("recovered{broken} 1\nrecovered{label=\"ok\"} 2\n", testTS)
	if err != nil {
		t.Fatalf("ConvertWithTimestamp error: %v", err)
	}
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
		"recovered": {typeName: "untyped"},
	})
}

func TestConvertTextMetadata_MixedCatalogPackEncoding(t *testing.T) {
	const input = "# HELP requests_total Total requests\n# TYPE requests_total counter\n" +
		"requests_total{method=\"GET\"} 42 1700000000123\n" +
		"# TYPE temperature_celsius gauge\ntemperature_celsius 21.5\n" +
		"queue_depth{queue=\"fast\"} 3\nqueue_depth{queue=\"slow\"} 7\n" +
		"# TYPE latency_seconds histogram\nlatency_seconds_bucket{le=\"+Inf\"} 2\n" +
		"latency_seconds_sum 0.5\nlatency_seconds_count 2\n" +
		"# HELP latency_seconds Duration in seconds\n" +
		"# HELP empty_help \nempty_help 1\n"
	result := convertValidTextFixture(t, input)
	want := map[string]textMetadata{
		"requests_total":      {help: "Total requests", typeName: "counter"},
		"temperature_celsius": {typeName: "gauge"},
		"queue_depth":         {typeName: "untyped"},
		"latency_seconds":     {help: "Duration in seconds", typeName: "histogram"},
		"empty_help":          {typeName: "untyped"},
	}
	assertTextMetadata(t, result.GetOpenMxHelpList(), want)
	wantSamples := []*model.OpenMx{
		{Metric: "requests_total", Timestamp: 1700000000123, Value: 42, Labels: []model.Label{{Key: "method", Value: "GET"}}},
		{Metric: "temperature_celsius", Timestamp: testTS, Value: 21.5, Labels: []model.Label{}},
		{Metric: "queue_depth", Timestamp: testTS, Value: 3, Labels: []model.Label{{Key: "queue", Value: "fast"}}},
		{Metric: "queue_depth", Timestamp: testTS, Value: 7, Labels: []model.Label{{Key: "queue", Value: "slow"}}},
		{Metric: "latency_seconds_bucket", Timestamp: testTS, Value: 2, Labels: []model.Label{{Key: "le", Value: "+Inf"}}},
		{Metric: "latency_seconds_sum", Timestamp: testTS, Value: 0.5, Labels: []model.Label{}},
		{Metric: "latency_seconds_count", Timestamp: testTS, Value: 2, Labels: []model.Label{}},
		{Metric: "empty_help", Timestamp: testTS, Value: 1, Labels: []model.Label{}},
	}
	if !reflect.DeepEqual(result.GetOpenMxList(), wantSamples) {
		t.Errorf("sample values, labels, timestamps or order changed: got %+v", result.GetOpenMxList())
	}
	assertTextMetadataPackEncoding(t, result.GetOpenMxHelpList(), want, true)
}

func assertTextMetadataPackEncoding(t *testing.T, records []*model.OpenMxHelp, want map[string]textMetadata, compressed bool) {
	t.Helper()
	encoded := model.NewOpenMxHelpPack().SetRecords(records)
	out := whatapio.NewDataOutputX()
	encoded.Write(out)

	// Decode the real sender wire format using golib's typed MapValue reader.
	// OpenMxHelp.Read has a pre-existing value-tag bug; do not hide that by
	// changing the model as part of this text-converter regression.
	in := whatapio.NewDataInputX(out.ToByteArray())
	new(pack.AbstractPack).Read(in)
	zip := in.ReadByte()
	if (zip == 1) != compressed || zip > 1 {
		t.Fatalf("compression flag = %d, want compressed=%v", zip, compressed)
	}
	payload := in.ReadBlob()
	if in.Available() != 0 {
		t.Fatal("unexpected bytes after help pack")
	}
	if zip == 1 {
		var err error
		payload, err = compressutil.UnZip(payload)
		if err != nil {
			t.Fatalf("help pack decompression failed: %v", err)
		}
	}
	in = whatapio.NewDataInputX(payload)
	if version := in.ReadByte(); version != 0 {
		t.Fatalf("help pack version = %d, want 0", version)
	}
	count := int(in.ReadShort())
	if count != len(records) {
		t.Fatalf("encoded records = %d, want %d", count, len(records))
	}
	decoded := make([]*model.OpenMxHelp, 0, count)
	for i := 0; i < count; i++ {
		if version := in.ReadByte(); version != 0 {
			t.Fatalf("help record version = %d, want 0", version)
		}
		record := model.NewOpenMxHelp(in.ReadText())
		properties, ok := value.ReadValue(in).(*value.MapValue)
		if !ok {
			t.Fatal("help properties are not a typed MapValue")
		}
		for keys := properties.Keys(); keys.HasMoreElements(); {
			key := keys.NextString()
			record.Put(key, properties.GetString(key))
		}
		decoded = append(decoded, record)
	}
	if in.Available() != 0 {
		t.Fatal("unexpected bytes after help records")
	}
	assertTextMetadata(t, decoded, want)
	properties := make(map[string]map[string]string)
	for _, record := range records {
		properties[record.Metric] = record.Property
	}
	for _, record := range decoded {
		if !reflect.DeepEqual(record.Property, properties[record.Metric]) {
			t.Errorf("properties changed during serialization for %q: %v", record.Metric, record.Property)
		}
	}
}

func TestConvertTextMetadata_SampleOnlyPackEncoding(t *testing.T) {
	result := convertValidTextFixture(t, "small 1\n")
	assertTextMetadataPackEncoding(t, result.GetOpenMxHelpList(), map[string]textMetadata{
		"small": {typeName: "untyped"},
	}, false)
}

func TestConvertTextMetadata_ExactDeclarationWins(t *testing.T) {
	const input = "# TYPE latency_count counter\n# HELP latency_count Independent counter\n" +
		"# TYPE latency histogram\nlatency_count 2\n"
	result := convertValidTextFixture(t, input)
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{
		"latency":       {typeName: "histogram"},
		"latency_count": {help: "Independent counter", typeName: "counter"},
	})
}

func TestConvertTextMetadata_TwentyTypesWithoutHelp(t *testing.T) {
	var input strings.Builder
	want := make(map[string]textMetadata)
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("device_metric_%d", i)
		typeName := []string{"counter", "gauge"}[i%2]
		fmt.Fprintf(&input, "# TYPE %s %s\n%s{port=\"one\"} %d\n%s{port=\"two\"} %d\n", name, typeName, name, i, name, i+1)
		want[name] = textMetadata{typeName: typeName}
	}
	result := convertValidTextFixture(t, input.String())
	if len(result.GetOpenMxList()) != 40 {
		t.Fatalf("sample count = %d, want 40", len(result.GetOpenMxList()))
	}
	assertTextMetadata(t, result.GetOpenMxHelpList(), want)
	assertTextMetadataPackEncoding(t, result.GetOpenMxHelpList(), want, true)
}

func TestConvertTextMetadata_PreservesSpecialValues(t *testing.T) {
	const input = "special{kind=\"positive\"} +Inf 0\nspecial{kind=\"negative\"} -Inf -1\nspecial{kind=\"nan\"} NaN\n"
	result := convertValidTextFixture(t, input)
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{"special": {typeName: "untyped"}})
	positive := mustSingle(t, result.GetOpenMxList(), "special", "kind", "positive")
	negative := mustSingle(t, result.GetOpenMxList(), "special", "kind", "negative")
	nan := mustSingle(t, result.GetOpenMxList(), "special", "kind", "nan")
	if !math.IsInf(positive.Value, 1) || positive.Timestamp != 0 || !math.IsInf(negative.Value, -1) || negative.Timestamp != -1 || !math.IsNaN(nan.Value) || nan.Timestamp != testTS {
		t.Errorf("special values or timestamps changed: positive=%+v negative=%+v nan=%+v", positive, negative, nan)
	}
}

func TestConvertTextMetadata_EmptyScrapeAndNoCrossScrapeState(t *testing.T) {
	for _, input := range []string{"", "\n# An ordinary comment\n# EOF\n"} {
		result := convertValidTextFixture(t, input)
		if len(result.GetOpenMxList()) != 0 || len(result.GetOpenMxHelpList()) != 0 {
			t.Errorf("empty scrape produced records: %+v", result)
		}
	}
	convertValidTextFixture(t, "# HELP local Described\n# TYPE local gauge\nlocal 1\n")
	result := convertValidTextFixture(t, "local 2\n")
	assertTextMetadata(t, result.GetOpenMxHelpList(), map[string]textMetadata{"local": {typeName: "untyped"}})
}
