// Command agentctl is the CLI entrypoint. It builds the real
// incus/lima/hyperv provider registry and hands off to internal/cli,
// which owns the actual command tree and stays importable/testable on
// its own.
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/apomonosi/sandboxing/internal/cli"
	"github.com/apomonosi/sandboxing/internal/provider"
	"github.com/apomonosi/sandboxing/internal/provider/hyperv"
	"github.com/apomonosi/sandboxing/internal/provider/incus"
	"github.com/apomonosi/sandboxing/internal/provider/lima"
)

func newRegistry() *provider.Registry {
	reg := provider.NewRegistry()
	reg.Register("incus", incus.New)
	reg.Register("lima", lima.New)
	reg.Register("hyperv", hyperv.New)
	return reg
}

func main() {
	root := cli.NewRootCmd(newRegistry())
	if err := root.Execute(); err != nil {
		var exitErr *cli.ExitError
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.Code)
		}
		fmt.Fprintln(os.Stderr, "agentctl:", err)
		os.Exit(1)
	}
}
