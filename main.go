package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"gopkg.in/yaml.v3"

	"vpn/internal/api"
	"vpn/internal/config"
	"vpn/internal/kernel"
	"vpn/internal/proxy"
	"vpn/internal/sub"
)

var (
	cfg     *config.VPNConfig
	dataDir string
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Initialize config
	cfg = loadConfig()

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "on":
		cmdOn(args)
	case "off":
		cmdOff(args)
	case "restart":
		cmdRestart(args)
	case "status":
		cmdStatus(args)
	case "log":
		cmdLog(args)
	case "sub":
		cmdSub(args)
	case "import":
		cmdImport(args)
	case "config":
		cmdConfig(args)
	case "delay":
		cmdDelay(args)
	case "node":
		cmdNode(args)
	case "tun":
		cmdTun(args)
	case "version", "-v", "--version":
		fmt.Println("vpn version 1.0.0")
	case "help", "-h", "--help":
		printUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n", cmd)
		printUsage()
		os.Exit(1)
	}
}

func loadConfig() *config.VPNConfig {
	cfg := config.DefaultConfig()

	// Check environment variable for data dir
	if d := os.Getenv("VPN_DATA_DIR"); d != "" {
		cfg.DataDir = d
	}

	// Find mihomo binary
	if cfg.MihomoBin == "" {
		cfg.MihomoBin = config.FindMihomoBin()
	}

	// Read config file if exists
	cfgPath := filepath.Join(cfg.DataDir, config.DefaultCfgFile)
	if data, err := os.ReadFile(cfgPath); err == nil {
		// Simple key=value parsing for config overrides
		// Full YAML parsing would require the yaml package
		_ = data
	}

	return cfg
}

func getKernelMgr() *kernel.Manager {
	return kernel.NewManager(cfg)
}

func getSubMgr() *sub.Manager {
	return sub.NewManager(cfg)
}

// ---- on ----

func cmdOn(args []string) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: vpn on        - Start proxy (start mihomo + print env)")
		fmt.Println("       vpn on -e     - Print proxy environment exports only")
		fmt.Println("       vpn on -s     - Start mihomo service only")
		fmt.Println()
		fmt.Println("To set proxy in current shell:")
		fmt.Println("  eval \"$(vpn on -e)\"    # bash/zsh")
		fmt.Println("  vpn on -e | source      # fish")
		return
	}

	envOnly := len(args) > 0 && (args[0] == "-e" || args[0] == "--env-only")
	svcOnly := len(args) > 0 && (args[0] == "-s" || args[0] == "--service-only")

	km := getKernelMgr()

	if !envOnly {
		if km.IsRunning() {
			fmt.Fprintln(os.Stderr, "✓ mihomo is already running")
		} else {
			paths := cfg.Paths()
			paths.EnsureDirs()
			if _, err := os.Stat(paths.RuntimeYAML); os.IsNotExist(err) {
				sm := getSubMgr()
				sm.Use(0)
			}

			fmt.Fprint(os.Stderr, "Starting mihomo... ")
			if err := km.Start(); err != nil {
				fmt.Fprintf(os.Stderr, "failed: %v\n", err)
				os.Exit(1)
			}
			fmt.Fprintln(os.Stderr, "done")
		}
	}

	if !svcOnly {
		paths := cfg.Paths()
		info, err := proxy.ParseProxyInfo(paths.RuntimeYAML)
		if err != nil {
			info = &proxy.ProxyInfo{MixedPort: 7890, SocksPort: 7891, BindAddr: "127.0.0.1"}
		}
		// Set git proxy (idempotent)
		if err := proxy.GitProxyOn(info); err != nil {
			fmt.Fprintf(os.Stderr, "Warning: git proxy: %v\n", err)
		}
		exports := proxy.FormatExports(info)
		fmt.Print(exports)
	}
}

// ---- off ----

func cmdOff(args []string) {
	if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
		fmt.Println("Usage: vpn off       - Stop proxy (stop mihomo + unset env)")
		fmt.Println("       vpn off -e    - Unset proxy environment only")
		fmt.Println("       vpn off -s    - Stop mihomo service only")
		return
	}

	envOnly := len(args) > 0 && (args[0] == "-e" || args[0] == "--env-only")
	svcOnly := len(args) > 0 && (args[0] == "-s" || args[0] == "--service-only")

	if !svcOnly {
		proxy.UnsetEnvVars()
		proxy.GitProxyOff()
		fmt.Println("✓ Terminal proxy environment unset")
		fmt.Println("✓ Git proxy unset")
	}

	if !envOnly {
		km := getKernelMgr()
		if km.IsRunning() {
			fmt.Print("Stopping mihomo... ")
			if err := km.Stop(); err != nil {
				fmt.Fprintf(os.Stderr, "failed: %v\n", err)
			} else {
				fmt.Println("done")
			}
		} else {
			fmt.Println("✓ mihomo is not running")
		}
	}
}

