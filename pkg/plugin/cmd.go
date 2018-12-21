package plugin

import (
	"encoding/json"
	"fmt"
	"github.com/aylei/kubectl-debug/pkg/util"
	dockerterm "github.com/docker/docker/pkg/term"
	"github.com/spf13/cobra"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"net/url"
)

const (
	example = `
	# debug a container in the running pod, the first container will be picked by default
	kubectl debug POD_NAME

	# specify namespace or container
	kubectl debug --namespace foo POD_NAME -c CONTAINER_NAME

	# override the default troubleshooting image
	kubectl debug POD_NAME --image aylei/debug-jvm

	# override entrypoint of debug container
	kubectl debug POD_NAME --image aylei/debug-jvm /bin/bash
`
	defaultImage     = "aylei/troubleshoot:latest"
	defaultRetain    = false
	defaultAgentPort = 10027
)

var (
	errNoContext = fmt.Errorf("no context is currently set, use %q to select a new one", "kubectl config use-context <context>")
)

// DebugOptions specify how to run debug container in a running pod
type DebugOptions struct {

	// Pod select options
	Namespace string
	PodName   string

	// Debug options
	RetainContainer bool
	Image           string
	ContainerName   string
	Command         []string
	AgentPort       int

	Flags     *genericclioptions.ConfigFlags
	PodClient coreclient.PodsGetter
	Args      []string
	Config    *restclient.Config

	genericclioptions.IOStreams
}

func NewDebugOptions(streams genericclioptions.IOStreams) *DebugOptions {
	return &DebugOptions{Flags: genericclioptions.NewConfigFlags(), IOStreams: streams}
}

// NewDebugCmd returns a cobra command wrapping DebugOptions
func NewDebugCmd(streams genericclioptions.IOStreams) *cobra.Command {
	opts := NewDebugOptions(streams)

	cmd := &cobra.Command{
		Use: "debug POD [-c CONTAINER] -- COMMAND [args...]",
		DisableFlagsInUseLine: true,
		Short:   "Run a container in running pod",
		Example: example,
		Run: func(c *cobra.Command, args []string) {
			argsLenAtDash := c.ArgsLenAtDash()
			if err := opts.Complete(c, args, argsLenAtDash); err != nil {
				fmt.Println(err)
			}
			if err := opts.Validate(); err != nil {
				fmt.Println(err)
			}
			if err := opts.Run(); err != nil {
				fmt.Println(err)
			}
			return
		},
	}
	//cmd.Flags().BoolVarP(&opts.RetainContainer, "retain", "r", defaultRetain,
	//	fmt.Sprintf("Retain container after debug session closed, default to %s", defaultRetain))
	cmd.Flags().StringVar(&opts.Image, "image", defaultImage,
		fmt.Sprintf("Container Image to run the debug container, default to %s", defaultImage))
	cmd.Flags().StringVarP(&opts.ContainerName, "container", "c", "",
		"Target container to debug, default to the first container in pod")
	cmd.Flags().IntVarP(&opts.AgentPort, "port", "p", defaultAgentPort,
		fmt.Sprintf("Agent port for debug cli to connect, default to %d", defaultAgentPort))
	opts.Flags.AddFlags(cmd.Flags())

	return cmd
}

// Complete populate default values from KUBECONFIG file
func (o *DebugOptions) Complete(cmd *cobra.Command, args []string, argsLenAtDash int) error {
	o.Args = args
	if len(args) == 0 {
		return fmt.Errorf("error pod not specified")
	}
	o.PodName = args[0]
	o.Command = args[1:]
	if len(o.Command) < 1 {
		// default command is guaranteed in default container
		o.Command = []string{"bash"}
	}

	var err error
	configLoader := o.Flags.ToRawKubeConfigLoader()
	o.Namespace, _, err = configLoader.Namespace()
	if err != nil {
		return err
	}

	o.Config, err = configLoader.ClientConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(o.Config)
	if err != nil {
		return err
	}
	o.PodClient = clientset.CoreV1()

	return nil
}

