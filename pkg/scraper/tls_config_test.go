package scraper

import "testing"

// TestParseTLSConfig_FullCertFields verifies that every TLS field is parsed from
// the raw tlsConfig map. This is a regression test for the bug where only
// insecureSkipVerify was honored and any applied certificate (caFile/caSecret,
// certFile/certSecret, keyFile/keySecret, serverName) was silently dropped.
func TestParseTLSConfig_FullCertFields(t *testing.T) {
	raw := map[string]interface{}{
		"insecureSkipVerify": false,
		"caFile":             "/etc/ssl/certs/etcd-tls/ca.pem",
		"certFile":           "/etc/ssl/certs/etcd-tls/cert.pem",
		"keyFile":            "/etc/ssl/certs/etcd-tls/key.pem",
		"serverName":         "etcd.kube-system.svc",
	}

	tc := parseTLSConfig(raw)
	if tc == nil {
		t.Fatal("parseTLSConfig returned nil for a non-nil map")
	}
	if tc.InsecureSkipVerify != false {
		t.Errorf("InsecureSkipVerify = %v, want false", tc.InsecureSkipVerify)
	}
	if tc.CAFile != "/etc/ssl/certs/etcd-tls/ca.pem" {
		t.Errorf("CAFile = %q, want the CA path", tc.CAFile)
	}
	if tc.CertFile != "/etc/ssl/certs/etcd-tls/cert.pem" {
		t.Errorf("CertFile = %q, want the cert path", tc.CertFile)
	}
	if tc.KeyFile != "/etc/ssl/certs/etcd-tls/key.pem" {
		t.Errorf("KeyFile = %q, want the key path", tc.KeyFile)
	}
	if tc.ServerName != "etcd.kube-system.svc" {
		t.Errorf("ServerName = %q, want etcd.kube-system.svc", tc.ServerName)
	}
}

// TestParseTLSConfig_SecretSelectors verifies that caSecret/certSecret/keySecret
// nested maps are parsed into SecretKeySelectors.
func TestParseTLSConfig_SecretSelectors(t *testing.T) {
	raw := map[string]interface{}{
		"caSecret":   map[string]interface{}{"name": "etcd-tls", "key": "ca.pem"},
		"certSecret": map[string]interface{}{"name": "etcd-tls", "key": "cert.pem", "namespace": "kube-system"},
		"keySecret":  map[string]interface{}{"name": "etcd-tls", "key": "key.pem"},
	}

	tc := parseTLSConfig(raw)
	if tc == nil {
		t.Fatal("parseTLSConfig returned nil for a non-nil map")
	}
	if tc.CASecret == nil || tc.CASecret.Name != "etcd-tls" || tc.CASecret.Key != "ca.pem" {
		t.Errorf("CASecret = %+v, want {etcd-tls ca.pem}", tc.CASecret)
	}
	if tc.CertSecret == nil || tc.CertSecret.Key != "cert.pem" || tc.CertSecret.Namespace != "kube-system" {
		t.Errorf("CertSecret = %+v, want cert.pem in kube-system", tc.CertSecret)
	}
	if tc.KeySecret == nil || tc.KeySecret.Key != "key.pem" {
		t.Errorf("KeySecret = %+v, want key.pem", tc.KeySecret)
	}
}

// TestParseTLSConfig_InsecureSkipVerify verifies the insecureSkipVerify path
// still works (the previously-only-supported field).
func TestParseTLSConfig_InsecureSkipVerify(t *testing.T) {
	tc := parseTLSConfig(map[string]interface{}{"insecureSkipVerify": true})
	if tc == nil || !tc.InsecureSkipVerify {
		t.Fatalf("InsecureSkipVerify not honored: %+v", tc)
	}
}

// TestParseTLSConfig_NilAndEmpty verifies behavior preservation: a nil map yields
// a nil config (default transport), a present-but-empty map yields an empty config.
func TestParseTLSConfig_NilAndEmpty(t *testing.T) {
	if tc := parseTLSConfig(nil); tc != nil {
		t.Errorf("parseTLSConfig(nil) = %+v, want nil", tc)
	}
	tc := parseTLSConfig(map[string]interface{}{})
	if tc == nil {
		t.Fatal("parseTLSConfig(empty map) = nil, want non-nil empty config")
	}
	if tc.InsecureSkipVerify || tc.CAFile != "" || tc.CASecret != nil {
		t.Errorf("empty map produced non-empty config: %+v", tc)
	}
}

// TestParseSecretKeySelector_NotAMap verifies non-map values return nil.
func TestParseSecretKeySelector_NotAMap(t *testing.T) {
	if got := parseSecretKeySelector("not-a-map"); got != nil {
		t.Errorf("parseSecretKeySelector(string) = %+v, want nil", got)
	}
	if got := parseSecretKeySelector(nil); got != nil {
		t.Errorf("parseSecretKeySelector(nil) = %+v, want nil", got)
	}
}
