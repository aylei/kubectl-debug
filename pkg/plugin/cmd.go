package plugin

import (
	"fmt"
	"github.com/spf13/cobra"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/tools/clientcmd/api"
)

const (
	example = `
	# debug a running pod
	kubectl debug 
`
)

var (
	errNoContext = fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
)

// DebugOptions specify how to run debug container in a running pod
type DebugOptions struct {
	Flags *genericclioptions.ConfigFlags

	ResultingContext *api.Context

	UserSpecifiedCluster   string
	UserSpecifiedContext   string
	UserSpecifiedAuthInfo  string
	UserSpecifiedNamespace string

	RetainContainer bool
	Image           string

	RawConfig api.Config
	Args      []string

	genericclioptions.IOStreams
}

func NewDebugOptions(streams genericclioptions.IOStreams) *DebugOptions {
	return &DebugOptions{Flags: genericclioptions.NewConfigFlags(), IOStreams: streams}
}

// NewDebugCmd returns a cobra command wrapping DebugOptions
func NewDebugCmd(streams genericclioptions.IOStreams) *cobra.Command {
	opts := NewDebugOptions(streams)

	cmd := &cobra.Command{
		Use:          "kubectl debug POD [-c CONTAINER] -- COMMAND [Args...] [options]",
		Short:        "Run a container in running pod",
		Example:      example,
		SilenceUsage: true,
		RunE: func(c *cobra.Command, args []string) error {
			if err := opts.Complete(c, args); err != nil {
				return err
			}
			if err := opts.Validate(); err != nil {
				return err
			}
			if err := opts.Run(); err != nil {
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&opts.RetainContainer, "retain", "r", opts.RetainContainer,
		"Retain container after debug session closed")
	cmd.Flags().StringVar(&opts.Image, "Image", opts.Image,
		"Container Image to run the debug container")
	opts.Flags.AddFlags(cmd.Flags())

	return cmd
}

// Complete populate default values from KUBECONFIG file
func (o *DebugOptions) Complete(cmd *cobra.Command, args []string) error {
	o.Args = args

	var err error
	o.RawConfig, err = o.Flags.ToRawKubeConfigLoader().RawConfig()
	if err != nil {
		return err
	}

	o.UserSpecifiedNamespace, err = cmd.Flags().GetString("namespace")
	if err != nil {
		return err
	}
	if len(args) > 0 {
		if len(o.UserSpecifiedNamespace) > 0 {
			return fmt.Errorf("cannot specify both a --namespace value and a new namespace argument")
		}

		o.UserSpecifiedNamespace = args[0]
	}

	// if no namespace argument or flag value was specified, then there
	// is no need to generate a resulting context
	if len(o.UserSpecifiedNamespace) == 0 {
		return nil
	}

	o.UserSpecifiedContext, err = cmd.Flags().GetString("context")
	if err != nil {
		return err
	}

	o.UserSpecifiedCluster, err = cmd.Flags().GetString("cluster")
	if err != nil {
		return err
	}

	o.UserSpecifiedAuthInfo, err = cmd.Flags().GetString("user")
	if err != nil {
		return err
	}

	currentContext, exists := o.RawConfig.Contexts[o.RawConfig.CurrentContext]
	if !exists {
		return errNoContext
	}

	o.ResultingContext = api.NewContext()
	o.ResultingContext.Cluster = currentContext.Cluster
	o.ResultingContext.AuthInfo = currentContext.AuthInfo

	// if a target context is explicitly provided by the user,
	// use that as our reference for the final, resulting context
	if len(o.UserSpecifiedContext) > 0 {
		if userCtx, exists := o.RawConfig.Contexts[o.UserSpecifiedContext]; exists {
			o.ResultingContext = userCtx.DeepCopy()
		}
	}

	// override context info with user provided values
	o.ResultingContext.Namespace = o.UserSpecifiedNamespace

	if len(o.UserSpecifiedCluster) > 0 {
		o.ResultingContext.Cluster = o.UserSpecifiedCluster
	}
	if len(o.UserSpecifiedAuthInfo) > 0 {
		o.ResultingContext.AuthInfo = o.UserSpecifiedAuthInfo
	}

	return nil
}

func (o *DebugOptions) Validate() error {
	if len(o.RawConfig.CurrentContext) == 0 {
		return errNoContext
	}
	return nil
}

func (o *DebugOptions) Run() error {
	// TODO: implements getHostIP and openDebugStream logic
}
