package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type GitLabConfig struct {
	Host      string `yaml:"host"`
	HTTPPort  int    `yaml:"http_port"`
	HTTPSPort int    `yaml:"https_port"`
	SSHPort   int    `yaml:"ssh_port"`
}

type ProxyConfig struct {
	HTTPListen  string `yaml:"http_listen"`
	HTTPSListen string `yaml:"https_listen"`
	SSHListen   string `yaml:"ssh_listen"`
}

type TLSConfig struct {
	Enabled  bool   `yaml:"enabled"`
	CertFile string `yaml:"cert_file"`
	KeyFile  string `yaml:"key_file"`
	Domain   string `yaml:"domain"`
}

type LoggingConfig struct {
	Level string `yaml:"level"`
}

type Config struct {
	GitLab  GitLabConfig  `yaml:"gitlab"`
	Proxy   ProxyConfig   `yaml:"proxy"`
	TLS     TLSConfig     `yaml:"tls"`
	Logging LoggingConfig `yaml:"logging"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("config validation failed: %w", err)
	}

	cfg.setDefaults()

	return &cfg, nil
}

func (c *Config) Validate() error {
	if c.GitLab.Host == "" {
		return fmt.Errorf("gitlab.host is required")
	}
	return nil
}

func (c *Config) setDefaults() {
	if c.GitLab.HTTPPort == 0 {
		c.GitLab.HTTPPort = 80
	}
	if c.GitLab.HTTPSPort == 0 {
		c.GitLab.HTTPSPort = 443
	}
	if c.GitLab.SSHPort == 0 {
		c.GitLab.SSHPort = 22
	}
	if c.Proxy.HTTPListen == "" {
		c.Proxy.HTTPListen = ":80"
	}
	if c.Proxy.HTTPSListen == "" {
		c.Proxy.HTTPSListen = ":443"
	}
	if c.Proxy.SSHListen == "" {
		c.Proxy.SSHListen = ":22"
	}
	if c.Logging.Level == "" {
		c.Logging.Level = "info"
	}
}

func (c *Config) GitLabHTTPAddr() string {
	return fmt.Sprintf("%s:%d", c.GitLab.Host, c.GitLab.HTTPPort)
}

func (c *Config) GitLabHTTPSAddr() string {
	return fmt.Sprintf("%s:%d", c.GitLab.Host, c.GitLab.HTTPSPort)
}

func (c *Config) GitLabSSHAddr() string {
	return fmt.Sprintf("%s:%d", c.GitLab.Host, c.GitLab.SSHPort)
}