func (o *DebugOptions) Validate() error {
	if len(o.PodName) == 0 {
		return fmt.Errorf("pod name must be specified")
	}
	if len(o.Command) == 0 {
		return fmt.Errorf("you must specify at least one command for the container")
	}
	return nil
}

func (o *DebugOptions) Run() error {
	fmt.Printf("Run command, namespace: %s, podName: %s, command: %v \n\r", o.Namespace, o.PodName, o.Command)

	pod, err := o.PodClient.Pods(o.Namespace).Get(o.PodName, v1.GetOptions{})
	if err != nil {
		return err
	}
	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("cannot exec into a container in a completed pod; current phase is %s", pod.Status.Phase)
	}
	hostIP := pod.Status.HostIP

	containerName := o.ContainerName
	if len(containerName) == 0 {
		if len(pod.Spec.Containers) > 1 {
			usageString := fmt.Sprintf("Defaulting container name to %s.", pod.Spec.Containers[0].Name)
			fmt.Fprintf(o.ErrOut, "%s\n\r", usageString)
		}
		containerName = pod.Spec.Containers[0].Name
	}

	containerId, err := o.getContainerIdByName(pod, containerName)
	if err != nil {
		return err
	}

	t := o.setupTTY()
	var sizeQueue remotecommand.TerminalSizeQueue
	if t.Raw {
		// this call spawns a goroutine to monitor/update the terminal size
		sizeQueue = t.MonitorSize(t.GetSize())
		// unset p.Err if it was previously set because both stdout and stderr go over p.Out when tty is
		// true
		o.ErrOut = nil
	}

	fn := func() error {

		// TODO: refactor as kubernetes api style, reuse rbac mechanism of kubernetes
		uri, err := url.Parse(fmt.Sprintf("http://%s:%d", hostIP, o.AgentPort))
		if err != nil {
			return err
		}
		uri.Path = fmt.Sprintf("/api/v1/debug")
		params := url.Values{}
		params.Add("image", o.Image)
		params.Add("container", containerId)
		bytes, err := json.Marshal(o.Command)
		if err != nil {
			return err
		}
		params.Add("command", string(bytes))
		uri.RawQuery = params.Encode()

		return o.remoteExecute("POST", uri, o.Config, o.In, o.Out, o.ErrOut, t.Raw, sizeQueue)
	}

	if err := t.Safe(fn); err != nil {
		fmt.Printf("error execute remote, %v\n", err)
		return err
	}

	return nil
}

func (o *DebugOptions) getContainerIdByName(pod *corev1.Pod, containerName string) (string, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if !containerStatus.Ready {
			return "", fmt.Errorf("container %s id not ready", containerName)
		}
		return containerStatus.ContainerID, nil
	}
	return "", fmt.Errorf("cannot find specified container %s", containerName)
}

func (o *DebugOptions) remoteExecute(
	method string,
	url *url.URL,
	config *restclient.Config,
	stdin io.Reader,
	stdout, stderr io.Writer,
	tty bool,
	terminalSizeQueue remotecommand.TerminalSizeQueue) error {

	exec, err := remotecommand.NewSPDYExecutor(config, method, url)
	if err != nil {
		return err
	}
	return exec.Stream(remotecommand.StreamOptions{
		Stdin:             stdin,
		Stdout:            stdout,
		Stderr:            stderr,
		Tty:               tty,
		TerminalSizeQueue: terminalSizeQueue,
	})
}

func (o *DebugOptions) setupTTY() term.TTY {
	t := term.TTY{
		Out: o.Out,
	}
	t.In = o.In
	t.Raw = true
	if !t.IsTerminalIn() {
		if o.ErrOut != nil {
			fmt.Fprintln(o.ErrOut, "Unable to use a TTY - input is not a terminal or the right kind of file")
		}
		return t
	}
	stdin, stdout, _ := dockerterm.StdStreams()
	o.In = stdin
	t.In = stdin
	if o.Out != nil {
		o.Out = stdout
		t.Out = stdout
	}
	return t
}
