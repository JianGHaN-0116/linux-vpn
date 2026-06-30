package sub

import (
	"crypto/tls"
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"vpn/internal/config"
)

// Profile represents a subscription profile.
type Profile struct {
	ID   int    `yaml:"id" json:"id"`
	Path string `yaml:"path" json:"path"`
	URL  string `yaml:"url" json:"url"`
	Name string `yaml:"name,omitempty" json:"name,omitempty"`
}

// ProfilesFile holds all subscription profiles and current active ID.
type ProfilesFile struct {
	Use      int       `yaml:"use" json:"use"`
	Profiles []Profile `yaml:"profiles" json:"profiles"`
}

// Manager handles subscription operations.
type Manager struct {
	cfg    *config.VPNConfig
	paths  config.Paths
	client *http.Client
}

// NewManager creates a new subscription manager.
func NewManager(cfg *config.VPNConfig) *Manager {
	tr := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: true},
	}
	return &Manager{
		cfg:   cfg,
		paths: cfg.Paths(),
		client: &http.Client{
			Transport: tr,
			Timeout:   time.Duration(cfg.SubTimeout) * time.Second,
		},
	}
}

// LoadProfiles reads the profiles YAML file.
func (m *Manager) LoadProfiles() (*ProfilesFile, error) {
	pf := &ProfilesFile{}
	data, err := os.ReadFile(m.paths.ProfilesYAML)
	if err != nil {
		if os.IsNotExist(err) {
			return pf, nil
		}
		return nil, err
	}
	if err := yaml.Unmarshal(data, pf); err != nil {
		return nil, fmt.Errorf("parse profiles: %w", err)
	}
	return pf, nil
}

// SaveProfiles writes the profiles YAML file.
func (m *Manager) SaveProfiles(pf *ProfilesFile) error {
	data, err := yaml.Marshal(pf)
	if err != nil {
		return err
	}
	return os.WriteFile(m.paths.ProfilesYAML, data, 0644)
}

// Add downloads and adds a new subscription.
func (m *Manager) Add(subURL string, useAfter bool) (*Profile, error) {
	pf, err := m.LoadProfiles()
	if err != nil {
		return nil, err
	}

	// Check for duplicates
	for _, p := range pf.Profiles {
		if p.URL == subURL {
			return nil, fmt.Errorf("subscription already exists: [%d] %s", p.ID, subURL)
		}
	}

	// Generate new ID
	newID := 1
	for _, p := range pf.Profiles {
		if p.ID >= newID {
			newID = p.ID + 1
		}
	}

	// Download config
	profilePath := filepath.Join(m.paths.ProfilesDir, fmt.Sprintf("%d.yaml", newID))
	if err := m.download(subURL, profilePath); err != nil {
		return nil, err
	}

	profile := Profile{
		ID:   newID,
		Path: profilePath,
		URL:  subURL,
	}
	pf.Profiles = append(pf.Profiles, profile)

	if err := m.SaveProfiles(pf); err != nil {
		os.Remove(profilePath)
		return nil, err
	}

	m.log(fmt.Sprintf("+ Added sub: [%d] %s", newID, subURL))

	if useAfter {
		if err := m.Use(newID); err != nil {
			return &profile, err
		}
	}

	return &profile, nil
}

// Remove deletes a subscription by ID.
func (m *Manager) Remove(id int) error {
	pf, err := m.LoadProfiles()
	if err != nil {
		return err
	}

	if pf.Use == id {
		return fmt.Errorf("cannot remove subscription %d: it is currently in use", id)
	}

	idx := -1
	for i, p := range pf.Profiles {
		if p.ID == id {
			idx = i
			break
		}
	}
	if idx == -1 {
		return fmt.Errorf("subscription %d not found", id)
	}

	profile := pf.Profiles[idx]
	os.Remove(profile.Path)

	pf.Profiles = append(pf.Profiles[:idx], pf.Profiles[idx+1:]...)
	if err := m.SaveProfiles(pf); err != nil {
		return err
	}

	m.log(fmt.Sprintf("- Removed sub: [%d] %s", id, profile.URL))
	return nil
}

// List returns all subscriptions.
func (m *Manager) List() ([]Profile, int, error) {
	pf, err := m.LoadProfiles()
	if err != nil {
		return nil, 0, err
	}
	return pf.Profiles, pf.Use, nil
}

// Use switches to a subscription by ID.
func (m *Manager) Use(id int) error {
	pf, err := m.LoadProfiles()
	if err != nil {
		return err
	}

	var profile *Profile
	for i := range pf.Profiles {
		if pf.Profiles[i].ID == id {
			profile = &pf.Profiles[i]
			break
		}
	}
	if profile == nil {
		return fmt.Errorf("subscription %d not found", id)
	}

	// Copy profile config to base config
	data, err := os.ReadFile(profile.Path)
	if err != nil {
		return fmt.Errorf("read profile: %w", err)
	}

	m.paths.EnsureDirs()
	if err := os.WriteFile(m.paths.BaseYAML, data, 0644); err != nil {
		return fmt.Errorf("write base config: %w", err)
	}

	// Merge with mixin to produce runtime config
	if err := m.mergeConfig(); err != nil {
		return fmt.Errorf("merge config: %w", err)
	}

	pf.Use = id
	if err := m.SaveProfiles(pf); err != nil {
		return err
	}

	m.log(fmt.Sprintf("=> Switched to sub: [%d] %s", id, profile.URL))
	return nil
}

