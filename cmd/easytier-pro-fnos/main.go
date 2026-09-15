// Command easytier-pro-fnos is the fnOS client for EasyTier Pro.
//
// It runs in two modes: `supervise` owns the daemon lifecycle (the package
// start-stop script starts it), and `serve` hosts the local API behind the
// fnOS gateway, supervises the EasyTier core and performs runtime updates.
package main

import (
	"fmt"
	"os"

	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/config"
	"github.com/EasyTier-Pro/easytier-pro-dsm/internal/daemon"
)

// buildVersion is injected by the packaging script.
var buildVersion = "dev"

func main() {
	command := "supervise"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}
	paths, err := config.ResolvePaths()
	if err != nil {
		fmt.Fprintf(os.Stderr, "easytier-pro-fnos: %v\n", err)
		os.Exit(1)
	}
	if command == "version" {
		fmt.Printf("easytier-pro-fnos %s\n", buildVersion)
		return
	}
	if err := daemon.Run(command, paths, buildVersion); err != nil {
		fmt.Fprintf(os.Stderr, "easytier-pro-fnos: %v\n", err)
		os.Exit(1)
	}
}
