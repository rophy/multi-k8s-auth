package config

import (
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type RenewalConfig struct {
	Enabled        bool          `yaml:"enabled"`
	ServiceAccount string        `yaml:"service_account"`
	Namespace      string        `yaml:"namespace"`
	Interval       time.Duration `yaml:"interval"`
	TokenDuration  time.Duration `yaml:"token_duration"`
}

// UnmarshalYAML handles duration parsing from string
func (r *RenewalConfig) UnmarshalYAML(unmarshal func(interface{}) error) error {
	type rawRenewalConfig struct {
		Enabled        bool   `yaml:"enabled"`
		ServiceAccount string `yaml:"service_account"`
		Namespace      string `yaml:"namespace"`
		Interval       string `yaml:"interval"`
		TokenDuration  string `yaml:"token_duration"`
	}
	var raw rawRenewalConfig
	if err := unmarshal(&raw); err != nil {
		return err
	}

	r.Enabled = raw.Enabled
	r.ServiceAccount = raw.ServiceAccount
	r.Namespace = raw.Namespace

	if raw.Interval != "" {
		d, err := time.ParseDuration(raw.Interval)
		if err != nil {
			return fmt.Errorf("parsing interval: %w", err)
		}
		r.Interval = d
	} else {
		r.Interval = 30 * time.Minute // default
	}

	if raw.TokenDuration != "" {
		d, err := time.ParseDuration(raw.TokenDuration)
		if err != nil {
			return fmt.Errorf("parsing token_duration: %w", err)
		}
		r.TokenDuration = d
	} else {
		r.TokenDuration = 1 * time.Hour // default
	}

	return nil
}

type ClusterConfig struct {
	Issuer    string         `yaml:"issuer"`
	APIServer string         `yaml:"api_server,omitempty"` // Override URL for OIDC discovery
	CACert    string         `yaml:"ca_cert,omitempty"`
	TokenPath string         `yaml:"token_path,omitempty"`
	Renewal   *RenewalConfig `yaml:"renewal,omitempty"`
}

// DiscoveryURL returns the URL to use for OIDC discovery.
// If api_server is set, use it; otherwise use issuer.
func (c *ClusterConfig) DiscoveryURL() string {
	if c.APIServer != "" {
		return c.APIServer
	}
	return c.Issuer
}

type Config struct {
	Clusters map[string]ClusterConfig `yaml:"clusters"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing config file: %w", err)
	}

	if len(cfg.Clusters) == 0 {
		return nil, fmt.Errorf("no clusters configured")
	}

	for name, cluster := range cfg.Clusters {
		if cluster.Issuer == "" {
			return nil, fmt.Errorf("cluster %q: issuer is required", name)
		}
	}

	return &cfg, nil
}

func (c *Config) ClusterNames() []string {
	names := make([]string, 0, len(c.Clusters))
	for name := range c.Clusters {
		names = append(names, name)
	}
	return names
}

// GetRenewalClusters returns cluster names that have renewal enabled
func (c *Config) GetRenewalClusters() []string {
	var names []string
	for name, cfg := range c.Clusters {
		if cfg.Renewal != nil && cfg.Renewal.Enabled {
			names = append(names, name)
		}
	}
	return names
}
