package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	configPkg "open-agent/pkg/config"
	"open-agent/tools/util/logutil"
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

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// HTTPClient is responsible for making HTTP requests to scrape metrics from targets
type HTTPClient struct {
	client     *http.Client
	isMinikube bool
}

var instance *HTTPClient

// GetInstance returns the singleton instance of HTTPClient
func GetInstance() *HTTPClient {
	if instance == nil {
		instance = &HTTPClient{
			client: &http.Client{
				Timeout: 10 * time.Second,
			},
			isMinikube: false,
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

	// Log the request
	if configPkg.IsDebugEnabled() {
		if c.isMinikube {
			logutil.Debugf("HTTP_CLIENT", "HTTP Request (Minikube client): GET %s", formattedURL)
		} else {
			logutil.Debugf("HTTP_CLIENT", "HTTP Request: GET %s", formattedURL)
		}
	}

	req, err := http.NewRequest("GET", formattedURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// Skip token authentication for Minikube as it uses client certificates
	if !c.isMinikube {
		// Try to add service account token for authentication
		token, err := GetServiceAccountToken()
		if err == nil {
			req.Header.Set("Authorization", "Bearer "+token)
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("HTTP_CLIENT", "Added Authorization header with Bearer token")
			}
		} else {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("HTTP_CLIENT", "No service account token available: %v", err)
			}
		}
	} else {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "Skipping token authentication for Minikube")
		}
	}

	req.Header.Set("Accept", "application/json")

	// Use the default client or create a new one with custom TLS config
	client := c.client

	// For Minikube, we already have a client with the correct TLS config
	// We don't need to create a new one unless a custom TLS config is provided
	if c.isMinikube {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "Using existing Minikube client with client certificates")
		}
	} else if tlsConfig != nil {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "Using custom TLS config with InsecureSkipVerify=%v", tlsConfig.InsecureSkipVerify)
		}

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

				if configPkg.IsDebugEnabled() {
					logutil.Debugf("HTTP_CLIENT", "Added Kubernetes CA cert to root CA pool")
				}
			} else {
				if configPkg.IsDebugEnabled() {
					logutil.Debugf("HTTP_CLIENT", "Failed to load Kubernetes CA cert: %v", err)
				}
			}
		}

		// Create a new client with the custom transport
		client = &http.Client{
			Timeout:   c.client.Timeout,
			Transport: transport,
		}
	}

	// Log the request start time if debug is enabled
	startTime := time.Now()
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("HTTP_CLIENT", "Sending HTTP request to %s", formattedURL)
	}

	resp, err := client.Do(req)
	if err != nil {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "HTTP request failed: %v", err)
		}
		return "", fmt.Errorf("error executing request: %v", err)
	}
	defer resp.Body.Close()

	// Log the response if debug is enabled
	duration := time.Since(startTime)
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("HTTP_CLIENT", "HTTP Response: %d %s (took %v)", resp.StatusCode, resp.Status, duration)
		logutil.Debugf("HTTP_CLIENT", "Response Headers: %v", resp.Header)
	}

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "Error reading response body: %v", err)
		}
		return "", fmt.Errorf("error reading response body: %v", err)
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "HTTP error: %d %s", resp.StatusCode, resp.Status)
			logutil.Debugf("HTTP_CLIENT", "Response body: %s", string(body))
		}
		return "", fmt.Errorf("HTTP error: %d %s", resp.StatusCode, resp.Status)
	}

	// Log the response body length if debug is enabled
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("HTTP_CLIENT", "Response body length: %d bytes", len(body))
		// Log a preview of the response body (first 500 characters)
		preview := string(body)
		if len(preview) > 500 {
			preview = preview[:500] + "..."
		}
		logutil.Debugf("HTTP_CLIENT", "Response body preview: %s", preview)
	}

	return string(body), nil
}

// createMinikubeTLSConfig creates a TLS configuration with Minikube certificates
func createMinikubeTLSConfig(home string) (*tls.Config, error) {
	// Paths to certificate files
	caCertPath := filepath.Join(home, ".minikube", "ca.crt")
	clientCertPath := filepath.Join(home, ".minikube", "profiles", "minikube", "client.crt")
	clientKeyPath := filepath.Join(home, ".minikube", "profiles", "minikube", "client.key")

	logutil.Infof("HTTP_CLIENT", "Loading Minikube certificates from: CA=%s, Cert=%s, Key=%s", caCertPath, clientCertPath, clientKeyPath)

	// Check if files exist
	if _, err := os.Stat(caCertPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("CA certificate file does not exist: %s", caCertPath)
	}
	if _, err := os.Stat(clientCertPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Client certificate file does not exist: %s", clientCertPath)
	}
	if _, err := os.Stat(clientKeyPath); os.IsNotExist(err) {
		return nil, fmt.Errorf("Client key file does not exist: %s", clientKeyPath)
	}

	// Load CA cert
	caCert, err := ioutil.ReadFile(caCertPath)
	if err != nil {
		return nil, fmt.Errorf("error loading CA certificate: %v", err)
	}
	logutil.Infof("HTTP_CLIENT", "Successfully loaded CA certificate (%d bytes)", len(caCert))

	// Create CA cert pool and add the CA cert
	caCertPool := x509.NewCertPool()
	if ok := caCertPool.AppendCertsFromPEM(caCert); !ok {
		return nil, fmt.Errorf("failed to append CA certificate to cert pool")
	}
	logutil.Infof("HTTP_CLIENT", "Successfully added CA certificate to cert pool")

	// Load client cert and key
	clientCert, err := tls.LoadX509KeyPair(clientCertPath, clientKeyPath)
	if err != nil {
		return nil, fmt.Errorf("error loading client certificate and key: %v", err)
	}
	logutil.Infof("HTTP_CLIENT", "Successfully loaded client certificate and key")

	// Create TLS config
	tlsConfig := &tls.Config{
		RootCAs:            caCertPool,
		Certificates:       []tls.Certificate{clientCert},
		ServerName:         "kubernetes", // Set ServerName to match the expected hostname in the server's certificate
		InsecureSkipVerify: true,         // Skip verification of the server's certificate
	}

	return tlsConfig, nil
}

// SetupMinikubeClient sets up the HTTP client with Minikube certificates
func SetupMinikubeClient(home string) error {
	logutil.Infof("HTTP_CLIENT", "Setting up Minikube client with certificates from %s", home)
	tlsConfig, err := createMinikubeTLSConfig(home)
	if err != nil {
		logutil.Infof("HTTP_CLIENT", "Error creating Minikube TLS config: %v", err)
		return err
	}
	logutil.Infof("HTTP_CLIENT", "Successfully created Minikube TLS config with %d certificates", len(tlsConfig.Certificates))

	// Create a transport with the TLS config
	transport := &http.Transport{
		TLSClientConfig: tlsConfig,
	}

	// Create a client with the transport, preserving any existing configuration
	timeout := 10 * time.Second
	if instance != nil && instance.client != nil {
		timeout = instance.client.Timeout
	}

	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}

	// Set the client as the instance
	instance = &HTTPClient{
		client:     client,
		isMinikube: true,
	}

	return nil
}
