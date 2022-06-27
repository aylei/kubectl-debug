package main

import (
	"github.com/jamestgrant/kubectl-debug/pkg/kubectl-debug"
	"github.com/spf13/pflag"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"os"
)

func main() {
	flags := pflag.NewFlagSet("kubectldebug", pflag.ExitOnError)
	pflag.CommandLine = flags

	// bypass to DebugCmd
	cmd := kubectldebug.NewDebugCmd(genericclioptions.IOStreams{In: os.Stdin, Out: os.Stdout, ErrOut: os.Stderr})
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
