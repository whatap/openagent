package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	// File and path constants
	localCAPath              = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	localServiceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	kubernetesServiceHost    = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePort    = "KUBERNETES_SERVICE_PORT"
)

// TLSConfig represents TLS configuration options
type TLSConfig struct {
	InsecureSkipVerify bool
}

// HTTPClient is responsible for making HTTP requests to scrape metrics from targets
type HTTPClient struct {
	client *http.Client
}

var instance *HTTPClient

// GetInstance returns the singleton instance of HTTPClient
func GetInstance() *HTTPClient {
	if instance == nil {
		instance = &HTTPClient{
			client: &http.Client{
				Timeout: 10 * time.Second,
			},
		}
		// Try to configure TLS with Kubernetes CA cert
		if cert, err := loadKubernetesCACert(); err == nil {
			rootCAs, _ := x509.SystemCertPool()
			if rootCAs == nil {
				rootCAs = x509.NewCertPool()
			}
			rootCAs.AddCert(cert)
			instance.client.Transport = &http.Transport{
				TLSClientConfig: &tls.Config{
					RootCAs: rootCAs,
				},
			}
		}
	}
	return instance
}

// FormatURL ensures the URL has a scheme (https:// by default)
func FormatURL(target string) string {
	if target == "" {
		return target
	}

	target = strings.TrimSpace(target)
	lower := strings.ToLower(target)

	if !strings.HasPrefix(lower, "http://") && !strings.HasPrefix(lower, "https://") {
		return "https://" + target
	}

	return target
}

// GetKubeServiceEndpoint constructs the Kubernetes service endpoint URL
func GetKubeServiceEndpoint(customHost, customPort string) string {
	host := os.Getenv(kubernetesServiceHost)
	if customHost != "" {
		host = customHost
	}

	port := os.Getenv(kubernetesServicePort)
	if customPort != "" {
		port = customPort
	}

	if host == "" || port == "" {
		return ""
	}

	return fmt.Sprintf("https://%s:%s", host, port)
}

// GetServiceAccountToken reads the service account token from the file
func GetServiceAccountToken() (string, error) {
	data, err := ioutil.ReadFile(localServiceAccountToken)
	if err != nil {
		return "", fmt.Errorf("error reading service account token: %v", err)
	}
	return string(data), nil
}

// loadKubernetesCACert loads the Kubernetes CA certificate
func loadKubernetesCACert() (*x509.Certificate, error) {
	data, err := ioutil.ReadFile(localCAPath)
	if err != nil {
		return nil, fmt.Errorf("error reading CA certificate: %v", err)
	}

	certs, err := x509.ParseCertificates(data)
	if err != nil {
		return nil, fmt.Errorf("error parsing CA certificate: %v", err)
	}

	if len(certs) == 0 {
		return nil, fmt.Errorf("no certificates found in CA file")
	}

	return certs[0], nil
}

// ExecuteGet performs an HTTP GET request to the specified URL
func (c *HTTPClient) ExecuteGet(targetURL string) (string, error) {
	return c.ExecuteGetWithTLSConfig(targetURL, nil)
}

// ExecuteGetWithTLSConfig performs an HTTP GET request to the specified URL with custom TLS configuration
func (c *HTTPClient) ExecuteGetWithTLSConfig(targetURL string, tlsConfig *TLSConfig) (string, error) {
	formattedURL := FormatURL(targetURL)

	req, err := http.NewRequest("GET", formattedURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// Try to add service account token for authentication
	token, err := GetServiceAccountToken()
	if err == nil {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	req.Header.Set("Accept", "application/json")

	// Use the default client or create a new one with custom TLS config
	client := c.client
	if tlsConfig != nil {
		// Create a custom transport with the specified TLS config
		transport := &http.Transport{
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: tlsConfig.InsecureSkipVerify,
			},
		}

		// If we have a Kubernetes CA cert and InsecureSkipVerify is false, add it to the cert pool
		if !tlsConfig.InsecureSkipVerify {
			if cert, err := loadKubernetesCACert(); err == nil {
				rootCAs, _ := x509.SystemCertPool()
				if rootCAs == nil {
					rootCAs = x509.NewCertPool()
				}
				rootCAs.AddCert(cert)
				transport.TLSClientConfig.RootCAs = rootCAs
			}
		}

		// Create a new client with the custom transport
		client = &http.Client{
			Timeout:   c.client.Timeout,
			Transport: transport,
		}
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("error executing request: %v", err)
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	return string(body), nil
}