// Update refreshes a subscription from its URL.
func (m *Manager) Update(id int) error {
	pf, err := m.LoadProfiles()
	if err != nil {
		return err
	}

	if id == 0 {
		id = pf.Use
	}
	if id == 0 {
		return fmt.Errorf("no subscription selected")
	}

	var profile *Profile
	for i := range pf.Profiles {
		if pf.Profiles[i].ID == id {
			profile = &pf.Profiles[i]
			break
		}
	}
	if profile == nil {
		return fmt.Errorf("subscription %d not found", id)
	}

	tempPath := m.paths.TempYAML
	if err := m.download(profile.URL, tempPath); err != nil {
		return err
	}

	// Move to profile path
	data, _ := os.ReadFile(tempPath)
	os.WriteFile(profile.Path, data, 0644)
	os.Remove(tempPath)

	m.log(fmt.Sprintf("OK Updated sub: [%d] %s", id, profile.URL))

	if pf.Use == id {
		return m.Use(id)
	}

	return nil
}

// Download downloads a raw subscription URL.
func (m *Manager) download(rawURL string, dest string) error {
	if strings.HasPrefix(rawURL, "file://") {
		src := strings.TrimPrefix(rawURL, "file://")
		data, err := os.ReadFile(src)
		if err != nil {
			return fmt.Errorf("read file: %w", err)
		}
		return os.WriteFile(dest, data, 0644)
	}

	resp, err := m.client.Get(rawURL)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("read response: %w", err)
	}

	content := strings.TrimPrefix(string(data), "\xEF\xBB\xBF")
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")

	if looksLikeHTML(content) {
		return fmt.Errorf("subscription returned HTML page; try a different User-Agent or check the URL")
	}

	if !looksLikeYAML(content) {
		converted, err := tryDecodeBase64(content)
		if err != nil {
			rawPath := dest + ".raw"
			os.WriteFile(rawPath, []byte(content), 0644)
			return fmt.Errorf("unsupported format, raw saved to %s: %w", rawPath, err)
		}
		content = converted
	}

	return os.WriteFile(dest, []byte(content), 0644)
}

func (m *Manager) mergeConfig() error {
	// Ensure base.yaml exists
	if _, err := os.Stat(m.paths.BaseYAML); os.IsNotExist(err) {
		baseData := "proxies: []\nproxy-groups: []\nrules: []\n"
		os.WriteFile(m.paths.BaseYAML, []byte(baseData), 0644)
	}

	// Ensure GeoIP/GeoSite data files are present for mihomo
	m.ensureGeoData()

	// Strategy: try yq merge first, fall back to simple merge
	yqBin := findYQ()
	if yqBin != "" {
		if err := m.yqMergeConfig(yqBin); err == nil {
			if m.validateRuntime() == nil {
				return nil
			}
		}
	}

	return m.simpleMergeConfig()
}

// ensureGeoData copies GeoIP/GeoSite files from known locations if missing.
func (m *Manager) ensureGeoData() {
	dataFiles := []string{"Country.mmdb", "geosite.dat"}
	sources := []string{
		"/root/clashctl/resources",
		filepath.Join(os.Getenv("HOME"), "clashctl/resources"),
		"/usr/share/mihomo",
		"/usr/local/share/mihomo",
	}

	for _, fname := range dataFiles {
		dest := filepath.Join(m.paths.DataDir, fname)
		if _, err := os.Stat(dest); err == nil {
			continue // already exists
		}
		for _, srcDir := range sources {
			src := filepath.Join(srcDir, fname)
			if data, err := os.ReadFile(src); err == nil {
				os.WriteFile(dest, data, 0644)
				break
			}
		}
	}
}

func (m *Manager) validateRuntime() error {
	data, err := os.ReadFile(m.paths.RuntimeYAML)
	if err != nil {
		return err
	}
	// Check that proxies is a list (not a map)
	var cfg map[string]interface{}
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return err
	}
	if proxies, ok := cfg["proxies"]; ok {
		if _, isList := proxies.([]interface{}); !isList {
			return fmt.Errorf("proxies is not a list")
		}
	}
	return nil
}

