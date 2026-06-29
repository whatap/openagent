package scraper

import "testing"

// TestBuildTLSConfig_ParsesAllFields verifies that buildTLSConfig reads not only
// insecureSkipVerify but also the caFile/certFile/keyFile/serverName fields that
// the operator renders into scrape_config (previously these were dropped).
func TestBuildTLSConfig_ParsesAllFields(t *testing.T) {
	m := map[string]interface{}{
		"insecureSkipVerify": true,
		"serverName":         "localhost",
		"caFile":             "/etc/ssl/certs/etcd-client-cert/ca.crt",
		"certFile":           "/etc/ssl/certs/etcd-client-cert/healthcheck-client.crt",
		"keyFile":            "/etc/ssl/certs/etcd-client-cert/healthcheck-client.key",
	}

	got := buildTLSConfig(m)
	if got == nil {
		t.Fatal("expected non-nil TLSConfig")
	}
	if !got.InsecureSkipVerify {
		t.Errorf("InsecureSkipVerify: want true, got false")
	}
	if got.ServerName != "localhost" {
		t.Errorf("ServerName: want localhost, got %q", got.ServerName)
	}
	if got.CAFile != m["caFile"] {
		t.Errorf("CAFile: want %q, got %q", m["caFile"], got.CAFile)
	}
	if got.CertFile != m["certFile"] {
		t.Errorf("CertFile: want %q, got %q", m["certFile"], got.CertFile)
	}
	if got.KeyFile != m["keyFile"] {
		t.Errorf("KeyFile: want %q, got %q", m["keyFile"], got.KeyFile)
	}
}

func TestBuildTLSConfig_NilReturnsNil(t *testing.T) {
	if got := buildTLSConfig(nil); got != nil {
		t.Errorf("expected nil for nil map, got %+v", got)
	}
}
