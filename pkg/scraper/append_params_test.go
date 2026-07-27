package scraper

import (
	"net/url"
	"testing"
)

// sortedQuery parses a URL and returns its query in a stable, comparable form.
func mustQuery(t *testing.T, rawURL string) url.Values {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("failed to parse result URL %q: %v", rawURL, err)
	}
	return u.Query()
}

func TestAppendParams(t *testing.T) {
	tests := []struct {
		name    string
		params  map[string][]string
		baseURL string
		// want maps each query key to its expected values (order-independent).
		want map[string][]string
	}{
		{
			name:    "no params returns url unchanged",
			params:  nil,
			baseURL: "http://10.0.0.5:9100/metrics",
			want:    map[string][]string{},
		},
		{
			name:    "params added to bare url",
			params:  map[string][]string{"module": {"http_2xx"}, "target": {"https://example.com"}},
			baseURL: "http://10.0.0.5:9100/probe",
			want:    map[string][]string{"module": {"http_2xx"}, "target": {"https://example.com"}},
		},
		{
			// Regression: ServiceDiscovery already baked params into the URL.
			// appendParams must NOT duplicate them.
			name:    "params already present are not duplicated",
			params:  map[string][]string{"module": {"http_2xx"}, "target": {"https://example.com"}},
			baseURL: "http://10.0.0.5:9100/probe?module=http_2xx&target=https%3A%2F%2Fexample.com",
			want:    map[string][]string{"module": {"http_2xx"}, "target": {"https://example.com"}},
		},
		{
			name:    "multi-value param with one already present adds only the missing value",
			params:  map[string][]string{"collect": {"cpu", "mem"}},
			baseURL: "http://10.0.0.5:9100/metrics?collect=cpu",
			want:    map[string][]string{"collect": {"cpu", "mem"}},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st := &ScraperTask{Params: tc.params}
			got, err := st.appendParams(tc.baseURL)
			if err != nil {
				t.Fatalf("appendParams returned error: %v", err)
			}

			gotQ := mustQuery(t, got)
			for key, wantVals := range tc.want {
				gotVals := gotQ[key]
				if len(gotVals) != len(wantVals) {
					t.Errorf("key %q: got %d values %v, want %d values %v", key, len(gotVals), gotVals, len(wantVals), wantVals)
					continue
				}
				// order-independent value comparison
				for _, wv := range wantVals {
					if !paramValueExists(gotVals, wv) {
						t.Errorf("key %q: expected value %q missing in %v", key, wv, gotVals)
					}
				}
			}
			// Ensure no unexpected extra keys were produced.
			for key := range gotQ {
				if _, ok := tc.want[key]; !ok {
					t.Errorf("unexpected query key %q in result %q", key, got)
				}
			}
		})
	}
}