// ---- restart ----

func cmdRestart(args []string) {
	km := getKernelMgr()
	fmt.Print("Restarting mihomo... ")
	if err := km.Restart(); err != nil {
		fmt.Fprintf(os.Stderr, "failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("done")

	// Refresh proxy env
	paths := cfg.Paths()
	info, err := proxy.ParseProxyInfo(paths.RuntimeYAML)
	if err == nil {
		proxy.SetEnvVars(info)
	}
	fmt.Println("✓ Proxy restarted")
}

// ---- status ----

func cmdStatus(args []string) {
	km := getKernelMgr()
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	fmt.Fprintln(w, "Status\tValue")
	fmt.Fprintln(w, "------\t------")

	if km.IsRunning() {
		fmt.Fprintf(w, "mihomo\trunning (PID: %d)\n", km.GetPid())
	} else {
		fmt.Fprintf(w, "mihomo\tstopped\n")
	}

	if proxy.IsProxySet() {
		fmt.Fprintf(w, "proxy env\tset\n")
		fmt.Fprintf(w, "  http_proxy\t%s\n", os.Getenv("http_proxy"))
		fmt.Fprintf(w, "  https_proxy\t%s\n", os.Getenv("https_proxy"))
		fmt.Fprintf(w, "  all_proxy\t%s\n", os.Getenv("all_proxy"))
	} else {
		fmt.Fprintf(w, "proxy env\tnot set\n")
	}
	fmt.Fprintf(w, "git proxy\t%s\n", proxy.GitProxyStatus())

	// Show current subscription
	sm := getSubMgr()
	profiles, use, err := sm.List()
	if err == nil && use > 0 {
		for _, p := range profiles {
			if p.ID == use {
				fmt.Fprintf(w, "subscription\t[%d] %s\n", p.ID, p.URL)
				break
			}
		}
	}

	w.Flush()
}

// ---- log ----

func cmdLog(args []string) {
	km := getKernelMgr()
	n := 50
	tail := false

	for _, a := range args {
		switch a {
		case "-f", "--follow":
			tail = true
		case "-h", "--help":
			fmt.Println("Usage: vpn log         - Show last 50 log lines")
			fmt.Println("       vpn log -f      - Follow log output")
			fmt.Println("       vpn log <N>     - Show last N lines")
			return
		default:
			fmt.Sscanf(a, "%d", &n)
		}
	}

	if tail {
		// Simple tail -f
		logPath := km.GetLogPath()
		cmd := exec.Command("tail", "-f", "-n", fmt.Sprintf("%d", n), logPath)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()
		return
	}

	logContent, err := km.GetLog(n)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading log: %v\n", err)
		os.Exit(1)
	}
	if logContent == "" {
		fmt.Println("(no log output)")
	} else {
		fmt.Print(logContent)
	}
}

// ---- sub ----

func cmdSub(args []string) {
	if len(args) == 0 {
		printSubUsage()
		return
	}

	action := args[0]
	subArgs := args[1:]

	switch action {
	case "add":
		subAdd(subArgs)
	case "remove", "rm", "del", "delete":
		subRemove(subArgs)
	case "list", "ls":
		subList(subArgs)
	case "use":
		subUse(subArgs)
	case "update", "up":
		subUpdate(subArgs)
	case "log":
		subLog(subArgs)
	case "-h", "--help", "help":
		printSubUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown sub command: %s\n", action)
		printSubUsage()
	}
}

func printSubUsage() {
	fmt.Println(strings.TrimSpace(`
vpn sub - Subscription management

Usage:
  vpn sub add <url>         Add a subscription
  vpn sub add -u <url>      Add and immediately use
  vpn sub remove <id>       Remove a subscription
  vpn sub list              List all subscriptions
  vpn sub use <id>          Switch to a subscription
  vpn sub update [id]       Update subscription(s)
  vpn sub log               Show subscription log
`))
}

func subAdd(args []string) {
	useAfter := false
	var url string

	for _, a := range args {
		switch a {
		case "-u", "--use":
			useAfter = true
		case "-h", "--help":
			fmt.Println("Usage: vpn sub add [-u] <url>")
			return
		default:
			if !strings.HasPrefix(a, "-") && url == "" {
				url = a
			}
		}
	}

	if url == "" {
		fmt.Print("Enter subscription URL: ")
		fmt.Scanln(&url)
		if url == "" {
			fmt.Fprintln(os.Stderr, "URL cannot be empty")
			os.Exit(1)
		}
	}

	sm := getSubMgr()
	profile, err := sm.Add(url, useAfter)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Subscription added: [%d] %s\n", profile.ID, profile.URL)
}

func subRemove(args []string) {
	var id int
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println("Usage: vpn sub remove <id>")
			return
		}
		fmt.Sscanf(a, "%d", &id)
	}

	if id == 0 {
		fmt.Print("Enter subscription ID to remove: ")
		fmt.Scanf("%d", &id)
	}

	sm := getSubMgr()
	if err := sm.Remove(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✓ Subscription %d removed\n", id)
}

