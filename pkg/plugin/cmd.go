package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/aylei/kubectl-debug/pkg/util"
	dockerterm "github.com/docker/docker/pkg/term"
	"github.com/spf13/cobra"
	"io"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/tools/watch"
	"k8s.io/kubernetes/pkg/client/conditions"
	"k8s.io/kubernetes/pkg/util/interrupt"
	"log"
	"net/url"
	"os/user"
	"time"
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

	# override the debug config file
	kubectl debug POD_NAME --debug-config ./debug-config.yml
`
	longDesc = `
Run a container in a running pod, this container will join the namespaces of an existing container of the pod.

You may set default configuration such as image and command in the config file, which locates in "~/.kube/debug-config" by default.
`
	defaultImage          = "nicolaka/netshoot:latest"
	defaultAgentPort      = 10027
	defaultConfigLocation = "/.kube/debug-config"
)

// DebugOptions specify how to run debug container in a running pod
type DebugOptions struct {

	// Pod select options
	Namespace string
	PodName   string

	// Debug options
	Image          string
	ContainerName  string
	Command        []string
	AgentPort      int
	ConfigLocation string
	Fork           bool

	Flags      *genericclioptions.ConfigFlags
	CoreClient coreclient.CoreV1Interface
	Args       []string
	Config     *restclient.Config

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
		Short:   "Run a container in a running pod",
		Long:    longDesc,
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
		},
	}
	//cmd.Flags().BoolVarP(&opts.RetainContainer, "retain", "r", defaultRetain,
	//	fmt.Sprintf("Retain container after debug session closed, default to %s", defaultRetain))
	cmd.Flags().StringVar(&opts.Image, "image", "",
		fmt.Sprintf("Container Image to run the debug container, default to %s", defaultImage))
	cmd.Flags().StringVarP(&opts.ContainerName, "container", "c", "",
		"Target container to debug, default to the first container in pod")
	cmd.Flags().IntVarP(&opts.AgentPort, "port", "p", 0,
		fmt.Sprintf("Agent port for debug cli to connect, default to %d", defaultAgentPort))
	cmd.Flags().StringVar(&opts.ConfigLocation, "debug-config", "",
		fmt.Sprintf("Debug config file, default to ~%s", defaultConfigLocation))
	cmd.Flags().BoolVar(&opts.Fork, "fork", false,
		"Fork a new pod for debugging (useful if the pod status is CrashLoopBackoff)")
	opts.Flags.AddFlags(cmd.Flags())

	return cmd
}

// Complete populate default values from KUBECONFIG file
func (o *DebugOptions) Complete(cmd *cobra.Command, args []string, argsLenAtDash int) error {
	o.Args = args
	if len(args) == 0 {
		return fmt.Errorf("error pod not specified")
	}

	var err error
	configLoader := o.Flags.ToRawKubeConfigLoader()
	o.Namespace, _, err = configLoader.Namespace()
	if err != nil {
		return err
	}

	o.PodName = args[0]

	// read defaults from config file
	configFile := o.ConfigLocation
	if len(o.ConfigLocation) < 1 {
		usr, err := user.Current()
		if err == nil {
			configFile = usr.HomeDir + defaultConfigLocation
		}
	}
	config, err := LoadFile(configFile)
	if err != nil {
		log.Println("error loading file ", err)
		config = &Config{}
	}

	// combine defaults, config file and user parameters
	o.Command = args[1:]
	if len(o.Command) < 1 {
		if len(config.Command) > 0 {
			o.Command = config.Command
		} else {
			o.Command = []string{"bash"}
		}
	}
	if len(o.Image) < 1 {
		if len(config.Image) > 0 {
			o.Image = config.Image
		} else {
			o.Image = defaultImage
		}
	}
	if o.AgentPort < 1 {
		if config.AgentPort > 0 {
			o.AgentPort = config.AgentPort
		} else {
			o.AgentPort = defaultAgentPort
		}
	}

	o.Config, err = configLoader.ClientConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(o.Config)
	if err != nil {
		return err
	}
	o.CoreClient = clientset.CoreV1()

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

	pod, err := o.CoreClient.Pods(o.Namespace).Get(o.PodName, v1.GetOptions{})
	if err != nil {
		return err
	}

	containerName := o.ContainerName
	if len(containerName) == 0 {
		if len(pod.Spec.Containers) > 1 {
			usageString := fmt.Sprintf("Defaulting container name to %s.", pod.Spec.Containers[0].Name)
			fmt.Fprintf(o.ErrOut, "%s\n\r", usageString)
		}
		containerName = pod.Spec.Containers[0].Name
	}

	// in fork mode, we launch an new pod as a copy of target pod
	// and hack the entry point of the target container with sleep command
	// which keeps the container running.
	if o.Fork {
		pod = copyAndStripPod(pod, containerName)
		pod, err = o.CoreClient.Pods(pod.Namespace).Create(pod)
		if err != nil {
			return err
		}
		watcher, err := o.CoreClient.Pods(pod.Namespace).Watch(v1.SingleObject(pod.ObjectMeta))
		if err != nil {
			return err
		}
		// FIXME: hard code -> config
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		log.Println("waiting for forked container running...")
		event, err := watch.UntilWithoutRetry(ctx, watcher, conditions.PodRunning)
		if err != nil {
			return err
		}
		pod = event.Object.(*corev1.Pod)
	}

	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		return fmt.Errorf("cannot debug in a completed pod; current phase is %s", pod.Status.Phase)
	}
	hostIP := pod.Status.HostIP

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

	// ensure forked pod is deleted on cancelation
	withCleanUp := func() error {
		return interrupt.Chain(nil, func() {
			if o.Fork {
				err := o.CoreClient.Pods(pod.Namespace).Delete(pod.Name, v1.NewDeleteOptions(0))
				if err != nil {
					// we may leak pod here, but we have nothing to do except noticing the user
					log.Printf("failed to delete pod %s, consider manual deletion.", pod.Name)
				}
			}
		}).Run(fn)
	}

	if err := t.Safe(withCleanUp); err != nil {
		fmt.Printf("error execute remote, %v\n", err)
		return err
	}

	return nil
}

func (o *DebugOptions) getContainerIdByName(pod *corev1.Pod, containerName string) (string, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name != containerName {
			continue
		}
		if !containerStatus.Ready {
			return "", fmt.Errorf("container [%s] not ready", containerName)
		}
		return containerStatus.ContainerID, nil
	}

	// #14 otherwise we should for running search init containers
	for _, initContainerStatus := range pod.Status.InitContainerStatuses {
		if initContainerStatus.Name != containerName {
			continue
		}
		if initContainerStatus.State.Running == nil {
			return "", fmt.Errorf("init container [%s] is not running", containerName)
		}
		return initContainerStatus.ContainerID, nil
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

// copyAndStripPod copy the given pod template, strip the probes and labels,
// and replace the entry point
func copyAndStripPod(pod *corev1.Pod, targetContainer string) *corev1.Pod {
	copied := &corev1.Pod{
		ObjectMeta: *pod.ObjectMeta.DeepCopy(),
		Spec:       *pod.Spec.DeepCopy(),
	}
	copied.Name = fmt.Sprintf("%s-%s-debug", pod.Name, uuid.NewUUID())
	copied.Labels = nil
	copied.Spec.RestartPolicy = corev1.RestartPolicyNever
	for i, c := range copied.Spec.Containers {
		copied.Spec.Containers[i].LivenessProbe = nil
		copied.Spec.Containers[i].ReadinessProbe = nil
		if c.Name == targetContainer {
			// Hack, infinite sleep command to keep the container running
			copied.Spec.Containers[i].Command = []string{"sh", "-c", "--"}
			copied.Spec.Containers[i].Args = []string{"while true; do sleep 30; done;"}
		}
	}
	copied.ResourceVersion = ""
	copied.UID = ""
	copied.SelfLink = ""
	copied.CreationTimestamp = v1.Time{}
	copied.OwnerReferences = []v1.OwnerReference{}

	return copied
}
