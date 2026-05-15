package main

import (
	"flag"
	"fmt"
	"os"

	"xct/internal/build"
	"xct/internal/tui"
)

var (
	appVersion   = "dev"
	appCommit    = "none"
	appBuildDate = "unknown"
)

func main() {
	baseDir := flag.String("base-dir", "", "controller base directory, defaults to /etc/xray-cdn-tunnel-controller")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	info := build.Info{Version: appVersion, Commit: appCommit, BuildDate: appBuildDate}
	if *showVersion {
		fmt.Println(info.String())
		return
	}

	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "xct-controller must be run as root on the Iran VPS")
		os.Exit(1)
	}

	if err := tui.Run(*baseDir, info); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