func subList(args []string) {
	sm := getSubMgr()
	profiles, use, err := sm.List()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(profiles) == 0 {
		fmt.Println("No subscriptions. Add one with: vpn sub add <url>")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tActive\tURL")
	fmt.Fprintln(w, "--\t------\t---")
	for _, p := range profiles {
		active := ""
		if p.ID == use {
			active = "✓"
		}
		fmt.Fprintf(w, "%d\t%s\t%s\n", p.ID, active, p.URL)
	}
	w.Flush()
}

func subUse(args []string) {
	var id int
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println("Usage: vpn sub use <id>")
			return
		}
		fmt.Sscanf(a, "%d", &id)
	}

	if id == 0 {
		// List and prompt
		sm := getSubMgr()
		profiles, _, _ := sm.List()
		for _, p := range profiles {
			fmt.Printf("  [%d] %s\n", p.ID, p.URL)
		}
		fmt.Print("Enter subscription ID to use: ")
		fmt.Scanf("%d", &id)
	}

	sm := getSubMgr()
	if err := sm.Use(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("✓ Switched to subscription %d\n", id)

	// Restart mihomo if running
	km := getKernelMgr()
	if km.IsRunning() {
		fmt.Print("Restarting mihomo to apply changes... ")
		km.Restart()
		fmt.Println("done")
		// Refresh proxy env
		paths := cfg.Paths()
		info, _ := proxy.ParseProxyInfo(paths.RuntimeYAML)
		if info != nil {
			proxy.SetEnvVars(info)
		}
	}
}

func subUpdate(args []string) {
	auto := false
	var id int

	for _, a := range args {
		switch a {
		case "--auto":
			auto = true
		case "-h", "--help":
			fmt.Println("Usage: vpn sub update [id]   - Update subscription")
			fmt.Println("       vpn sub update --auto  - Set up auto-update cron")
			return
		default:
			fmt.Sscanf(a, "%d", &id)
		}
	}

	if auto {
		fmt.Println("Auto-update feature: add to crontab manually:")
		fmt.Println("  0 0 */2 * * vpn sub update")
		return
	}

	sm := getSubMgr()
	if err := sm.Update(id); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✓ Subscription updated")
}

func subLog(args []string) {
	paths := cfg.Paths()
	data, err := os.ReadFile(paths.ProfilesLog)
	if err != nil {
		fmt.Println("(no subscription log)")
		return
	}
	fmt.Print(string(data))
}

// ---- config ----

func cmdConfig(args []string) {
	if len(args) == 0 {
		printConfig()
		return
	}

	action := args[0]
	switch action {
	case "show":
		printConfig()
	case "set":
		if len(args) < 3 {
			fmt.Println("Usage: vpn config set <key> <value>")
			return
		}
		fmt.Printf("Setting %s = %s (config persistence not yet implemented)\n", args[1], args[2])
	case "-h", "--help":
		fmt.Println("Usage: vpn config show       - Show current configuration")
		fmt.Println("       vpn config set <k> <v> - Set a configuration value")
	default:
		fmt.Fprintf(os.Stderr, "Unknown config command: %s\n", action)
	}
}

func printConfig() {
	fmt.Printf("Data directory:     %s\n", cfg.DataDir)
	fmt.Printf("Mihomo binary:      %s\n", cfg.MihomoBin)
	fmt.Printf("Subconverter:       %s\n", cfg.SubconverterBin)
	fmt.Printf("HTTP Port:          %d\n", cfg.Port)
	fmt.Printf("SOCKS Port:         %d\n", cfg.SocksPort)
	fmt.Printf("Mixed Port:         %d\n", cfg.MixedPort)
	fmt.Printf("External Controller: %s\n", cfg.ExternalController)
	fmt.Printf("Allow LAN:          %v\n", cfg.AllowLAN)
	fmt.Printf("Log Level:          %s\n", cfg.LogLevel)
	fmt.Printf("Sub Timeout:        %ds\n", cfg.SubTimeout)
	fmt.Printf("Sub UA:             %s\n", cfg.SubUA)
}

