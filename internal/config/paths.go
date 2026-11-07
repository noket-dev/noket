package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

const (
	DirEnv     = "NOKET_CONFIG_DIR"
	configFile = "config.toml"
	credsFile  = "credentials.json"
	nodeFile   = "node.json"
	pidFile    = "noketd.pid"
	logFile    = "noketd.log"
	statusFile = "noketd.status.json"
)

var (
	ErrNoCredentials = errors.New("not logged in")
	ErrNoStatus      = errors.New("noketd status is not available")
	ErrNotRunning    = errors.New("noketd is not running")
)

// Dir returns ~/.noket. NOKET_CONFIG_DIR overrides it, which is handy for
// containers and tests.
func Dir() (string, error) {
	if dir := strings.TrimSpace(os.Getenv(DirEnv)); dir != "" {
		return filepath.Abs(dir)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	return filepath.Join(home, ".noket"), nil
}

// EnsureDir creates the config directory with 0700 permissions.
func EnsureDir() (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("create config directory %s: %w", dir, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("secure config directory %s: %w", dir, err)
	}
	return dir, nil
}

func Path(name string) (string, error) {
	dir, err := Dir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

func ConfigPath() (string, error) { return Path(configFile) }
func LogPath() (string, error)    { return Path(logFile) }

// atomicWrite writes data to a temp file in the destination directory and
// renames it over path, so readers never see a partial file.
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %s: %w", dir, err)
	}
	tmp, err := os.CreateTemp(dir, ".noket-*")
	if err != nil {
		return fmt.Errorf("create temporary file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()

	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("set permissions on %s: %w", tmpName, err)
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write %s: %w", tmpName, err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("sync %s: %w", tmpName, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close %s: %w", tmpName, err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("replace %s: %w", path, err)
	}
	return nil
}

func ReadPID() (int, error) {
	path, err := Path(pidFile)
	if err != nil {
		return 0, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, ErrNotRunning
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid pid file %s", path)
	}
	return pid, nil
}

func WritePID(pid int) error {
	path, err := Path(pidFile)
	if err != nil {
		return err
	}
	return atomicWrite(path, []byte(strconv.Itoa(pid)+"\n"), 0o600)
}

func RemovePID() {
	if path, err := Path(pidFile); err == nil {
		_ = os.Remove(path)
	}
}
