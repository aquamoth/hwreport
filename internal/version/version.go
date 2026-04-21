package version

import (
	"fmt"
	"runtime/debug"
)

var (
	semanticVersion = "dev"
	commitHash      = ""
)

type Info struct {
	Version string
	Commit  string
}

func Get() Info {
	version := semanticVersion
	if version == "" {
		version = "dev"
	}

	commit := commitHash
	if commit == "" {
		commit = buildInfoRevision()
	}
	if commit == "" {
		commit = "unknown"
	}

	return Info{
		Version: version,
		Commit:  shortCommit(commit),
	}
}

func (i Info) String() string {
	return fmt.Sprintf("%s (commit %s)", i.Version, i.Commit)
}

func buildInfoRevision() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	for _, setting := range info.Settings {
		if setting.Key == "vcs.revision" {
			return setting.Value
		}
	}

	return ""
}

func shortCommit(commit string) string {
	if len(commit) > 12 {
		return commit[:12]
	}
	return commit
}
