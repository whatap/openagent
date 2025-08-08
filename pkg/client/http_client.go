package client

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io/ioutil"
	"net/http"
	"strings"
	"time"

	configPkg "open-agent/pkg/config"
	"open-agent/pkg/k8s"
	"open-agent/tools/util/logutil"
)

const (
	// File and path constants
	localCAPath              = "/var/run/secrets/kubernetes.io/serviceaccount/ca.crt"
	localServiceAccountToken = "/var/run/secrets/kubernetes.io/serviceaccount/token"
	kubernetesServiceHost    = "KUBERNETES_SERVICE_HOST"
	kubernetesServicePort    = "KUBERNETES_SERVICE_PORT"
)

// SecretKeySelector defines a reference to a secret key
type SecretKeySelector struct {
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key" yaml:"key"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// TLSConfig represents TLS configuration options
type TLSConfig struct {
	// InsecureSkipVerify disables target certificate validation
	InsecureSkipVerify bool `json:"insecureSkipVerify,omitempty" yaml:"insecureSkipVerify,omitempty"`

	// CA certificate configuration via Kubernetes Secret
	CASecret *SecretKeySelector `json:"caSecret,omitempty" yaml:"caSecret,omitempty"`

	// Client certificate configuration via Kubernetes Secret
	CertSecret *SecretKeySelector `json:"certSecret,omitempty" yaml:"certSecret,omitempty"`

	// Client private key configuration via Kubernetes Secret
	KeySecret *SecretKeySelector `json:"keySecret,omitempty" yaml:"keySecret,omitempty"`

	// CA certificate file path (alternative to CASecret)
	CAFile string `json:"caFile,omitempty" yaml:"caFile,omitempty"`

	// Client certificate file path (alternative to CertSecret)
	CertFile string `json:"certFile,omitempty" yaml:"certFile,omitempty"`

	// Client private key file path (alternative to KeySecret)
	KeyFile string `json:"keyFile,omitempty" yaml:"keyFile,omitempty"`

	// ServerName extension to indicate the name of the server
	ServerName string `json:"serverName,omitempty" yaml:"serverName,omitempty"`
}

// Validate validates the TLS configuration to ensure consistency
func (c *TLSConfig) Validate() error {
	if c == nil {
		return nil
	}

	// Check for mutually exclusive CA configurations
	caConfigCount := 0
	if c.CAFile != "" {
		caConfigCount++
	}
	if c.CASecret != nil {
		caConfigCount++
	}
	if caConfigCount > 1 {
		return fmt.Errorf("at most one of caFile and caSecret must be configured")
	}

	// Check for mutually exclusive client certificate configurations
	certConfigCount := 0
	if c.CertFile != "" {
		certConfigCount++
	}
	if c.CertSecret != nil {
		certConfigCount++
	}
	if certConfigCount > 1 {
		return fmt.Errorf("at most one of certFile and certSecret must be configured")
	}

	// Check for mutually exclusive client key configurations
	keyConfigCount := 0
	if c.KeyFile != "" {
		keyConfigCount++
	}
	if c.KeySecret != nil {
		keyConfigCount++
	}
	if keyConfigCount > 1 {
		return fmt.Errorf("at most one of keyFile and keySecret must be configured")
	}

	// Validate client certificate and key pairing
	hasClientCert := c.CertFile != "" || c.CertSecret != nil
	hasClientKey := c.KeyFile != "" || c.KeySecret != nil

	if hasClientCert && !hasClientKey {
		return fmt.Errorf("client key must be configured when client certificate is specified")
	}
	if hasClientKey && !hasClientCert {
		return fmt.Errorf("client certificate must be configured when client key is specified")
	}

	// Validate that file and secret configurations are not mixed for client auth
	if (c.CertFile != "" && c.KeySecret != nil) || (c.CertSecret != nil && c.KeyFile != "") {
		return fmt.Errorf("client certificate and key must use the same configuration method (both file or both secret)")
	}

	// Validate SecretKeySelector fields
	if c.CASecret != nil && (c.CASecret.Name == "" || c.CASecret.Key == "") {
		return fmt.Errorf("caSecret must have both name and key specified")
	}
	if c.CertSecret != nil && (c.CertSecret.Name == "" || c.CertSecret.Key == "") {
		return fmt.Errorf("certSecret must have both name and key specified")
	}
	if c.KeySecret != nil && (c.KeySecret.Name == "" || c.KeySecret.Key == "") {
		return fmt.Errorf("keySecret must have both name and key specified")
	}

	return nil
}

// Use the package-level functions provided by the config package
// instead of creating our own instance of WhatapConfig

// HTTPClient is responsible for making HTTP requests to scrape metrics from targets
type HTTPClient struct {
	client *http.Client
}

var instance *HTTPClient

func GetInstance() *HTTPClient {
	if instance == nil {
		instance = &HTTPClient{
			client: &http.Client{
				Timeout: 10 * time.Second,
			},
		}

		// Only try to configure TLS with Kubernetes CA cert in K8s environment
		k8sClient := k8s.GetInstance()
		if k8sClient.IsInitialized() {
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
				if configPkg.IsDebugEnabled() {
					logutil.Debugf("HTTP_CLIENT", "Configured TLS with Kubernetes CA certificate")
				}
			} else {
				if configPkg.IsDebugEnabled() {
					logutil.Debugf("HTTP_CLIENT", "Failed to load Kubernetes CA certificate: %v", err)
				}
			}
		} else {
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("HTTP_CLIENT", "Not in Kubernetes environment, skipping CA certificate loading")
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

// loadCertificateFromFile loads a certificate from a file path
func loadCertificateFromFile(filePath string) ([]byte, error) {
	if filePath == "" {
		return nil, fmt.Errorf("certificate file path is empty")
	}

	data, err := ioutil.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("error reading certificate file %s: %v", filePath, err)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("certificate file %s is empty", filePath)
	}

	return data, nil
}

// loadCertificateFromSecret loads a certificate from a Kubernetes Secret
func loadCertificateFromSecret(secretSelector *SecretKeySelector) ([]byte, error) {
	if secretSelector == nil {
		return nil, fmt.Errorf("secret selector is nil")
	}

	// Import k8s package when this function is actually used
	k8sClient := k8s.GetInstance()
	if !k8sClient.IsInitialized() {
		return nil, fmt.Errorf("kubernetes client not initialized")
	}

	namespace := secretSelector.Namespace
	if namespace == "" {
		namespace = "default"
	}

	secret, err := k8sClient.GetSecret(namespace, secretSelector.Name)
	if err != nil {
		return nil, fmt.Errorf("failed to get secret %s/%s: %v", namespace, secretSelector.Name, err)
	}

	data, ok := secret.Data[secretSelector.Key]
	if !ok {
		return nil, fmt.Errorf("key %s not found in secret %s/%s", secretSelector.Key, namespace, secretSelector.Name)
	}

	if len(data) == 0 {
		return nil, fmt.Errorf("secret %s/%s key %s is empty", namespace, secretSelector.Name, secretSelector.Key)
	}

	return data, nil
}

func (c *HTTPClient) ExecuteGet(targetURL string) (string, error) {
	return c.ExecuteGetWithTLSConfig(targetURL, nil)
}

func (c *HTTPClient) ExecuteGetWithTLSConfig(targetURL string, tlsConfig *TLSConfig) (string, error) {
	formattedURL := FormatURL(targetURL)
	// Log the request
	if configPkg.IsDebugEnabled() {
		logutil.Debugf("HTTP_CLIENT", "HTTP Request: GET %s", formattedURL)
	}

	req, err := http.NewRequest("GET", formattedURL, nil)
	if err != nil {
		return "", fmt.Errorf("error creating request: %v", err)
	}

	// Try to add service account token for authentication in K8s environment only
	k8sClient := k8s.GetInstance()
	if k8sClient.IsInitialized() {
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
			logutil.Debugf("HTTP_CLIENT", "Not in Kubernetes environment, skipping service account token")
		}
	}

	req.Header.Set("Accept", "application/json")

	// Use the default client or create a new one with custom TLS config
	client := c.client
	if tlsConfig != nil {
		// Validate TLS configuration
		if err := tlsConfig.Validate(); err != nil {
			return "", fmt.Errorf("invalid TLS configuration: %v", err)
		}

		if configPkg.IsDebugEnabled() {
			logutil.Debugf("HTTP_CLIENT", "Using custom TLS config with InsecureSkipVerify=%v", tlsConfig.InsecureSkipVerify)
		}

		// Create a custom transport with the specified TLS config
		customTLSConfig := &tls.Config{
			InsecureSkipVerify: tlsConfig.InsecureSkipVerify,
		}

		// Set server name if specified
		if tlsConfig.ServerName != "" {
			customTLSConfig.ServerName = tlsConfig.ServerName
			if configPkg.IsDebugEnabled() {
				logutil.Debugf("HTTP_CLIENT", "Set server name: %s", tlsConfig.ServerName)
			}
		}

		// Configure certificate validation if InsecureSkipVerify is false
		if !tlsConfig.InsecureSkipVerify {
			// Load system root CAs as base
			rootCAs, _ := x509.SystemCertPool()
			if rootCAs == nil {
				rootCAs = x509.NewCertPool()
			}

			// Add CA certificate (prefer file over secret over default K8s CA)
			var caData []byte
			var caErr error

			if tlsConfig.CAFile != "" {
				// Load CA from file
				caData, caErr = loadCertificateFromFile(tlsConfig.CAFile)
				if caErr == nil {
					if rootCAs.AppendCertsFromPEM(caData) {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Added CA certificate from file: %s", tlsConfig.CAFile)
						}
					} else {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Failed to parse CA certificate from file: %s", tlsConfig.CAFile)
						}
					}
				} else {
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Failed to load CA certificate from file %s: %v", tlsConfig.CAFile, caErr)
					}
				}
			} else if tlsConfig.CASecret != nil {
				// Load CA from secret
				caData, caErr = loadCertificateFromSecret(tlsConfig.CASecret)
				if caErr == nil {
					if rootCAs.AppendCertsFromPEM(caData) {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Added CA certificate from secret: %s/%s", tlsConfig.CASecret.Name, tlsConfig.CASecret.Key)
						}
					} else {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Failed to parse CA certificate from secret: %s/%s", tlsConfig.CASecret.Name, tlsConfig.CASecret.Key)
						}
					}
				} else {
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Failed to load CA certificate from secret %s/%s: %v", tlsConfig.CASecret.Name, tlsConfig.CASecret.Key, caErr)
					}
				}
			} else {
				// Fall back to default Kubernetes CA only in K8s environment
				k8sClient := k8s.GetInstance()
				if k8sClient.IsInitialized() {
					if cert, err := loadKubernetesCACert(); err == nil {
						rootCAs.AddCert(cert)
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Added default Kubernetes CA cert to root CA pool")
						}
					} else {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Failed to load default Kubernetes CA cert: %v", err)
						}
					}
				} else {
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Not in Kubernetes environment, using system CA pool only")
					}
				}
			}

			customTLSConfig.RootCAs = rootCAs

			// Configure client certificate authentication
			if (tlsConfig.CertFile != "" && tlsConfig.KeyFile != "") || (tlsConfig.CertSecret != nil && tlsConfig.KeySecret != nil) {
				var certData, keyData []byte
				var certErr, keyErr error

				if tlsConfig.CertFile != "" && tlsConfig.KeyFile != "" {
					// Load client cert and key from files
					certData, certErr = loadCertificateFromFile(tlsConfig.CertFile)
					keyData, keyErr = loadCertificateFromFile(tlsConfig.KeyFile)
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Loading client certificate from files: cert=%s, key=%s", tlsConfig.CertFile, tlsConfig.KeyFile)
					}
				} else if tlsConfig.CertSecret != nil && tlsConfig.KeySecret != nil {
					// Load client cert and key from secrets
					certData, certErr = loadCertificateFromSecret(tlsConfig.CertSecret)
					keyData, keyErr = loadCertificateFromSecret(tlsConfig.KeySecret)
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Loading client certificate from secrets: cert=%s/%s, key=%s/%s",
							tlsConfig.CertSecret.Name, tlsConfig.CertSecret.Key, tlsConfig.KeySecret.Name, tlsConfig.KeySecret.Key)
					}
				}

				if certErr == nil && keyErr == nil && len(certData) > 0 && len(keyData) > 0 {
					if clientCert, err := tls.X509KeyPair(certData, keyData); err == nil {
						customTLSConfig.Certificates = []tls.Certificate{clientCert}
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Successfully configured client certificate authentication")
						}
					} else {
						if configPkg.IsDebugEnabled() {
							logutil.Debugf("HTTP_CLIENT", "Failed to create client certificate pair: %v", err)
						}
					}
				} else {
					if configPkg.IsDebugEnabled() {
						logutil.Debugf("HTTP_CLIENT", "Failed to load client certificate or key: certErr=%v, keyErr=%v", certErr, keyErr)
					}
				}
			}
		}

		transport := &http.Transport{
			TLSClientConfig: customTLSConfig,
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
