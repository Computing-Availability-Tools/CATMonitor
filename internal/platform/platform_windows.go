//go:build windows

package platform

import "os"

var (
	DefaultConfigPath string
	DefaultDataDir    string
)

func init() {
	programData := os.Getenv("ProgramData")
	if programData == "" {
		programData = `C:\ProgramData`
	}
	DefaultConfigPath = programData + `\catmonitor\catmonitor.yaml`
	DefaultDataDir = programData + `\catmonitor\data`
}