// ---- tun ----

func cmdTun(args []string) {
	if len(args) == 0 {
		// Show TUN status
		_ = args
		fmt.Println("TUN mode: not yet implemented")
		fmt.Println("Use: vpn tun on  /  vpn tun off")
		return
	}

	switch args[0] {
	case "on":
		fmt.Println("TUN mode on: requires root privileges")
		fmt.Println("This feature requires the mihomo config to have tun.enable = true")
		fmt.Println("Use 'vpn config set tun.enable true' to enable")
	case "off":
		fmt.Println("TUN mode off")
	case "-h", "--help":
		fmt.Println("Usage: vpn tun on    - Enable TUN mode (global VPN)")
		fmt.Println("       vpn tun off   - Disable TUN mode")
		fmt.Println("       vpn tun       - Show TUN status")
	default:
		fmt.Fprintf(os.Stderr, "Unknown tun command: %s\n", args[0])
	}
}

// ---- node ----

func ensureAPI() *api.Client {
	secret := cfg.Secret
	if secret == "" {
		if data, err := os.ReadFile(cfg.Paths().RuntimeYAML); err == nil {
			var rtCfg map[string]interface{}
			if err := yaml.Unmarshal(data, &rtCfg); err == nil {
				if s, ok := rtCfg["secret"].(string); ok {
					secret = s
				}
			}
		}
	}
	return api.NewClient(cfg.ExternalController, secret)
}

func cmdNode(args []string) {
	if len(args) > 0 && args[0] == "-h" || len(args) > 0 && args[0] == "--help" {
		fmt.Println("Usage: vpn node               - List proxy groups and current node")
		fmt.Println("       vpn node <group> <name> - Switch to a node in a group")
		return
	}

	ac := ensureAPI()

	switch len(args) {
	case 0:
		groups, err := ac.ListGroups()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "Group\tType\tCurrent\t")
		fmt.Fprintln(w, "-----\t----\t-------\t")
		for _, g := range groups {
			fmt.Fprintf(w, "%s\t%s\t%s\t\n", g.Name, g.Type, g.Now)
		}
		w.Flush()

	case 2:
		if err := ac.SwitchNode(args[0], args[1]); err != nil {
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("✓ Switched %s → %s\n", args[0], args[1])

	default:
		fmt.Fprintln(os.Stderr, "Usage: vpn node [<group> <name>]")
		os.Exit(1)
	}
}

// ---- delay ----

func cmdDelay(args []string) {
	timeout := 5000
	testURL := "http://www.gstatic.com/generate_204"

	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Println("Usage: vpn delay [flags]")
			fmt.Println("  -t <ms>    Timeout in milliseconds (default 5000)")
			fmt.Println("  -u <url>   Test URL (default http://www.gstatic.com/generate_204)")
			return
		case "-t":
			continue
		case "-u":
			continue
		default:
			if len(args) > 1 {
				for i, arg := range args {
					switch arg {
					case "-t":
						if i+1 < len(args) {
							fmt.Sscanf(args[i+1], "%d", &timeout)
						}
					case "-u":
						if i+1 < len(args) {
							testURL = args[i+1]
						}
					}
				}
			}
		}
	}

	km := getKernelMgr()
	if !km.IsRunning() {
		fmt.Fprintln(os.Stderr, "mihomo is not running. Start it first: vpn on")
		os.Exit(1)
	}

	ac := ensureAPI()
	results, err := ac.TestAllProxies(timeout, testURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}

	if len(results) == 0 {
		fmt.Println("No proxies found.")
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "Proxy\tDelay\t")
	fmt.Fprintln(w, "-----\t-----\t")
	for _, r := range results {
		if r.Error != "" {
			fmt.Fprintf(w, "%s\t%s\t\n", r.Name, r.Error)
		} else {
			fmt.Fprintf(w, "%s\t%d ms\t\n", r.Name, r.Delay)
		}
	}
	w.Flush()
}

// ---- import (migrate from clashctl) ----

