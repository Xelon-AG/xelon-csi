package driver

import (
	"fmt"
	"runtime"
)

// These are set during build time via -ldflags
var (
	gitCommit       = "none"
	gitTreeState    = "none"
	sourceDateEpoch = "0"
	version         = "dev"
)

type VersionInfo struct {
	GitCommit       string `json:"git_commit,omitempty"`
	GitTreeState    string `json:"git_tree_state,omitempty"`
	GoVersion       string `json:"go_version,omitempty"`
	SourceDateEpoch string `json:"source_data_epoch,omitempty"`
	Version         string `json:"version,omitempty"`
}

func GetVersion() string {
	return version
}

func GetVersionInfo() VersionInfo {
	return VersionInfo{
		GitCommit:       gitCommit,
		GitTreeState:    gitTreeState,
		GoVersion:       runtime.Version(),
		SourceDateEpoch: sourceDateEpoch,
		Version:         version,
	}
}

func UserAgent() string {
	return fmt.Sprintf("xelon-csi/%s", version)
}
