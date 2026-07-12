package platform

import (
	"os"
	"path/filepath"
)

var dataDir = DefaultDataDir
var configPath = DefaultConfigPath

func DataDir() string {
	if d := os.Getenv("CATMONITOR_DATA_DIR"); d != "" {
		return d
	}
	return dataDir
}

func SetDataDir(d string) {
	dataDir = d
}

func ConfigPath() string {
	if c := os.Getenv("CATMONITOR_CONFIG"); c != "" {
		return c
	}
	return configPath
}

func SetConfigPath(c string) {
	configPath = c
}

func ConfigDir() string {
	return filepath.Dir(ConfigPath())
}
