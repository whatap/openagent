package client

import (
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestExecuteGetWithAuthResponseClosesOwnedTransport(t *testing.T) {
	t.Setenv("KUBECONFIG", filepath.Join(t.TempDir(), "missing-kubeconfig"))
	t.Setenv("KUBERNETES_SERVICE_HOST", "")
	t.Setenv("KUBERNETES_SERVICE_PORT", "")
	const requests = 16
	for _, tc := range []struct {
		name      string
		https     bool
		status    int
		tlsConfig *TLSConfig
	}{
		{"https_success", true, http.StatusOK, &TLSConfig{InsecureSkipVerify: true}},
		{"https_http_error", true, http.StatusInternalServerError, &TLSConfig{InsecureSkipVerify: true}},
		{"http_empty_tls_config", false, http.StatusOK, &TLSConfig{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			closed := make(chan struct{}, requests)
			srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte("metric_total 1\n"))
			}))
			srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
				if state == http.StateClosed {
					closed <- struct{}{}
				}
			}
			if tc.https {
				srv.StartTLS()
			} else {
				srv.Start()
			}
			t.Cleanup(srv.Close)

			c := &HTTPClient{client: &http.Client{Timeout: time.Second}}
			for i := 0; i < requests; i++ {
				body, contentType, err := c.ExecuteGetWithAuthResponse(srv.URL, tc.tlsConfig, nil, nil, time.Second)
				if tc.status == http.StatusOK {
					if err != nil {
						t.Fatalf("request %d: %v", i, err)
					}
					if string(body) != "metric_total 1\n" || contentType != "text/plain" {
						t.Fatalf("unexpected response: body=%q contentType=%q", body, contentType)
					}
				} else if err == nil || !strings.Contains(err.Error(), "HTTP error: 500") {
					t.Fatalf("request %d: expected HTTP 500 error, got %v", i, err)
				}
			}

			// Observe peer-side closure before server cleanup; a completed body
			// alone only returns the connection to the abandoned transport's pool.
			deadline := time.NewTimer(2 * time.Second)
			defer deadline.Stop()
			for n := 0; n < requests; n++ {
				select {
				case <-closed:
				case <-deadline.C:
					t.Fatalf("closed %d/%d connections; request-owned transports retained sockets", n, requests)
				}
			}
		})
	}
}

func TestExecuteGetWithAuthResponsePreservesSharedTransport(t *testing.T) {
	var connections atomic.Int32
	srv := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("metric_total 1\n"))
	}))
	srv.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	srv.Start()
	t.Cleanup(srv.Close)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	t.Cleanup(transport.CloseIdleConnections)
	c := &HTTPClient{client: &http.Client{Transport: transport, Timeout: time.Second}}
	for i := 0; i < 16; i++ {
		if _, _, err := c.ExecuteGetWithAuthResponse(srv.URL, nil, nil, nil, 0); err != nil {
			t.Fatalf("request %d: %v", i, err)
		}
	}
	if got := connections.Load(); got != 1 {
		t.Fatalf("shared transport opened %d connections; expected reuse of one connection", got)
	}
}
