package config

import (
	"fmt"
	"os"
	"path/filepath"
)

const (
	DefaultDir      = ".vpn"
	DefaultDataDir  = "data"
	DefaultBinDir   = "bin"
	DefaultLogFile  = "mihomo.log"
	DefaultPidFile  = "mihomo.pid"
	DefaultCfgFile  = "config.yaml"
	DefaultMixinFile = "mixin.yaml"
	DefaultProfilesFile = "profiles.yaml"
	DefaultProfilesLog  = "profiles.log"
)

// VPNConfig holds the tool's own configuration.
type VPNConfig struct {
	// Mihomo binary path
	MihomoBin string `yaml:"mihomo_bin,omitempty"`

	// Subconverter binary path (optional)
	SubconverterBin string `yaml:"subconverter_bin,omitempty"`

	// Subconverter URL for converting non-Clash subscriptions (default https://api.v1.mk/sub)
	SubconverterURL string `yaml:"subconverter_url,omitempty"`

	// Data directory for runtime configs, profiles, logs
	DataDir string `yaml:"data_dir,omitempty"`

	// HTTP proxy port (default 7890)
	Port int `yaml:"port,omitempty"`

	// SOCKS proxy port (default 7891)
	SocksPort int `yaml:"socks_port,omitempty"`

	// Mixed port (default 7890, takes precedence)
	MixedPort int `yaml:"mixed_port,omitempty"`

	// External controller address (default 127.0.0.1:9090)
	ExternalController string `yaml:"external_controller,omitempty"`

	// Secret for external controller
	Secret string `yaml:"secret,omitempty"`

	// Allow LAN connections
	AllowLAN bool `yaml:"allow_lan,omitempty"`

	// Log level: debug, info, warning, error
	LogLevel string `yaml:"log_level,omitempty"`

	// Subscription download timeout in seconds
	SubTimeout int `yaml:"sub_timeout,omitempty"`

	// User-Agent for subscription download
	SubUA string `yaml:"sub_ua,omitempty"`
}

func DefaultConfig() *VPNConfig {
	home, _ := os.UserHomeDir()
	if home == "" {
		home = "/root"
	}
	return &VPNConfig{
		MihomoBin:          "",
		SubconverterBin:    "",
		SubconverterURL:    "https://api.v1.mk/sub",
		DataDir:            filepath.Join(home, DefaultDir),
		Port:               7890,
		SocksPort:          7891,
		MixedPort:          7890,
		ExternalController: "127.0.0.1:9090",
		Secret:             "",
		AllowLAN:           false,
		LogLevel:           "info",
		SubTimeout:         10,
		SubUA:              "mihomo",
	}
}

// Paths returns derived paths from the config.
func (c *VPNConfig) Paths() Paths {
	return Paths{
		DataDir:      c.DataDir,
		RuntimeYAML:  filepath.Join(c.DataDir, "runtime.yaml"),
		BaseYAML:     filepath.Join(c.DataDir, "base.yaml"),
		MixinYAML:    filepath.Join(c.DataDir, DefaultMixinFile),
		ProfilesYAML: filepath.Join(c.DataDir, DefaultProfilesFile),
		ProfilesLog:  filepath.Join(c.DataDir, DefaultProfilesLog),
		ProfilesDir:  filepath.Join(c.DataDir, "profiles"),
		LogFile:      filepath.Join(c.DataDir, DefaultLogFile),
		PidFile:      filepath.Join(c.DataDir, DefaultPidFile),
		TempYAML:     filepath.Join(c.DataDir, "temp.yaml"),
	}
}

type Paths struct {
	DataDir      string
	RuntimeYAML  string
	BaseYAML     string
	MixinYAML    string
	ProfilesYAML string
	ProfilesLog  string
	ProfilesDir  string
	LogFile      string
	PidFile      string
	TempYAML     string
}

// EnsureDirs creates required directories.
func (p Paths) EnsureDirs() error {
	dirs := []string{p.DataDir, p.ProfilesDir}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0755); err != nil {
			return fmt.Errorf("create dir %s: %w", d, err)
		}
	}
	return nil
}

// FindMihomoBin searches for the mihomo binary in common locations.
func FindMihomoBin() string {
	candidates := []string{
		"mihomo",
		"/usr/local/bin/mihomo",
		"/usr/bin/mihomo",
	}

	// Check current working directory and common install paths
	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates,
			filepath.Join(cwd, "mihomo"),
			filepath.Join(cwd, "clashctl/bin/mihomo"),
		)
	}

	// Check parent of cwd (for development)
	if cwd != "" {
		parent := filepath.Dir(cwd)
		candidates = append(candidates,
			filepath.Join(parent, "clashctl/bin/mihomo"),
		)
	}

	home, _ := os.UserHomeDir()
	if home != "" {
		candidates = append(candidates,
			filepath.Join(home, ".vpn/bin/mihomo"),
			filepath.Join(home, "clashctl/bin/mihomo"),
		)
	}

	// Absolute fallback - look for it in known install paths
	knownPaths := []string{
		"/root/clashctl/bin/mihomo",
		"/opt/clashctl/bin/mihomo",
	}
	candidates = append(candidates, knownPaths...)

	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() && isExecutable(fi) {
			return p
		}
	}
	return ""
}

func isExecutable(fi os.FileInfo) bool {
	return fi.Mode()&0111 != 0
}
