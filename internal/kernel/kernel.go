package kernel

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"vpn/internal/config"
)

// Manager handles the mihomo kernel process.
type Manager struct {
	Bin     string
	DataDir string
	CfgFile string
	LogFile string
	PidFile string
}

// NewManager creates a new kernel manager.
func NewManager(cfg *config.VPNConfig) *Manager {
	paths := cfg.Paths()
	return &Manager{
		Bin:     cfg.MihomoBin,
		DataDir: paths.DataDir,
		CfgFile: paths.RuntimeYAML,
		LogFile: paths.LogFile,
		PidFile: paths.PidFile,
	}
}

// IsRunning checks if mihomo is currently running.
func (m *Manager) IsRunning() bool {
	pid := m.readPid()
	if pid <= 0 {
		return false
	}
	// Check if process exists
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// On Unix, FindProcess always succeeds; send signal 0 to check
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// Start launches the mihomo process.
func (m *Manager) Start() error {
	if m.IsRunning() {
		return fmt.Errorf("mihomo is already running")
	}

	// Ensure log directory exists
	os.MkdirAll(filepath.Dir(m.LogFile), 0755)

	// Open log file
	logF, err := os.OpenFile(m.LogFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return fmt.Errorf("open log file: %w", err)
	}
	defer logF.Close()

	cmd := exec.Command(m.Bin, "-d", m.DataDir, "-f", m.CfgFile)
	cmd.Stdout = logF
	cmd.Stderr = logF
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start mihomo: %w", err)
	}

	// Save PID
	m.savePid(cmd.Process.Pid)

	// Wait a moment to check if it's still running
	time.Sleep(500 * time.Millisecond)
	if !m.IsRunning() {
		return fmt.Errorf("mihomo started but exited immediately; check log: %s", m.LogFile)
	}

	return nil
}

// Stop stops the mihomo process.
func (m *Manager) Stop() error {
	pid := m.readPid()
	if pid <= 0 {
		return nil
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		m.removePid()
		return nil
	}

	// Send SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		// Process may already be gone
		m.removePid()
		return nil
	}

	// Wait for graceful shutdown
	for i := 0; i < 10; i++ {
		if !m.IsRunning() {
			m.removePid()
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}

	// Force kill
	proc.Signal(syscall.SIGKILL)
	time.Sleep(100 * time.Millisecond)
	m.removePid()
	return nil
}

// Restart stops and starts mihomo.
func (m *Manager) Restart() error {
	m.Stop()
	time.Sleep(300 * time.Millisecond)
	return m.Start()
}

// GetLog returns the last n lines of the log file.
func (m *Manager) GetLog(n int) (string, error) {
	data, err := os.ReadFile(m.LogFile)
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > n && n > 0 {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n"), nil
}

// GetLogPath returns the log file path.
func (m *Manager) GetLogPath() string {
	return m.LogFile
}

// GetPid returns the current PID.
func (m *Manager) GetPid() int {
	return m.readPid()
}

func (m *Manager) readPid() int {
	data, err := os.ReadFile(m.PidFile)
	if err != nil {
		return -1
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return -1
	}
	return pid
}

func (m *Manager) savePid(pid int) {
	os.WriteFile(m.PidFile, []byte(strconv.Itoa(pid)), 0644)
}

func (m *Manager) removePid() {
	os.Remove(m.PidFile)
}

// ValidateConfig tests a mihomo config file.
func (m *Manager) ValidateConfig(cfgFile string) error {
	cmd := exec.Command(m.Bin, "-d", m.DataDir, "-f", cfgFile, "-t")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("config validation failed: %s", string(output))
	}
	return nil
}
