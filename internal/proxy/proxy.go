package proxy

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

// ProxyInfo holds extracted proxy settings from runtime config.
type ProxyInfo struct {
	MixedPort int
	HTTPPort  int
	SocksPort int
	BindAddr  string
	AllowLAN  bool
	Auth      string
}

// ParseProxyInfo extracts proxy settings from a runtime YAML config.
func ParseProxyInfo(configPath string) (*ProxyInfo, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, err
	}

	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	info := &ProxyInfo{
		BindAddr: "127.0.0.1",
	}

	if v, ok := cfg["mixed-port"]; ok {
		info.MixedPort = toInt(v)
	}
	if v, ok := cfg["port"]; ok {
		info.HTTPPort = toInt(v)
	}
	if v, ok := cfg["socks-port"]; ok {
		info.SocksPort = toInt(v)
	}
	if v, ok := cfg["allow-lan"]; ok {
		info.AllowLAN = toBool(v)
	}
	if v, ok := cfg["bind-address"]; ok {
		if s, ok := v.(string); ok && s != "*" {
			info.BindAddr = s
		}
	}

	// If allow-lan and bind-address is *, find local IP
	if info.AllowLAN && info.BindAddr == "127.0.0.1" {
		info.BindAddr = getLocalIP()
	}

	// Check authentication
	if auths, ok := cfg["authentication"]; ok {
		if authList, ok := auths.([]interface{}); ok && len(authList) > 0 {
			if s, ok := authList[0].(string); ok && s != "" {
				info.Auth = s + "@"
			}
		}
	}

	// Default ports
	if info.MixedPort == 0 && info.HTTPPort == 0 {
		info.MixedPort = 7890
	}
	if info.SocksPort == 0 {
		info.SocksPort = info.MixedPort
	}

	return info, nil
}

// SetEnvVars sets proxy environment variables for the current shell session.
func SetEnvVars(info *ProxyInfo) map[string]string {
	port := info.MixedPort
	if info.HTTPPort != 0 {
		port = info.HTTPPort
	}

	httpAddr := fmt.Sprintf("http://%s%s:%d", info.Auth, info.BindAddr, port)
	socksAddr := fmt.Sprintf("socks5://%s%s:%d", info.Auth, info.BindAddr, info.SocksPort)
	noProxy := "localhost,127.0.0.1,::1"

	envVars := map[string]string{
		"http_proxy":  httpAddr,
		"HTTP_PROXY":  httpAddr,
		"https_proxy": httpAddr,
		"HTTPS_PROXY": httpAddr,
		"all_proxy":   socksAddr,
		"ALL_PROXY":   socksAddr,
		"no_proxy":    noProxy,
		"NO_PROXY":    noProxy,
	}

	for k, v := range envVars {
		os.Setenv(k, v)
	}

	return envVars
}

// FormatExports returns shell export commands for the proxy settings.
func FormatExports(info *ProxyInfo) string {
	port := info.MixedPort
	if info.HTTPPort != 0 {
		port = info.HTTPPort
	}

	httpAddr := fmt.Sprintf("http://%s%s:%d", info.Auth, info.BindAddr, port)
	socksAddr := fmt.Sprintf("socks5://%s%s:%d", info.Auth, info.BindAddr, info.SocksPort)
	noProxy := "localhost,127.0.0.1,::1"

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("export http_proxy=%s\n", httpAddr))
	sb.WriteString(fmt.Sprintf("export HTTP_PROXY=%s\n", httpAddr))
	sb.WriteString(fmt.Sprintf("export https_proxy=%s\n", httpAddr))
	sb.WriteString(fmt.Sprintf("export HTTPS_PROXY=%s\n", httpAddr))
	sb.WriteString(fmt.Sprintf("export all_proxy=%s\n", socksAddr))
	sb.WriteString(fmt.Sprintf("export ALL_PROXY=%s\n", socksAddr))
	sb.WriteString(fmt.Sprintf("export no_proxy=%s\n", noProxy))
	sb.WriteString(fmt.Sprintf("export NO_PROXY=%s\n", noProxy))
	return sb.String()
}

// UnsetEnvVars removes proxy environment variables.
func UnsetEnvVars() {
	vars := []string{
		"http_proxy", "HTTP_PROXY",
		"https_proxy", "HTTPS_PROXY",
		"all_proxy", "ALL_PROXY",
		"no_proxy", "NO_PROXY",
	}
	for _, v := range vars {
		os.Unsetenv(v)
	}
}

// IsProxySet checks if proxy environment variables are set.
func IsProxySet() bool {
	return os.Getenv("http_proxy") != "" || os.Getenv("HTTP_PROXY") != ""
}

// effectiveHTTPPort returns the effective HTTP proxy port.
func (info *ProxyInfo) effectiveHTTPPort() int {
	if info.HTTPPort != 0 {
		return info.HTTPPort
	}
	return info.MixedPort
}

