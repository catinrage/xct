package build

import "fmt"

type Info struct {
	Version   string
	Commit    string
	BuildDate string
}

func (i Info) String() string {
	version := i.Version
	if version == "" {
		version = "dev"
	}
	commit := i.Commit
	if commit == "" {
		commit = "none"
	}
	date := i.BuildDate
	if date == "" {
		date = "unknown"
	}
	return fmt.Sprintf("version=%s commit=%s build_date=%s", version, commit, date)
}

func (i Info) Label() string {
	if i.Version == "" {
		return "dev"
	}
	if i.Commit == "" {
		return i.Version
	}
	return i.Version + " (" + i.Commit + ")"
}
