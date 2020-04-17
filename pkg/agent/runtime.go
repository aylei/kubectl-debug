package agent

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"strings"
	"time"

	"github.com/aylei/kubectl-debug/pkg/nsenter"
	term "github.com/aylei/kubectl-debug/pkg/util"
	containerd "github.com/containerd/containerd"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	kubetype "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
)

type ContainerRuntimeScheme string

const (
	DockerScheme     ContainerRuntimeScheme = "docker"
	ContainerdScheme ContainerRuntimeScheme = "containerd"
)

type ContainerInfo struct {
	Pid               int
	MountDestinations []string
}

type RunConfig struct {
	context              context.Context
	timeout              time.Duration
	idOfContainerToDebug string
	image                string
	command              []string
	stdin                io.Reader
	stdout               io.WriteCloser
	stderr               io.WriteCloser
	tty                  bool
	resize               <-chan remotecommand.TerminalSize
}

func (c *RunConfig) getContextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(c.context, c.timeout)
}

type ContainerRuntime interface {
	PullImage(ctx context.Context, image string, authStr string, stdout io.WriteCloser) error
	ContainerInfo(ctx context.Context, targetContainerId string) (ContainerInfo, error)
	RunDebugContainer(cfg RunConfig) error
}

type DockerContainerRuntime struct {
	client *dockerclient.Client
}

var DockerContainerRuntimeImplementsContainerRuntime ContainerRuntime = (*DockerContainerRuntime)(nil)

func (c *DockerContainerRuntime) PullImage(ctx context.Context, image string, authStr string, stdout io.WriteCloser) error {
	authBytes := base64.URLEncoding.EncodeToString([]byte(authStr))
	out, err := c.client.ImagePull(ctx, image, types.ImagePullOptions{RegistryAuth: string(authBytes)})
	if err != nil {
		return err
	}
	defer out.Close()
	// write pull progress to user
	term.DisplayJSONMessagesStream(out, stdout, 1, true, nil)
	return nil
}

func (c *DockerContainerRuntime) ContainerInfo(ctx context.Context, targetContainerId string) (ContainerInfo, error) {
	var ret ContainerInfo
	cntnr, err := c.client.ContainerInspect(ctx, targetContainerId)
	if err != nil {
		return ContainerInfo{}, err
	}
	ret.Pid = cntnr.State.Pid
	for _, mount := range cntnr.Mounts {
		ret.MountDestinations = append(ret.MountDestinations, mount.Destination)
	}
	return ret, nil
}

func (c *DockerContainerRuntime) RunDebugContainer(cfg RunConfig) error {

	createdBody, err := c.CreateContainer(cfg)
	if err != nil {
		return err
	}
	if err := c.StartContainer(cfg, createdBody.ID); err != nil {
		return err
	}

	defer c.CleanContainer(cfg, createdBody.ID)

	cfg.stdout.Write([]byte("container created, open tty...\n\r"))

	// from now on, should pipe stdin to the container and no long read stdin
	// close(m.stopListenEOF)

	return c.AttachToContainer(cfg, createdBody.ID)
}

func (c *DockerContainerRuntime) CreateContainer(cfg RunConfig) (*container.ContainerCreateCreatedBody, error) {

	config := &container.Config{
		Entrypoint: strslice.StrSlice(cfg.command),
		Image:      cfg.image,
		Tty:        true,
		OpenStdin:  true,
		StdinOnce:  true,
	}
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(c.containerMode(cfg.idOfContainerToDebug)),
		UsernsMode:  container.UsernsMode(c.containerMode(cfg.idOfContainerToDebug)),
		IpcMode:     container.IpcMode(c.containerMode(cfg.idOfContainerToDebug)),
		PidMode:     container.PidMode(c.containerMode(cfg.idOfContainerToDebug)),
		CapAdd:      strslice.StrSlice([]string{"SYS_PTRACE", "SYS_ADMIN"}),
	}
	ctx, cancel := cfg.getContextWithTimeout()
	defer cancel()
	body, err := c.client.ContainerCreate(ctx, config, hostConfig, nil, "")
	if err != nil {
		return nil, err
	}
	return &body, nil
}