func cmdImport(args []string) {
	srcDir := "/root/clashctl/resources"
	for _, a := range args {
		if a == "-h" || a == "--help" {
			fmt.Println("Usage: vpn import [source-dir]")
			fmt.Println("  Import subscriptions and config from an existing clashctl installation.")
			fmt.Println("  Default source: /root/clashctl/resources")
			return
		}
		if !strings.HasPrefix(a, "-") {
			srcDir = a
		}
	}

	profilesPath := filepath.Join(srcDir, "profiles.yaml")
	if _, err := os.Stat(profilesPath); os.IsNotExist(err) {
		fmt.Fprintf(os.Stderr, "Error: profiles.yaml not found in %s\n", srcDir)
		os.Exit(1)
	}

	// Parse existing profiles
	data, err := os.ReadFile(profilesPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error reading profiles: %v\n", err)
		os.Exit(1)
	}

	var oldProfiles struct {
		Use      int `yaml:"use"`
		Profiles []struct {
			ID   int    `yaml:"id"`
			Path string `yaml:"path"`
			URL  string `yaml:"url"`
		} `yaml:"profiles"`
	}
	if err := yaml.Unmarshal(data, &oldProfiles); err != nil {
		fmt.Fprintf(os.Stderr, "Error parsing profiles: %v\n", err)
		os.Exit(1)
	}

	if len(oldProfiles.Profiles) == 0 {
		fmt.Println("No subscriptions found in source.")
		return
	}

	// Import into our profiles
	paths := cfg.Paths()
	paths.EnsureDirs()

	sm := getSubMgr()
	pf, _ := sm.LoadProfiles()

	imported := 0
	for _, old := range oldProfiles.Profiles {
		// Check if URL already exists
		exists := false
		for _, existing := range pf.Profiles {
			if existing.URL == old.URL {
				exists = true
				break
			}
		}
		if exists {
			fmt.Printf("  [skip] %s (already exists)\n", old.URL)
			continue
		}

		// Copy profile config
		if old.Path != "" {
			oldData, err := os.ReadFile(old.Path)
			if err != nil {
				fmt.Printf("  [error] cannot read %s: %v\n", old.Path, err)
				continue
			}

			newID := len(pf.Profiles) + 1
			for _, p := range pf.Profiles {
				if p.ID >= newID {
					newID = p.ID + 1
				}
			}

			newPath := filepath.Join(paths.ProfilesDir, fmt.Sprintf("%d.yaml", newID))
			if err := os.WriteFile(newPath, oldData, 0644); err != nil {
				fmt.Printf("  [error] cannot write %s: %v\n", newPath, err)
				continue
			}

			profile := sub.Profile{
				ID:   newID,
				Path: newPath,
				URL:  old.URL,
			}
			pf.Profiles = append(pf.Profiles, profile)
			imported++
			fmt.Printf("  [import] [%d] %s\n", newID, old.URL)
		}
	}

	if imported > 0 {
		// Set active if the imported one was active
		if oldProfiles.Use > 0 {
			// Find the imported profile matching the old use ID
			for _, old := range oldProfiles.Profiles {
				if old.ID == oldProfiles.Use {
					// Find new ID by URL
					for _, p := range pf.Profiles {
						if p.URL == old.URL {
							pf.Use = p.ID
							break
						}
					}
					break
				}
			}
		}
		if err := sm.SaveProfiles(pf); err != nil {
			fmt.Fprintf(os.Stderr, "Error saving profiles: %v\n", err)
		}
	}

	fmt.Printf("\n✓ Imported %d subscription(s)\n", imported)

	// Also copy mixin and base config
	copyFile(filepath.Join(srcDir, "mixin.yaml"), paths.MixinYAML)
	fmt.Println("✓ Mixin config copied")
}

func copyFile(src, dst string) {
	if _, err := os.Stat(src); err == nil {
		data, _ := os.ReadFile(src)
		if _, err := os.Stat(dst); os.IsNotExist(err) {
			os.WriteFile(dst, data, 0644)
		}
	}
}

func printUsage() {
	fmt.Println(strings.TrimSpace(`
vpn - VPN proxy tool based on mihomo clash kernel

Usage:
  vpn <command> [options]

Commands:
  on           Start proxy (start mihomo + set terminal proxy env)
  off          Stop proxy (stop mihomo + unset proxy env)
  restart      Restart mihomo
  status       Show proxy status
  log          View mihomo logs
  sub          Subscription management
  import       Import from existing clashctl installation
  config       View/set configuration
  tun          TUN mode management
  delay        Test and show proxy delay
  node         List or switch proxy nodes
  version      Show version

Run 'vpn <command> --help' for more details on a command.
`))
}
