package config

// SecretKeySelector defines a reference to a secret key
type SecretKeySelector struct {
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key" yaml:"key"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// BasicAuthConfig represents HTTP Basic Auth configuration.
// Username/Password reference Kubernetes Secrets, while UsernameFile/PasswordFile
// read local files so standalone (non-K8s) deployments can use Basic Auth too.
type BasicAuthConfig struct {
	Username     *SecretKeySelector `json:"username,omitempty" yaml:"username,omitempty"`
	Password     *SecretKeySelector `json:"password,omitempty" yaml:"password,omitempty"`
	UsernameFile string             `json:"usernameFile,omitempty" yaml:"usernameFile,omitempty"`
	PasswordFile string             `json:"passwordFile,omitempty" yaml:"passwordFile,omitempty"`
}

// AuthorizationConfig configures the Authorization header sent with scrape
// requests, e.g. a Bearer API token required by the target exporter.
// Credentials sources are mutually exclusive; the first non-empty of
// Credentials, CredentialsEnv, CredentialsFile, CredentialsSecret is used.
type AuthorizationConfig struct {
	// Type is the Authorization scheme (default "Bearer").
	Type string `json:"type,omitempty" yaml:"type,omitempty"`
	// Credentials is the inline credential value. Prefer CredentialsEnv,
	// CredentialsFile or CredentialsSecret to keep secrets out of the config file.
	Credentials string `json:"credentials,omitempty" yaml:"credentials,omitempty"`
	// CredentialsEnv reads the credential from the named environment variable
	// (e.g. "OPENAGENT_SCRAPE_TOKEN"). The variable is process-static (fixed at
	// launch); use CredentialsFile for tokens that must rotate at runtime.
	CredentialsEnv string `json:"credentialsEnv,omitempty" yaml:"credentialsEnv,omitempty"`
	// CredentialsFile reads the credential from a local file (standalone mode).
	CredentialsFile string `json:"credentialsFile,omitempty" yaml:"credentialsFile,omitempty"`
	// CredentialsSecret reads the credential from a Kubernetes Secret.
	CredentialsSecret *SecretKeySelector `json:"credentialsSecret,omitempty" yaml:"credentialsSecret,omitempty"`
}
