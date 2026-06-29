package client

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// --- test PKI helpers -------------------------------------------------------

type certPair struct {
	certPEM []byte
	keyPEM  []byte
	cert    tls.Certificate
	leaf    *x509.Certificate
}

func mustGenCA(t *testing.T) (*x509.Certificate, *ecdsa.PrivateKey) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("ca key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "test-ca"},
		NotBefore:             time.Unix(0, 0),
		NotAfter:              time.Unix(1<<31-1, 0),
		IsCA:                  true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatalf("ca cert: %v", err)
	}
	caCert, _ := x509.ParseCertificate(der)
	return caCert, key
}

func mustGenLeaf(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, cn string, server bool) certPair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatalf("leaf key: %v", err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: cn},
		NotBefore:    time.Unix(0, 0),
		NotAfter:     time.Unix(1<<31-1, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
	}
	if server {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}
		tmpl.DNSNames = []string{"localhost"}
		tmpl.IPAddresses = []net.IP{net.ParseIP("127.0.0.1")}
	} else {
		tmpl.ExtKeyUsage = []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatalf("leaf cert: %v", err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyDER, _ := x509.MarshalECPrivateKey(key)
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("x509 keypair: %v", err)
	}
	leaf, _ := x509.ParseCertificate(der)
	return certPair{certPEM: certPEM, keyPEM: keyPEM, cert: tlsCert, leaf: leaf}
}

func writeFile(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return p
}

// startMTLSServer starts an HTTPS server that REQUIRES a client certificate
// signed by caCert (mTLS), mimicking targets like etcd. It serves a fixed body
// at /metrics.
func startMTLSServer(t *testing.T, serverCert tls.Certificate, caCert *x509.Certificate) *httptest.Server {
	t.Helper()
	clientCAs := x509.NewCertPool()
	clientCAs.AddCert(caCert)

	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("etcd_up 1\n"))
	}))
	srv.TLS = &tls.Config{
		Certificates: []tls.Certificate{serverCert},
		ClientAuth:   tls.RequireAndVerifyClientCert,
		ClientCAs:    clientCAs,
	}
	srv.StartTLS()
	t.Cleanup(srv.Close)
	return srv
}

// --- tests ------------------------------------------------------------------

// TestMTLS_InsecureSkipVerifyTrue_PresentsClientCert verifies the core fix:
// even with insecureSkipVerify=true, the client certificate must still be
// presented so that mTLS targets (etcd) accept the connection.
func TestMTLS_InsecureSkipVerifyTrue_PresentsClientCert(t *testing.T) {
	ca, caKey := mustGenCA(t)
	server := mustGenLeaf(t, ca, caKey, "localhost", true)
	clientC := mustGenLeaf(t, ca, caKey, "openagent-client", false)

	srv := startMTLSServer(t, server.cert, ca)

	dir := t.TempDir()
	caFile := writeFile(t, dir, "ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))
	certFile := writeFile(t, dir, "client.crt", clientC.certPEM)
	keyFile := writeFile(t, dir, "client.key", clientC.keyPEM)
	_ = caFile

	c := GetInstance()
	body, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", &TLSConfig{
		InsecureSkipVerify: true, // skip server verification, but client cert MUST still be sent
		CertFile:           certFile,
		KeyFile:            keyFile,
	}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("expected mTLS request to succeed with insecureSkipVerify=true, got error: %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
}

// TestMTLS_InsecureSkipVerifyFalse_WithCAFile verifies server verification via
// caFile together with client cert presentation (full mTLS).
func TestMTLS_InsecureSkipVerifyFalse_WithCAFile(t *testing.T) {
	ca, caKey := mustGenCA(t)
	server := mustGenLeaf(t, ca, caKey, "localhost", true)
	clientC := mustGenLeaf(t, ca, caKey, "openagent-client", false)

	srv := startMTLSServer(t, server.cert, ca)

	dir := t.TempDir()
	caFile := writeFile(t, dir, "ca.crt", pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: ca.Raw}))
	certFile := writeFile(t, dir, "client.crt", clientC.certPEM)
	keyFile := writeFile(t, dir, "client.key", clientC.keyPEM)

	c := GetInstance()
	body, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", &TLSConfig{
		InsecureSkipVerify: false,
		ServerName:         "localhost",
		CAFile:             caFile,
		CertFile:           certFile,
		KeyFile:            keyFile,
	}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("expected full-mTLS request to succeed, got error: %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
}

// TestMTLS_NoClientCert_Fails confirms the test server truly enforces mTLS:
// without a client cert the request must fail (guards against false positives).
func TestMTLS_NoClientCert_Fails(t *testing.T) {
	ca, caKey := mustGenCA(t)
	server := mustGenLeaf(t, ca, caKey, "localhost", true)

	srv := startMTLSServer(t, server.cert, ca)

	c := GetInstance()
	_, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", &TLSConfig{
		InsecureSkipVerify: true, // skip server verify, but provide NO client cert
	}, nil, 5*time.Second)
	if err == nil {
		t.Fatalf("expected request to fail without client certificate, but it succeeded")
	}
}