// HTTPProxyURL returns the HTTP proxy URL for git/curl/etc.
func (info *ProxyInfo) HTTPProxyURL() string {
	return fmt.Sprintf("http://%s%s:%d", info.Auth, info.BindAddr, info.effectiveHTTPPort())
}

// GitProxyOn sets git global http.proxy and https.proxy.
func GitProxyOn(info *ProxyInfo) error {
	proxyURL := info.HTTPProxyURL()
	if err := gitProxySet(proxyURL); err != nil {
		return err
	}
	return nil
}

// GitSSHProxyOn adds ProxyCommand for github.com to ~/.ssh/config.
// Uses socat to tunnel SSH through the HTTP proxy (CONNECT method).
func GitSSHProxyOn(info *ProxyInfo) error {
	home, _ := os.UserHomeDir()
	if home == "" {
		return fmt.Errorf("cannot find home directory")
	}
	sshDir := home + "/.ssh"
	sshConfig := sshDir + "/config"

	if _, err := exec.LookPath("socat"); err != nil {
		return fmt.Errorf("socat not installed — install it: apt install socat")
	}

	os.MkdirAll(sshDir, 0700)

	// Check if github.com proxy is already configured
	existing, _ := os.ReadFile(sshConfig)
	if strings.Contains(string(existing), "# vpn-proxy:github") {
		return nil // already configured
	}

	block := fmt.Sprintf(`
# vpn-proxy:github — added by vpn tool
Host github.com
  HostName github.com
  User git
  Port 22
  ProxyCommand socat - PROXY:%s:%%h:%%p,proxyport=%d
# vpn-proxy:end
`, info.BindAddr, info.effectiveHTTPPort())

	f, err := os.OpenFile(sshConfig, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open ssh config: %w", err)
	}
	defer f.Close()
	fmt.Fprint(f, block)
	return nil
}

// GitSSHProxyOff removes the vpn-proxy block from ~/.ssh/config.
func GitSSHProxyOff() error {
	home, _ := os.UserHomeDir()
	if home == "" {
		return nil
	}
	sshConfig := home + "/.ssh/config"
	data, err := os.ReadFile(sshConfig)
	if err != nil {
		return nil
	}
	content := string(data)

	// Remove block between markers
	for {
		start := strings.Index(content, "# vpn-proxy:github")
		if start == -1 {
			break
		}
		end := strings.Index(content[start:], "# vpn-proxy:end")
		if end == -1 {
			break
		}
		end += start + len("# vpn-proxy:end")
		// Remove including the newline after end marker
		if end < len(content) && content[end] == '\n' {
			end++
		}
		content = content[:start] + content[end:]
	}
	return os.WriteFile(sshConfig, []byte(content), 0644)
}

// GitProxyOff unsets git global http.proxy and https.proxy.
func GitProxyOff() error {
	return gitProxyUnset()
}

func gitProxySet(proxyURL string) error {
	// Only set if git is available
	if _, err := exec.LookPath("git"); err != nil {
		return nil // git not installed, skip silently
	}
	cmd := exec.Command("git", "config", "--global", "http.proxy", proxyURL)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git http.proxy: %s", out)
	}
	cmd = exec.Command("git", "config", "--global", "https.proxy", proxyURL)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git https.proxy: %s", out)
	}
	return nil
}

func gitProxyUnset() error {
	if _, err := exec.LookPath("git"); err != nil {
		return nil
	}
	exec.Command("git", "config", "--global", "--unset", "http.proxy").Run()
	exec.Command("git", "config", "--global", "--unset", "https.proxy").Run()
	return nil
}

// IsGitProxySet checks if git http.proxy is currently configured.
func IsGitProxySet() bool {
	if _, err := exec.LookPath("git"); err != nil {
		return false
	}
	cmd := exec.Command("git", "config", "--global", "http.proxy")
	out, err := cmd.Output()
	return err == nil && len(out) > 0 && strings.TrimSpace(string(out)) != ""
}

// GitProxyStatus returns the current git proxy setting.
func GitProxyStatus() string {
	if _, err := exec.LookPath("git"); err != nil {
		return "git not installed"
	}
	cmd := exec.Command("git", "config", "--global", "http.proxy")
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return "not set"
	}
	return strings.TrimSpace(string(out))
}

func toInt(v interface{}) int {
	switch val := v.(type) {
	case int:
		return val
	case int64:
		return int(val)
	case float64:
		return int(val)
	}
	return 0
}

func toBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return strings.ToLower(val) == "true"
	}
	return false
}

func getLocalIP() string {
	// Try to determine the primary non-loopback IP
	// This is a simple approach — in production you might use net.InterfaceAddrs()
	addrs, err := os.ReadFile("/proc/net/fib_trie")
	if err == nil {
		// Simple heuristic: look for the default route interface IP
		// For now, return 127.0.0.1 as fallback
		_ = addrs
	}
	return "127.0.0.1"
}
