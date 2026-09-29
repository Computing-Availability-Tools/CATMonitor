package platform

import (
	"os"
	"path/filepath"
	"testing"
)

// unsetEnv removes an env var for the duration of the test and restores it after.
func unsetEnv(t *testing.T, key string) {
	t.Helper()
	old, had := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if had {
			_ = os.Setenv(key, old)
		}
	})
}

func TestDefaults(t *testing.T) {
	unsetEnv(t, "CATMONITOR_DATA_DIR")
	unsetEnv(t, "CATMONITOR_CONFIG")
	SetDataDir(DefaultDataDir)
	SetConfigPath(DefaultConfigPath)

	if got := DataDir(); got != DefaultDataDir {
		t.Errorf("DataDir() = %q, want default %q", got, DefaultDataDir)
	}
	if got := ConfigPath(); got != DefaultConfigPath {
		t.Errorf("ConfigPath() = %q, want default %q", got, DefaultConfigPath)
	}
}

func TestDataDirEnvOverride(t *testing.T) {
	t.Setenv("CATMONITOR_DATA_DIR", "/env/data")
	if got := DataDir(); got != "/env/data" {
		t.Errorf("DataDir() = %q, want env value", got)
	}
}

func TestConfigPathEnvOverride(t *testing.T) {
	t.Setenv("CATMONITOR_CONFIG", "/env/config.yaml")
	if got := ConfigPath(); got != "/env/config.yaml" {
		t.Errorf("ConfigPath() = %q, want env value", got)
	}
}

func TestSetDataDirOverride(t *testing.T) {
	unsetEnv(t, "CATMONITOR_DATA_DIR")
	t.Cleanup(func() { SetDataDir(DefaultDataDir) })
	SetDataDir("/custom/data")
	if got := DataDir(); got != "/custom/data" {
		t.Errorf("DataDir() = %q, want /custom/data", got)
	}
}

func TestSetConfigPathAndDir(t *testing.T) {
	unsetEnv(t, "CATMONITOR_CONFIG")
	t.Cleanup(func() { SetConfigPath(DefaultConfigPath) })
	SetConfigPath("/etc/custom/catmonitor.yaml")
	if got := ConfigPath(); got != "/etc/custom/catmonitor.yaml" {
		t.Errorf("ConfigPath() = %q", got)
	}
	if got := ConfigDir(); got != filepath.Dir("/etc/custom/catmonitor.yaml") {
		t.Errorf("ConfigDir() = %q, want %q", got, filepath.Dir("/etc/custom/catmonitor.yaml"))
	}
}

// The env var wins over the programmatically set value.
func TestEnvBeatsSetter(t *testing.T) {
	t.Setenv("CATMONITOR_DATA_DIR", "/env/wins")
	t.Cleanup(func() { SetDataDir(DefaultDataDir) })
	SetDataDir("/setter/loses")
	if got := DataDir(); got != "/env/wins" {
		t.Errorf("DataDir() = %q, want env value /env/wins", got)
	}
}
