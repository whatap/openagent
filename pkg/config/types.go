package config

// SecretKeySelector defines a reference to a secret key
type SecretKeySelector struct {
	Name      string `json:"name" yaml:"name"`
	Key       string `json:"key" yaml:"key"`
	Namespace string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
}

// BasicAuthConfig represents HTTP Basic Auth configuration
type BasicAuthConfig struct {
	Username *SecretKeySelector `json:"username,omitempty" yaml:"username,omitempty"`
	Password *SecretKeySelector `json:"password,omitempty" yaml:"password,omitempty"`
}