func (c *DockerContainerRuntime) containerMode(idOfCntnrToDbg string) string {
	return fmt.Sprintf("container:%s", idOfCntnrToDbg)
}

// Run a new container, this container will join the network,
// mount, and pid namespace of the given container
func (c *DockerContainerRuntime) StartContainer(cfg RunConfig, id string) error {
	ctx, cancel := cfg.getContextWithTimeout()
	defer cancel()
	err := c.client.ContainerStart(ctx, id, types.ContainerStartOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (c *DockerContainerRuntime) CleanContainer(cfg RunConfig, id string) {
	// cleanup procedure should use background context
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	// wait the container gracefully exit
	statusCh, errCh := c.client.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	var rmErr error
	select {
	case err := <-errCh:
		if err != nil {
			log.Println("error waiting container exit, kill with --force")
			// timeout or error occurs, try force remove anywawy
			rmErr = c.RmContainer(cfg, id, true)
		}
	case <-statusCh:
		rmErr = c.RmContainer(cfg, id, false)
	}
	if rmErr != nil {
		log.Printf("error remove container: %s \n", id)
	} else {
		log.Printf("Debug session end, debug container %s removed", id)
	}
}

func (c *DockerContainerRuntime) RmContainer(cfg RunConfig, id string, force bool) error {
	// cleanup procedure should use background context
	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()
	return c.client.ContainerRemove(ctx, id,
		types.ContainerRemoveOptions{
			Force: true,
		})
}

// AttachToContainer do `docker attach`.  Blocks until container I/O complete
func (c *DockerContainerRuntime) AttachToContainer(cfg RunConfig, container string) error {
	HandleResizing(cfg.resize, func(size remotecommand.TerminalSize) {
		c.resizeContainerTTY(cfg, container, uint(size.Height), uint(size.Width))
	})

	opts := types.ContainerAttachOptions{
		Stream: true,
		Stdin:  cfg.stdin != nil,
		Stdout: cfg.stdout != nil,
		Stderr: cfg.stderr != nil,
	}
	ctx, cancel := cfg.getContextWithTimeout()
	defer cancel()
	resp, err := c.client.ContainerAttach(ctx, container, opts)
	if err != nil {
		return err
	}
	defer resp.Close()

	return c.holdHijackedConnection(cfg, resp)
}

func (c *DockerContainerRuntime) resizeContainerTTY(cfg RunConfig, id string, height, width uint) error {
	ctx, cancel := cfg.getContextWithTimeout()
	defer cancel()
	return c.client.ContainerResize(ctx, id, types.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

// holdHijackedConnection hold the HijackedResponse, redirect the inputStream to the connection, and redirect the response
// stream to stdout and stderr. NOTE: If needed, we could also add context in this function.
func (c *DockerContainerRuntime) holdHijackedConnection(cfg RunConfig, resp types.HijackedResponse) error {
	receiveStdout := make(chan error)
	if cfg.stdout != nil || cfg.stderr != nil {
		go func() {
			receiveStdout <- c.redirectResponseToOutputStream(cfg, resp.Reader)
		}()
	}

	stdinDone := make(chan struct{})
	go func() {
		if cfg.stdin != nil {
			io.Copy(resp.Conn, cfg.stdin)
		}
		resp.CloseWrite()
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		return err
	case <-stdinDone:
		if cfg.stdout != nil || cfg.stderr != nil {
			return <-receiveStdout
		}
	}
	return nil
}

func (c *DockerContainerRuntime) redirectResponseToOutputStream(cfg RunConfig, resp io.Reader) error {
	var stdout io.Writer = cfg.stdout
	if stdout == nil {
		stdout = ioutil.Discard
	}
	var stderr io.Writer = cfg.stderr
	if stderr == nil {
		stderr = ioutil.Discard
	}
	var err error
	if cfg.tty {
		_, err = io.Copy(stdout, resp)
	} else {
		_, err = stdcopy.StdCopy(stdout, stderr, resp)
	}
	return err
}

type ContainerdContainerRuntime struct {
	client *containerd.Client
}

var ContainerdContainerRuntimeImplementsContainerRuntime ContainerRuntime = (*ContainerdContainerRuntime)(nil)

func (c *ContainerdContainerRuntime) PullImage(ctx context.Context, image string, authStr string, stdout io.WriteCloser) error {
	return nil
}

func (c *ContainerdContainerRuntime) ContainerInfo(ctx context.Context, targetContainerId string) (ContainerInfo, error) {
	return ContainerInfo{}, nil
}

func (c *ContainerdContainerRuntime) RunDebugContainer(cfg RunConfig) error {
	return nil
}

// DebugAttacher implements Attacher
// we use this struct in order to inject debug info (image, command) in the debug procedure
type DebugAttacher struct {
	containerRuntime     ContainerRuntime
	image                string
	authStr              string
	lxcfsEnabled         bool
	command              []string
	timeout              time.Duration
	idOfContainerToDebug string
	verbosity            int

	// control the preparing of debug container
	stopListenEOF chan struct{}
	context       context.Context
	cancel        context.CancelFunc
}

var DebugAttacherImplementsAttacher kubeletremote.Attacher = (*DebugAttacher)(nil)

// Implement kubeletremote.Attacher
func (a *DebugAttacher) AttachContainer(name string, uid kubetype.UID, container string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	if a.verbosity > 0 {
		log.Println("Enter")
	}

	return a.DebugContainer(RunConfig{
		context:              a.context,
		timeout:              a.timeout,
		idOfContainerToDebug: a.idOfContainerToDebug,
		image:                a.image,
		command:              a.command,
		stdin:                in,
		stdout:               out,
		stderr:               err,
		tty:                  tty,
		resize:               resize,
	})
}

// DebugContainer executes the main debug flow
func (m *DebugAttacher) DebugContainer(cfg RunConfig) error {

	if m.verbosity > 0 {
		log.Println("Enter")
	}
	log.Printf("Accept new debug reqeust:\n\t target container: %s \n\t image: %s \n\t command: %v \n", m.idOfContainerToDebug, m.image, m.command)

	// the following steps may takes much time,
	// so we listen to EOF from stdin
	// which helps user to terminate the procedure proactively

	// FIXME: the following logic will 'eat' a character
	//var buf bytes.Buffer
	//tee := io.TeeReader(stdin, &buf)
	//go func() {
	//	p := make([]byte, 4)
	//	OUTER:
	//	for {
	//		select {
	//		case <- m.stopListenEOF:
	//			break OUTER
	//		default:
	//			n, err := tee.Read(p)
	//			// 4 -> EOT
	//			if (n > 0 && binary.LittleEndian.Uint32(p) == 4) || err == io.EOF {
	//				log.Println("receive ctrl-d or EOF when preparing debug container, cancel session")
	//				m.cancel()
	//				break OUTER
	//			}
	//		}
	//	}
	//} ()
	// step 0: set container procfs correct by lxcfs
	cfg.stdout.Write([]byte(fmt.Sprintf("set container procfs correct %t .. \n\r", m.lxcfsEnabled)))
	if m.lxcfsEnabled {
		if err := CheckLxcfsMount(); err != nil {
			return err
		}

		if err := m.SetContainerLxcfs(cfg); err != nil {
			return err
		}
	}

	// step 1: pull image
	cfg.stdout.Write([]byte(fmt.Sprintf("pulling image %s... \n\r", m.image)))
	err := m.containerRuntime.PullImage(m.context, m.image, m.authStr, cfg.stdout)
	if err != nil {
		return err
	}

	// step 2: run debug container (join the namespaces of target container)
	cfg.stdout.Write([]byte("starting debug container...\n\r"))
	return m.containerRuntime.RunDebugContainer(cfg)
}

func (m *DebugAttacher) SetContainerLxcfs(cfg RunConfig) error {
	ctx, cancel := cfg.getContextWithTimeout()
	defer cancel()
	cntnrInf, err := m.containerRuntime.ContainerInfo(ctx, m.idOfContainerToDebug)
	if err != nil {
		return err
	}
	for _, mntDst := range cntnrInf.MountDestinations {
		if mntDst == LxcfsRootDir {
			log.Printf("remount lxcfs when the rootdir of lxcfs of target container has been mounted. \n\t ")
			for _, procfile := range LxcfsProcFiles {
				nsenter := &nsenter.MountNSEnter{
					Target:     cntnrInf.Pid,
					MountLxcfs: true,
				}
				_, stderr, err := nsenter.Execute("--", "mount", "-B", LxcfsHomeDir+procfile, procfile)
				if err != nil {
					log.Printf("bind mount lxcfs files failed. \n\t reason: %s", stderr)
					return err
				}
			}
		}
	}

	return nil
}

// RuntimeManager is responsible for docker operation
type RuntimeManager struct {
	dockerClient         *dockerclient.Client
	containerdClient     *containerd.Client
	timeout              time.Duration
	verbosity            int
	idOfContainerToDebug string
	containerScheme      ContainerRuntimeScheme
}

func NewRuntimeManager(srvCfg Config, containerUri string, verbosity int) (*RuntimeManager, error) {
	if len(containerUri) < 1 {
		return nil, errors.New("target container id must be provided")
	}
	containerUriParts := strings.SplitN(containerUri, "://", 2)
	if len(containerUriParts) != 2 {
		msg := fmt.Sprintf("target container id must have form scheme:id but was %v", containerUri)
		log.Println(msg)
		return nil, errors.New(msg)
	}
	containerScheme := ContainerRuntimeScheme(containerUriParts[0])
	idOfContainerToDebug := containerUriParts[1]

	// 2020-04-09 d : TODO Need to touch this in order to support containerd
	var dockerClient *dockerclient.Client
	var containerdClient *containerd.Client
	switch containerScheme {
	case DockerScheme:
		{
			var err error
			dockerClient, err = dockerclient.NewClient(srvCfg.DockerEndpoint, "", nil, nil)
			if err != nil {
				return nil, err
			}
		}
	case ContainerdScheme:
		{
			var err error
			containerdClient, err = containerd.New(srvCfg.ContainerdEndpoint)
			if err != nil {
				return nil, err
			}
			return nil, errors.New("Containerd support is not yet complete.")
		}
	default:
		{
			msg := "only docker and containerd container runtimes are suppored right now"
			log.Println(msg)
			return nil, errors.New(msg)
		}
	}

	return &RuntimeManager{
		dockerClient:         dockerClient,
		containerdClient:     containerdClient,
		timeout:              srvCfg.RuntimeTimeout,
		verbosity:            verbosity,
		idOfContainerToDebug: idOfContainerToDebug,
		containerScheme:      containerScheme,
	}, nil
}

// GetAttacher returns an implementation of Attacher
func (m *RuntimeManager) GetAttacher(image, authStr string, lxcfsEnabled bool, command []string, context context.Context, cancel context.CancelFunc) kubeletremote.Attacher {
	var containerRuntime ContainerRuntime
	if m.dockerClient != nil {
		containerRuntime = &DockerContainerRuntime{
			client: m.dockerClient,
		}
	} else {
		containerRuntime = &ContainerdContainerRuntime{
			client: m.containerdClient,
		}
	}
	return &DebugAttacher{
		containerRuntime:     containerRuntime,
		image:                image,
		authStr:              authStr,
		lxcfsEnabled:         lxcfsEnabled,
		command:              command,
		context:              context,
		idOfContainerToDebug: m.idOfContainerToDebug,
		verbosity:            m.verbosity,
		timeout:              m.timeout,
		cancel:               cancel,
		stopListenEOF:        make(chan struct{}),
	}
}
