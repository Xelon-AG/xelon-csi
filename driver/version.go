package driver

import (
	"fmt"
	"runtime"
)

// These are set during build time via -ldflags
var (
	driverVersion = "dev"
	gitCommit     = "none"
	gitTreeState  = "none"
	buildDate     = "unknown"
)

type VersionInfo struct {
	DriverVersion string `json:"driver_version"`
	GitCommit     string `json:"git_commit"`
	GitTreeState  string `json:"git_tree_state"`
	BuildDate     string `json:"build_date"`
	GoVersion     string `json:"go_version"`
	Compiler      string `json:"compiler"`
	Platform      string `json:"platform"`
}

func GetVersion() VersionInfo {
	return VersionInfo{
		DriverVersion: driverVersion,
		GitCommit:     gitCommit,
		GitTreeState:  gitTreeState,
		BuildDate:     buildDate,
		GoVersion:     runtime.Version(),
		Compiler:      runtime.Compiler,
		Platform:      fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
	}
}
