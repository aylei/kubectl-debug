package plugin

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/user"
	"strconv"
	"sync"
	"time"

	"github.com/aylei/kubectl-debug/version"

	term "github.com/aylei/kubectl-debug/pkg/util"
	dockerterm "github.com/docker/docker/pkg/term"
	"github.com/spf13/cobra"
	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/uuid"
	"k8s.io/cli-runtime/pkg/genericclioptions"
	"k8s.io/client-go/kubernetes"
	coreclient "k8s.io/client-go/kubernetes/typed/core/v1"
	restclient "k8s.io/client-go/rest"
	"k8s.io/client-go/tools/portforward"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/client-go/tools/watch"
	"k8s.io/client-go/transport/spdy"
	"k8s.io/kubernetes/pkg/client/conditions"
	cmdutil "k8s.io/kubernetes/pkg/kubectl/cmd/util"
	"k8s.io/kubernetes/pkg/util/interrupt"
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

	# check version
	kubectl --version
`
	longDesc = `
Run a container in a running pod, this container will join the namespaces of an existing container of the pod.

You may set default configuration such as image and command in the config file, which locates in "~/.kube/debug-config" by default.
`
	defaultImage          = "nicolaka/netshoot:latest"
	defaultAgentPort      = 10027
	defaultConfigLocation = "/.kube/debug-config"
	defaultDaemonSetName  = "debug-agent"
	defaultDaemonSetNs    = "default"

	usageError = "expects 'debug POD_NAME' for debug command"

	defaultAgentImage             = "aylei/debug-agent:latest"
	defaultAgentPodNamePrefix     = "debug-agent-pod"
	defaultAgentPodNamespace      = "default"
	defaultAgentPodCpuRequests    = ""
	defaultAgentPodCpuLimits      = ""
	defaultAgentPodMemoryRequests = ""
	defaultAgentPodMemoryLimits   = ""

	defaultRegistrySecretName      = "kubectl-debug-registry-secret"
	defaultRegistrySecretNamespace = "default"

	defaultPortForward = true
	defaultAgentless   = true
)

// DebugOptions specify how to run debug container in a running pod
type DebugOptions struct {

	// Pod select options
	Namespace string
	PodName   string

	// Debug options
	Image                   string
	RegistrySecretName      string
	RegistrySecretNamespace string

	ContainerName       string
	Command             []string
	AgentPort           int
	AppName             string
	ConfigLocation      string
	Fork                bool
	ForkPodRetainLabels []string
	//used for agentless mode
	AgentLess  bool
	AgentImage string
	// agentPodName = agentPodNamePrefix + nodeName
	AgentPodName      string
	AgentPodNamespace string
	AgentPodNode      string
	AgentPodResource  agentPodResources

	Flags      *genericclioptions.ConfigFlags
	CoreClient coreclient.CoreV1Interface
	KubeCli    *kubernetes.Clientset
	Args       []string
	Config     *restclient.Config

	// use for port-forward
	RESTClient    *restclient.RESTClient
	PortForwarder portForwarder
	Ports         []string
	StopChannel   chan struct{}
	ReadyChannel  chan struct{}

	PortForward         bool
	DebugAgentDaemonSet string
	DebugAgentNamespace string

	genericclioptions.IOStreams

	wait sync.WaitGroup
}

type agentPodResources struct {
	CpuRequests    string
	CpuLimits      string
	MemoryRequests string
	MemoryLimits   string
}

// NewDebugOptions new debug options
func NewDebugOptions(streams genericclioptions.IOStreams) *DebugOptions {
	return &DebugOptions{
		Flags:     genericclioptions.NewConfigFlags(),
		IOStreams: streams,
		PortForwarder: &defaultPortForwarder{
			IOStreams: streams,
		},
	}
}

// NewDebugCmd returns a cobra command wrapping DebugOptions
func NewDebugCmd(streams genericclioptions.IOStreams) *cobra.Command {
	opts := NewDebugOptions(streams)

	cmd := &cobra.Command{
		Use:                   "debug POD [-c CONTAINER] -- COMMAND [args...]",
		DisableFlagsInUseLine: true,
		Short:                 "Run a container in a running pod",
		Long:                  longDesc,
		Example:               example,
		Version:               version.Version(),
		Run: func(c *cobra.Command, args []string) {
			argsLenAtDash := c.ArgsLenAtDash()
			cmdutil.CheckErr(opts.Complete(c, args, argsLenAtDash))
			cmdutil.CheckErr(opts.Validate())
			cmdutil.CheckErr(opts.Run())
		},
	}
	//cmd.Flags().BoolVarP(&opts.RetainContainer, "retain", "r", defaultRetain,
	//	fmt.Sprintf("Retain container after debug session closed, default to %s", defaultRetain))
	cmd.Flags().StringVar(&opts.Image, "image", "",
		fmt.Sprintf("Container Image to run the debug container, default to %s", defaultImage))
	cmd.Flags().StringVar(&opts.RegistrySecretName, "registry-secret-name", "",
		"private registry secret name, default is kubectl-debug-registry-secret")
	cmd.Flags().StringVar(&opts.RegistrySecretNamespace, "registry-secret-namespace", "",
		"private registry secret namespace, default is default")
	cmd.Flags().StringSliceVar(&opts.ForkPodRetainLabels, "fork-pod-retain-labels", []string{},
		"in fork mode the pod labels retain labels name list, default is not set")
	cmd.Flags().StringVarP(&opts.ContainerName, "container", "c", "",
		"Target container to debug, default to the first container in pod")
	cmd.Flags().IntVarP(&opts.AgentPort, "port", "p", 0,
		fmt.Sprintf("Agent port for debug cli to connect, default to %d", defaultAgentPort))
	cmd.Flags().StringVar(&opts.ConfigLocation, "debug-config", "",
		fmt.Sprintf("Debug config file, default to ~%s", defaultConfigLocation))
	cmd.Flags().BoolVar(&opts.Fork, "fork", false,
		"Fork a new pod for debugging (useful if the pod status is CrashLoopBackoff)")
	cmd.Flags().BoolVar(&opts.PortForward, "port-forward", true,
		fmt.Sprintf("Whether using port-forward to connect debug-agent, default to %t", defaultPortForward))
	cmd.Flags().StringVar(&opts.DebugAgentDaemonSet, "daemonset-name", opts.DebugAgentDaemonSet,
		"Debug agent daemonset name when using port-forward")
	cmd.Flags().StringVar(&opts.DebugAgentNamespace, "daemonset-ns", opts.DebugAgentNamespace,
		"Debug agent namespace, default to 'default'")
	// flags used for agentless mode.
	cmd.Flags().BoolVarP(&opts.AgentLess, "agentless", "a", true,
		fmt.Sprintf("Whether to turn on agentless mode. Agentless mode: debug target pod if there isn't an agent running on the target host, default to %t", defaultAgentless))
	cmd.Flags().StringVar(&opts.AgentImage, "agent-image", "",
		fmt.Sprintf("Agentless mode, the container Image to run the agent container , default to %s", defaultAgentImage))
	cmd.Flags().StringVar(&opts.AgentPodName, "agent-pod-name-prefix", "",
		fmt.Sprintf("Agentless mode, pod name prefix , default to %s", defaultAgentPodNamePrefix))
	cmd.Flags().StringVar(&opts.AgentPodNamespace, "agent-pod-namespace", "",
		fmt.Sprintf("Agentless mode, agent pod namespace, default to %s", defaultAgentPodNamespace))
	cmd.Flags().StringVar(&opts.AgentPodResource.CpuRequests, "agent-pod-cpu-requests", "",
		fmt.Sprintf("Agentless mode, agent pod cpu requests, default is not set"))
	cmd.Flags().StringVar(&opts.AgentPodResource.MemoryRequests, "agent-pod-memory-requests", "",
		fmt.Sprintf("Agentless mode, agent pod memory requests, default is not set"))
	cmd.Flags().StringVar(&opts.AgentPodResource.CpuLimits, "agent-pod-cpu-limits", "",
		fmt.Sprintf("Agentless mode, agent pod cpu limits, default is not set"))
	cmd.Flags().StringVar(&opts.AgentPodResource.MemoryLimits, "agent-pod-memory-limits", "",
		fmt.Sprintf("Agentless mode, agent pod memory limits, default is not set"))
	opts.Flags.AddFlags(cmd.Flags())

	return cmd
}

// Complete populate default values from KUBECONFIG file
func (o *DebugOptions) Complete(cmd *cobra.Command, args []string, argsLenAtDash int) error {
	o.Args = args
	if len(args) == 0 {
		return cmdutil.UsageErrorf(cmd, usageError)
	}

	var err error
	configLoader := o.Flags.ToRawKubeConfigLoader()
	o.Namespace, _, err = configLoader.Namespace()
	if err != nil {
		return err
	}

	matchVersionKubeConfigFlags := cmdutil.NewMatchVersionFlags(o.Flags)
	f := cmdutil.NewFactory(matchVersionKubeConfigFlags)
	o.RESTClient, err = f.RESTClient()
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
		if !os.IsNotExist(err) {
			// TODO: support verbosity level
			fmt.Fprintf(o.ErrOut, "error parsing configuration file: %v", err)
		}
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
	if len(o.RegistrySecretName) < 1 {
		if len(config.RegistrySecretName) > 0 {
			o.RegistrySecretName = config.RegistrySecretName
		} else {
			o.RegistrySecretName = defaultRegistrySecretName
		}
	}
	if len(o.RegistrySecretNamespace) < 1 {
		if len(config.RegistrySecretNamespace) > 0 {
			o.RegistrySecretNamespace = config.RegistrySecretNamespace
		} else {
			o.RegistrySecretNamespace = defaultRegistrySecretNamespace
		}
	}
	if len(o.ForkPodRetainLabels) < 1 {
		if len(config.ForkPodRetainLabels) > 0 {
			o.ForkPodRetainLabels = config.ForkPodRetainLabels
		}
	}
	if o.AgentPort < 1 {
		if config.AgentPort > 0 {
			o.AgentPort = config.AgentPort
		} else {
			o.AgentPort = defaultAgentPort
		}
	}
	if len(o.DebugAgentNamespace) < 1 {
		if len(config.DebugAgentNamespace) > 0 {
			o.DebugAgentNamespace = config.DebugAgentNamespace
		} else {
			o.DebugAgentNamespace = defaultDaemonSetNs
		}
	}
	if len(o.DebugAgentDaemonSet) < 1 {
		if len(config.DebugAgentDaemonSet) > 0 {
			o.DebugAgentDaemonSet = config.DebugAgentDaemonSet
		} else {
			o.DebugAgentDaemonSet = defaultDaemonSetName
		}
	}

	if len(o.AgentPodName) < 1 {
		if len(config.AgentPodNamePrefix) > 0 {
			o.AgentPodName = config.AgentPodNamePrefix
		} else {
			o.AgentPodName = defaultAgentPodNamePrefix
		}
	}

	if len(o.AgentImage) < 1 {
		if len(config.AgentImage) > 0 {
			o.AgentImage = config.AgentImage
		} else {
			o.AgentImage = defaultAgentImage
		}
	}

	if len(o.AgentPodNamespace) < 1 {
		if len(config.AgentPodNamespace) > 0 {
			o.AgentPodNamespace = config.AgentPodNamespace
		} else {
			o.AgentPodNamespace = defaultAgentPodNamespace
		}
	}

	if len(o.AgentPodResource.CpuRequests) < 1 {
		if len(config.AgentPodCpuRequests) > 0 {
			o.AgentPodResource.CpuRequests = config.AgentPodCpuRequests
		} else {
			o.AgentPodResource.CpuRequests = defaultAgentPodCpuRequests
		}
	}

	if len(o.AgentPodResource.MemoryRequests) < 1 {
		if len(config.AgentPodMemoryRequests) > 0 {
			o.AgentPodResource.MemoryRequests = config.AgentPodMemoryRequests
		} else {
			o.AgentPodResource.MemoryRequests = defaultAgentPodMemoryRequests
		}
	}

	if len(o.AgentPodResource.CpuLimits) < 1 {
		if len(config.AgentPodCpuLimits) > 0 {
			o.AgentPodResource.CpuLimits = config.AgentPodCpuLimits
		} else {
			o.AgentPodResource.CpuLimits = defaultAgentPodCpuLimits
		}
	}

	if len(o.AgentPodResource.MemoryLimits) < 1 {
		if len(config.AgentPodMemoryLimits) > 0 {
			o.AgentPodResource.MemoryLimits = config.AgentPodMemoryLimits
		} else {
			o.AgentPodResource.MemoryLimits = defaultAgentPodMemoryLimits
		}
	}

	if config.PortForward {
		o.PortForward = true
	}
	if config.Agentless {
		o.AgentLess = true
	}

	o.Ports = []string{strconv.Itoa(o.AgentPort)}
	o.Config, err = configLoader.ClientConfig()
	if err != nil {
		return err
	}
	clientset, err := kubernetes.NewForConfig(o.Config)
	if err != nil {
		return err
	}
	o.KubeCli = clientset
	o.CoreClient = clientset.CoreV1()
	o.StopChannel = make(chan struct{}, 1)
	o.ReadyChannel = make(chan struct{})
	return nil
}

// Validate validate
func (o *DebugOptions) Validate() error {
	if len(o.PodName) == 0 {
		return fmt.Errorf("pod name must be specified")
	}
	if len(o.Command) == 0 {
		return fmt.Errorf("you must specify at least one command for the container")
	}
	return nil
}

// TODO: refactor Run() spaghetti code
// Run run
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
	err = o.auth(pod)
	if err != nil {
		return err
	}
	// Launch debug launching pod in agentless mode.
	var agentPod *corev1.Pod
	if o.AgentLess {
		o.AgentPodNode = pod.Spec.NodeName
		o.AgentPodName = fmt.Sprintf("%s-%s", o.AgentPodName, uuid.NewUUID())
		agentPod = o.getAgentPod()
		agentPod, err = o.launchPod(agentPod)
		if err != nil {
			fmt.Fprintf(o.Out, "the agentPod is not running, you should check the reason and delete the failed agentPod and retry.\n")
			return err
		}
	}

	// in fork mode, we launch an new pod as a copy of target pod
	// and hack the entry point of the target container with sleep command
	// which keeps the container running.
	if o.Fork {
		// build the fork pod labels
		podLabels := o.buildForkPodLabels(pod)
		// copy pod and run
		pod = copyAndStripPod(pod, containerName, podLabels)
		pod, err = o.launchPod(pod)
		if err != nil {
			fmt.Fprintf(o.Out, "the ForkedPod is not running, you should check the reason and delete the failed ForkedPod and retry\n")
			o.deleteAgent(agentPod)
			return err
		}
	}

	if pod.Status.Phase == corev1.PodSucceeded || pod.Status.Phase == corev1.PodFailed {
		o.deleteAgent(agentPod)
		return fmt.Errorf("cannot debug in a completed pod; current phase is %s", pod.Status.Phase)
	}

	containerID, err := o.getContainerIDByName(pod, containerName)
	if err != nil {
		o.deleteAgent(agentPod)
		return err
	}

	t := o.setupTTY()
	var sizeQueue remotecommand.TerminalSizeQueue
	if t.Raw {
		// this call spawns a goroutine to monitor/update the terminal size
		sizeQueue = t.MonitorSize(t.GetSize())
		// unset p.Err if it was previously set because both stdout and stderr go over p.Out when tty is
		// true
		// o.ErrOut = nil
	}

	if o.PortForward {
		var agent *corev1.Pod
		if !o.AgentLess {
			// Agent is running
			daemonSet, err := o.KubeCli.AppsV1().DaemonSets(o.DebugAgentNamespace).Get(o.DebugAgentDaemonSet, v1.GetOptions{})
			if err != nil {
				return err
			}
			labelSet := labels.Set(daemonSet.Spec.Selector.MatchLabels)
			agents, err := o.CoreClient.Pods(o.DebugAgentNamespace).List(v1.ListOptions{
				LabelSelector: labelSet.String(),
			})
			if err != nil {
				return err
			}
			for i := range agents.Items {
				if agents.Items[i].Spec.NodeName == pod.Spec.NodeName {
					agent = &agents.Items[i]
					break
				}
			}
		} else {
			agent = agentPod
		}

		if agent == nil {
			return fmt.Errorf("there is no agent pod in the same node with your speficy pod %s", o.PodName)
		}
		fmt.Fprintf(o.Out, "pod %s PodIP %s, agentPodIP %s\n", o.PodName, pod.Status.PodIP, agent.Status.HostIP)
		err = o.runPortForward(agent)
		if err != nil {
			o.deleteAgent(agentPod)
			return err
		}
		// client can't access the node ip in the k8s cluster sometimes,
		// than we use forward ports to connect the specified pod and that will listen
		// on specified ports in localhost, the ports can not access until receive the
		// ready signal
		fmt.Fprintln(o.Out, "wait for forward port to debug agent ready...")
		<-o.ReadyChannel
	}

	fn := func() error {
		// TODO: refactor as kubernetes api style, reuse rbac mechanism of kubernetes
		var targetHost string
		if o.PortForward {
			targetHost = "localhost"
		} else {
			targetHost = pod.Status.HostIP
		}
		uri, err := url.Parse(fmt.Sprintf("http://%s:%d", targetHost, o.AgentPort))
		if err != nil {
			return err
		}
		uri.Path = fmt.Sprintf("/api/v1/debug")
		params := url.Values{}
		params.Add("image", o.Image)
		params.Add("container", containerID)
		var authStr string
		registrySecret, err := o.CoreClient.Secrets(o.RegistrySecretNamespace).Get(o.RegistrySecretName, v1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				authStr = ""
			} else {
				return err
			}
		} else {
			authStr = string(registrySecret.Data["authStr"])
		}
		params.Add("authStr", authStr)
		commandBytes, err := json.Marshal(o.Command)
		if err != nil {
			return err
		}
		params.Add("command", string(commandBytes))
		uri.RawQuery = params.Encode()
		return o.remoteExecute("POST", uri, o.Config, o.In, o.Out, o.ErrOut, t.Raw, sizeQueue)
	}

	// ensure forked pod is deleted on cancelation
	withCleanUp := func() error {
		return interrupt.Chain(nil, func() {
			if o.Fork {
				fmt.Fprintf(o.Out, "Start deleting forked pod %s \n\r", pod.Name)
				err := o.CoreClient.Pods(pod.Namespace).Delete(pod.Name, v1.NewDeleteOptions(0))
				if err != nil {
					// we may leak pod here, but we have nothing to do except noticing the user
					fmt.Fprintf(o.ErrOut, "failed to delete forked pod[Name:%s, Namespace:%s], consider manual deletion.\n\r", pod.Name, pod.Namespace)
				}
			}

			if o.PortForward {
				// close the port-forward
				if o.StopChannel != nil {
					close(o.StopChannel)
				}
			}
			// delete agent pod
			if o.AgentLess && agentPod != nil {
				fmt.Fprintf(o.Out, "Start deleting agent pod %s \n\r", pod.Name)
				o.deleteAgent(agentPod)
			}
		}).Run(fn)
	}

	if err := t.Safe(withCleanUp); err != nil {
		fmt.Fprintf(o.Out, "error execute remote, %v\n", err)
		return err
	}
	o.wait.Wait()
	return nil
}

func (o *DebugOptions) getContainerIDByName(pod *corev1.Pod, containerName string) (string, error) {
	for _, containerStatus := range pod.Status.ContainerStatuses {
		if containerStatus.Name != containerName {
			continue
		}
		// #52 if a pod is running but not ready(because of readiness probe), we can connect
		if containerStatus.State.Running == nil {
			return "", fmt.Errorf("container [%s] not running", containerName)
		}
		return containerStatus.ContainerID, nil
	}

	// #14 otherwise we should search for running init containers
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

func (o *DebugOptions) buildForkPodLabels(pod *corev1.Pod) map[string]string {
	podLabels := map[string]string{}
	for _, label := range o.ForkPodRetainLabels {
		for k, v := range pod.ObjectMeta.Labels {
			if label == k {
				podLabels[k] = v
			}
		}
	}
	return podLabels
}

// copyAndStripPod copy the given pod template, strip the probes and labels,
// and replace the entry point
func copyAndStripPod(pod *corev1.Pod, targetContainer string, podLabels map[string]string) *corev1.Pod {
	copied := &corev1.Pod{
		ObjectMeta: *pod.ObjectMeta.DeepCopy(),
		Spec:       *pod.Spec.DeepCopy(),
	}
	copied.Name = fmt.Sprintf("%s-%s-debug", pod.Name, uuid.NewUUID())
	copied.Labels = podLabels
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

// launchPod launch given pod until it's running
func (o *DebugOptions) launchPod(pod *corev1.Pod) (*corev1.Pod, error) {
	pod, err := o.CoreClient.Pods(pod.Namespace).Create(pod)
	if err != nil {
		return pod, err
	}

	watcher, err := o.CoreClient.Pods(pod.Namespace).Watch(v1.SingleObject(pod.ObjectMeta))
	if err != nil {
		return nil, err
	}
	// FIXME: hard code -> config
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	fmt.Fprintf(o.Out, "Waiting for pod %s to run...\n", pod.Name)
	event, err := watch.UntilWithoutRetry(ctx, watcher, conditions.PodRunning)
	if err != nil {
		fmt.Fprintf(o.ErrOut, "Error occurred while waiting for pod to run:  %v\n", err)
		return nil, err
	}
	pod = event.Object.(*corev1.Pod)
	return pod, nil
}

// getAgentPod construnct agentPod from agent pod template
func (o *DebugOptions) getAgentPod() *corev1.Pod {
	agentPod := &corev1.Pod{
		TypeMeta: v1.TypeMeta{
			Kind:       "Pod",
			APIVersion: "v1",
		},
		ObjectMeta: v1.ObjectMeta{
			Name:      o.AgentPodName,
			Namespace: o.AgentPodNamespace,
		},
		Spec: corev1.PodSpec{
			NodeName: o.AgentPodNode,
			Containers: []corev1.Container{
				{
					Name:            "debug-agent",
					Image:           o.AgentImage,
					ImagePullPolicy: corev1.PullAlways,
					LivenessProbe: &corev1.Probe{
						Handler: corev1.Handler{
							HTTPGet: &corev1.HTTPGetAction{
								Path: "/healthz",
								Port: intstr.FromInt(10027),
							},
						},
						InitialDelaySeconds: 10,
						PeriodSeconds:       10,
						SuccessThreshold:    1,
						TimeoutSeconds:      1,
						FailureThreshold:    3,
					},
					Resources: o.buildAgentResourceRequirements(),
					VolumeMounts: []corev1.VolumeMount{
						{
							Name:      "docker",
							MountPath: "/var/run/docker.sock",
						},
					},
					Ports: []corev1.ContainerPort{
						{
							Name:          "http",
							HostPort:      int32(o.AgentPort),
							ContainerPort: 10027,
						},
					},
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "docker",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/var/run/docker.sock",
						},
					},
				},
			},
			RestartPolicy: corev1.RestartPolicyNever,
		},
	}
	fmt.Fprintf(o.Out, "Agent Pod info: [Name:%s, Namespace:%s, Image:%s, HostPort:%d, ContainerPort:%d]\n", agentPod.ObjectMeta.Name, agentPod.ObjectMeta.Namespace, agentPod.Spec.Containers[0].Image, agentPod.Spec.Containers[0].Ports[0].HostPort, agentPod.Spec.Containers[0].Ports[0].ContainerPort)
	return agentPod
}

func (o *DebugOptions) runPortForward(pod *corev1.Pod) error {
	if pod.Status.Phase != corev1.PodRunning {
		return fmt.Errorf("unable to forward port because pod is not running. Current status=%v", pod.Status.Phase)
	}
	o.wait.Add(1)
	go func() {
		defer o.wait.Done()
		req := o.RESTClient.Post().
			Resource("pods").
			Namespace(pod.Namespace).
			Name(pod.Name).
			SubResource("portforward")
		o.PortForwarder.ForwardPorts("POST", req.URL(), o)
		fmt.Fprintln(o.Out, "end port-forward...")
	}()
	return nil
}

type portForwarder interface {
	ForwardPorts(method string, url *url.URL, opts *DebugOptions) error
}

type defaultPortForwarder struct {
	genericclioptions.IOStreams
}

// ForwardPorts forward ports
func (f *defaultPortForwarder) ForwardPorts(method string, url *url.URL, opts *DebugOptions) error {
	transport, upgrader, err := spdy.RoundTripperFor(opts.Config)
	if err != nil {
		return err
	}
	dialer := spdy.NewDialer(upgrader, &http.Client{Transport: transport}, method, url)
	fw, err := portforward.New(dialer, opts.Ports, opts.StopChannel, opts.ReadyChannel, f.Out, f.ErrOut)
	if err != nil {
		return err
	}
	return fw.ForwardPorts()
}

// auth checks if current user has permission to create pods/exec subresource.
func (o *DebugOptions) auth(pod *corev1.Pod) error {
	sarClient := o.KubeCli.AuthorizationV1()
	sar := &authorizationv1.SelfSubjectAccessReview{
		Spec: authorizationv1.SelfSubjectAccessReviewSpec{
			ResourceAttributes: &authorizationv1.ResourceAttributes{
				Namespace:   pod.Namespace,
				Verb:        "create",
				Group:       "",
				Resource:    "pods",
				Subresource: "exec",
				Name:        "",
			},
		},
	}
	response, err := sarClient.SelfSubjectAccessReviews().Create(sar)
	if err != nil {
		fmt.Fprintf(o.ErrOut, "Failed to create SelfSubjectAccessReview: %v \n", err)
		return err
	}
	if !response.Status.Allowed {
		denyReason := fmt.Sprintf("Current user has no permission to create pods/exec subresource in namespace:%s. Detail:", pod.Namespace)
		if len(response.Status.Reason) > 0 {
			denyReason = fmt.Sprintf("%s %v, ", denyReason, response.Status.Reason)
		}
		if len(response.Status.EvaluationError) > 0 {
			denyReason = fmt.Sprintf("%s %v", denyReason, response.Status.EvaluationError)
		}
		return fmt.Errorf(denyReason)
	}
	return nil
}

// delete the agent pod
func (o *DebugOptions) deleteAgent(agentPod *corev1.Pod) {
	// only with agentless flag we can delete the agent pod
	if !o.AgentLess {
		return
	}
	err := o.CoreClient.Pods(agentPod.Namespace).Delete(agentPod.Name, v1.NewDeleteOptions(0))
	if err != nil {
		fmt.Fprintf(o.ErrOut, "failed to delete agent pod[Name:%s, Namespace: %s], consider manual deletion.\nerror msg: %v", agentPod.Name, agentPod.Namespace, err)
	}
}

// build the agent pod Resource Requirements
func (o *DebugOptions) buildAgentResourceRequirements() corev1.ResourceRequirements {
	return getResourceRequirements(getResourceList(o.AgentPodResource.CpuRequests, o.AgentPodResource.MemoryRequests), getResourceList(o.AgentPodResource.CpuLimits, o.AgentPodResource.MemoryLimits))
}

func getResourceList(cpu, memory string) corev1.ResourceList {
	// catch error in resource.MustParse
	defer func() {
		if err := recover(); err != nil {
			fmt.Printf("Parse Resource list error: %v\n", err)
		}
	}()
	res := corev1.ResourceList{}
	if cpu != "" {
		res[corev1.ResourceCPU] = resource.MustParse(cpu)
	}
	if memory != "" {
		res[corev1.ResourceMemory] = resource.MustParse(memory)
	}
	return res
}

func getResourceRequirements(requests, limits corev1.ResourceList) corev1.ResourceRequirements {
	res := corev1.ResourceRequirements{}
	res.Requests = requests
	res.Limits = limits
	return res
}
