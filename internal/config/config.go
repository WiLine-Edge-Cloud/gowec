// Package config keeps gowec's contexts: named pairs of API host and key.
package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"gopkg.in/yaml.v3"
)

// DefaultAPIURL is the prepaid deployment, the only line with the public API.
const DefaultAPIURL = "https://prepaid-cloudapi.wiline.com"

// Context is one API host and the key used against it.
type Context struct {
	APIURL string `yaml:"api_url"`
	Token  string `yaml:"token,omitempty"`
}

// Config is the whole file.
type Config struct {
	CurrentContext string              `yaml:"current_context"`
	Contexts       map[string]*Context `yaml:"contexts"`

	path string
}

// Dir is where gowec keeps its files: $GOWEC_CONFIG's directory, else ~/.config/gowec.
func Dir() string {
	if p := os.Getenv("GOWEC_CONFIG"); p != "" {
		return filepath.Dir(p)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gowec"
	}
	return filepath.Join(home, ".config", "gowec")
}

func path() string {
	if p := os.Getenv("GOWEC_CONFIG"); p != "" {
		return p
	}
	return filepath.Join(Dir(), "config.yaml")
}

// Load reads the config file; a missing file is an empty config.
func Load() (*Config, error) {
	cfg := &Config{Contexts: map[string]*Context{}, path: path()}
	data, err := os.ReadFile(cfg.path)
	if errors.Is(err, os.ErrNotExist) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}
	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("can't read %s: %w", cfg.path, err)
	}
	if cfg.Contexts == nil {
		cfg.Contexts = map[string]*Context{}
	}
	return cfg, nil
}

// Save writes the file readable by its owner only: it holds API keys.
func (c *Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	data, err := yaml.Marshal(c)
	if err != nil {
		return err
	}
	tmp := c.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, c.path)
}

// Names lists the contexts in order.
func (c *Config) Names() []string {
	names := make([]string, 0, len(c.Contexts))
	for name := range c.Contexts {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// Active is the context a command runs against, after the environment's say.
type Active struct {
	Name   string
	APIURL string
	Token  string
}

// Resolve picks the active context: the --context flag, else $GOWEC_CONTEXT,
// else the current one. $GOWEC_API_URL and $GOWEC_TOKEN override its values,
// so CI can run without a config file.
func (c *Config) Resolve(flag string) Active {
	name := flag
	if name == "" {
		name = os.Getenv("GOWEC_CONTEXT")
	}
	if name == "" {
		name = c.CurrentContext
	}
	active := Active{Name: name}
	if ctx := c.Contexts[name]; ctx != nil {
		active.APIURL, active.Token = ctx.APIURL, ctx.Token
	}
	if v := os.Getenv("GOWEC_API_URL"); v != "" {
		active.APIURL = v
	}
	if v := os.Getenv("GOWEC_TOKEN"); v != "" {
		active.Token = v
		if active.Name == "" {
			active.Name = "env"
		}
	}
	if active.APIURL == "" {
		active.APIURL = DefaultAPIURL
	}
	return active
}
