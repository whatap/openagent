package client

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	configPkg "open-agent/pkg/config"
)

func writeCredFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatalf("failed to write credential file: %v", err)
	}
	return path
}

// startAuthCheckServer returns a server that responds 200 only when the
// incoming Authorization header matches want.
func startAuthCheckServer(t *testing.T, want string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("metric_total 1\n"))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAuthorization_BearerFromFile(t *testing.T) {
	tokenFile := writeCredFile(t, t.TempDir(), "token", "my-api-token\n")
	srv := startAuthCheckServer(t, "Bearer my-api-token")

	c := GetInstance()
	body, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", nil, nil, &configPkg.AuthorizationConfig{
		CredentialsFile: tokenFile,
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected bearer-from-file request to succeed, got error: %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
}

func TestAuthorization_BearerFromEnv(t *testing.T) {
	t.Setenv("OPENAGENT_SCRAPE_TOKEN", "env-token")
	srv := startAuthCheckServer(t, "Bearer env-token")

	c := GetInstance()
	_, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", nil, nil, &configPkg.AuthorizationConfig{
		CredentialsEnv: "OPENAGENT_SCRAPE_TOKEN",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected bearer-from-env request to succeed, got error: %v", err)
	}
}

func TestAuthorization_InlineCredentialsAndCustomType(t *testing.T) {
	srv := startAuthCheckServer(t, "Token abc123")

	c := GetInstance()
	_, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", nil, nil, &configPkg.AuthorizationConfig{
		Type:        "Token",
		Credentials: "abc123",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected inline-credentials request to succeed, got error: %v", err)
	}
}

func TestAuthorization_DefaultTypeIsBearer(t *testing.T) {
	srv := startAuthCheckServer(t, "Bearer tok")

	c := GetInstance()
	_, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", nil, nil, &configPkg.AuthorizationConfig{
		Credentials: "tok",
	}, 5*time.Second)
	if err != nil {
		t.Fatalf("expected default Bearer scheme, got error: %v", err)
	}
}

func TestAuthorization_TakesPrecedenceOverBasicAuth(t *testing.T) {
	dir := t.TempDir()
	userFile := writeCredFile(t, dir, "user", "ignored-user")
	passFile := writeCredFile(t, dir, "pass", "ignored-pass")
	srv := startAuthCheckServer(t, "Bearer winner")

	c := GetInstance()
	_, err := c.ExecuteGetWithAuth(srv.URL+"/metrics",
		nil,
		&configPkg.BasicAuthConfig{UsernameFile: userFile, PasswordFile: passFile},
		&configPkg.AuthorizationConfig{Credentials: "winner"},
		5*time.Second)
	if err != nil {
		t.Fatalf("expected authorization to take precedence over basic auth, got error: %v", err)
	}
}

func TestBasicAuth_FromFiles(t *testing.T) {
	dir := t.TempDir()
	userFile := writeCredFile(t, dir, "user", "scrape-user\n")
	passFile := writeCredFile(t, dir, "pass", "scrape-pass\n")

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "scrape-user" || pass != "scrape-pass" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Write([]byte("metric_total 1\n"))
	}))
	defer srv.Close()

	c := GetInstance()
	body, err := c.ExecuteGetWithAuth(srv.URL+"/metrics", nil, &configPkg.BasicAuthConfig{
		UsernameFile: userFile,
		PasswordFile: passFile,
	}, nil, 5*time.Second)
	if err != nil {
		t.Fatalf("expected file-based basic auth request to succeed, got error: %v", err)
	}
	if body == "" {
		t.Fatalf("expected non-empty body")
	}
}