func (m *Manager) simpleMergeConfig() error {
	baseData, err := os.ReadFile(m.paths.BaseYAML)
	if err != nil {
		return fmt.Errorf("read base config: %w", err)
	}

	// Parse base config
	var base map[string]interface{}
	if err := yaml.Unmarshal(baseData, &base); err != nil {
		return fmt.Errorf("parse base config: %w", err)
	}

	// Apply simple overrides from a flat mixin (only top-level scalar/simple keys)
	mixinData, err := os.ReadFile(m.paths.MixinYAML)
	if err == nil {
		var mixin map[string]interface{}
		if err := yaml.Unmarshal(mixinData, &mixin); err == nil {
			// Only apply flat overrides; skip complex keys like rules/proxies/proxy-groups
			for k, v := range mixin {
				switch k {
				case "rules", "proxies", "proxy-groups", "tun", "dns",
					"rule-providers", "proxy-providers":
					// Skip — these use the custom prepend/append/override format
					// which needs the full yq merge. Keep base config values.
					continue
				default:
					base[k] = v
				}
			}
		}
	}

	outData, err := yaml.Marshal(base)
	if err != nil {
		return fmt.Errorf("marshal runtime: %w", err)
	}

	return os.WriteFile(m.paths.RuntimeYAML, outData, 0644)
}

func (m *Manager) yqMergeConfig(yqBin string) error {
	// Build the yq expression matching the original clashctl merge logic
	// This merges base.yaml with mixin.yaml to produce runtime.yaml
	expr := `
select(fileIndex==0) as $config |
select(fileIndex==1) as $mixin |
$mixin |= del(._custom) |
(($config // {}) * $mixin) as $runtime |
$runtime |
.rules = (
  ($mixin.rules.prepend // []) +
  ($config.rules // []) +
  ($mixin.rules.append // [])
) |
.proxies = (
  ($mixin.proxies.prepend // []) +
  (
    ($config.proxies // []) as $configList |
    ($mixin.proxies.override // []) as $overrideList |
    $configList | map(
      . as $configItem |
      (
        $overrideList[] | select(.name == $configItem.name)
      ) // $configItem
    )
  ) +
  ($mixin.proxies.append // [])
) |
.proxy-groups = (
  ($mixin.proxy-groups.prepend // []) +
  (
    ($config.proxy-groups // []) as $configList |
    ($mixin.proxy-groups.override // []) as $overrideList |
    $configList | map(
      . as $configItem |
      (
        $overrideList[] | select(.name == $configItem.name)
      ) // $configItem
    )
  ) +
  ($mixin.proxy-groups.append // [])
) |
($mixin.proxy-groups.inject // {}) as $inj |
.proxy-groups[] |= (
  . as $g |
  ($inj | .[$g.name] // []) as $extra |
  .proxies = (.proxies + $extra | unique)
)
`
	cmd := exec.Command(yqBin, "eval-all", expr, m.paths.BaseYAML, m.paths.MixinYAML)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("yq merge failed: %s", string(output))
	}

	return os.WriteFile(m.paths.RuntimeYAML, output, 0644)
}

func (m *Manager) log(msg string) {
	f, err := os.OpenFile(m.paths.ProfilesLog, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05"), msg)
}

func findYQ() string {
	candidates := []string{
		"yq",
		"/usr/local/bin/yq",
		"/usr/bin/yq",
		"/root/clashctl/bin/yq",
	}
	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates,
			filepath.Join(cwd, "clashctl/bin/yq"),
			filepath.Join(filepath.Dir(cwd), "clashctl/bin/yq"),
		)
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func looksLikeHTML(content string) bool {
	lower := strings.ToLower(content[:minInt(len(content), 500)])
	return strings.Contains(lower, "<html") ||
		strings.Contains(lower, "<!doctype") ||
		strings.Contains(lower, "<head") ||
		strings.Contains(lower, "<body")
}

func looksLikeYAML(content string) bool {
	return strings.Contains(content, "proxies:") ||
		strings.Contains(content, "proxy-providers:") ||
		(strings.Contains(content, "port:") && strings.Contains(content, "socks-port:"))
}

func tryDecodeBase64(content string) (string, error) {
	content = strings.TrimSpace(content)
	content = strings.Map(func(r rune) rune {
		if r == '-' || r == '_' {
			return r
		}
		return r
	}, content)

	encodings := []struct {
		name string
		enc  *base64.Encoding
	}{
		{"std", base64.StdEncoding},
		{"raw-std", base64.RawStdEncoding},
		{"url", base64.URLEncoding},
		{"raw-url", base64.RawURLEncoding},
	}

	for _, e := range encodings {
		decoded, err := e.enc.DecodeString(content)
		if err == nil {
			result := string(decoded)
			if looksLikeYAML(result) || strings.Contains(result, "://") {
				return result, nil
			}
		}
	}

	return "", fmt.Errorf("not a valid base64 subscription")
}

func findSubconverter() string {
	candidates := []string{
		"subconverter",
		"/usr/local/bin/subconverter",
		"/usr/bin/subconverter",
	}
	cwd, _ := os.Getwd()
	if cwd != "" {
		candidates = append(candidates, filepath.Join(cwd, "clashctl/bin/subconverter/subconverter"))
	}
	for _, p := range candidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
