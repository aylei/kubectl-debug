package main

import (
	"github.com/aylei/kubectl-debug/pkg/plugin"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"os"
)

func main() {
	flags := pflag.NewFlagSet("kubectl-debug", pflag.ExitOnError)
	pflag.CommandLine = flags

	// bypass to DebugCmd
	cmd := plugin.NewDebugCmd(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
