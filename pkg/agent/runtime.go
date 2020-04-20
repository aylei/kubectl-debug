package agent

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/aylei/kubectl-debug/pkg/nsenter"
	term "github.com/aylei/kubectl-debug/pkg/util"
	containerd "github.com/containerd/containerd"
	"github.com/containerd/containerd/cio"
	"github.com/containerd/containerd/content"
	"github.com/containerd/containerd/errdefs"
	glog "github.com/containerd/containerd/log"
	"github.com/containerd/containerd/namespaces"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/containerd/pkg/progress"
	"github.com/containerd/containerd/remotes"
	"github.com/containerd/containerd/remotes/docker"
	"github.com/containerd/typeurl"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/opencontainers/go-digest"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"github.com/opencontainers/runtime-spec/specs-go"
	kubetype "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
)

type ContainerRuntimeScheme string

const (
	DockerScheme     ContainerRuntimeScheme = "docker"
	ContainerdScheme ContainerRuntimeScheme = "containerd"
	KubectlDebugNS   string                 = "kubectl-debug"
	K8NS             string                 = "k8s.io"
)

type ContainerInfo struct {
	Pid               int64
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
	PullImage(ctx context.Context, image string,
		skipTLS bool, authStr string,
		stdout io.WriteCloser) error
	ContainerInfo(ctx context.Context, targetContainerId string) (ContainerInfo, error)
	RunDebugContainer(cfg RunConfig) error
}

type DockerContainerRuntime struct {
	client *dockerclient.Client
}

var DockerContainerRuntimeImplementsContainerRuntime ContainerRuntime = (*DockerContainerRuntime)(nil)

func (c *DockerContainerRuntime) PullImage(ctx context.Context,
	image string, skipTLS bool, authStr string,
	stdout io.WriteCloser) error {
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
	ret.Pid = int64(cntnr.State.Pid)
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
	image  containerd.Image
	pid    int64
}

var ContainerdContainerRuntimeImplementsContainerRuntime ContainerRuntime = (*ContainerdContainerRuntime)(nil)

var PushTracker = docker.NewInMemoryTracker()

type jobs struct {
	name     string
	added    map[digest.Digest]struct{}
	descs    []ocispec.Descriptor
	mu       sync.Mutex
	resolved bool
}

func (j *jobs) isResolved() bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	return j.resolved
}

func (j *jobs) jobs() []ocispec.Descriptor {
	j.mu.Lock()
	defer j.mu.Unlock()

	var descs []ocispec.Descriptor
	return append(descs, j.descs...)
}

func newJobs(name string) *jobs {
	return &jobs{
		name:  name,
		added: map[digest.Digest]struct{}{},
	}
}

type StatusInfo struct {
	Ref       string
	Status    string
	Offset    int64
	Total     int64
	StartedAt time.Time
	UpdatedAt time.Time
}

func Display(w io.Writer, statuses []StatusInfo, start time.Time) {
	var total int64
	for _, status := range statuses {
		total += status.Offset
		switch status.Status {
		case "downloading", "uploading":
			var bar progress.Bar
			if status.Total > 0.0 {
				bar = progress.Bar(float64(status.Offset) / float64(status.Total))
			}
			fmt.Fprintf(w, "%s:\t%s\t%40r\t%8.8s/%s\t\r\n",
				status.Ref,
				status.Status,
				bar,
				progress.Bytes(status.Offset), progress.Bytes(status.Total))
		case "resolving", "waiting":
			bar := progress.Bar(0.0)
			fmt.Fprintf(w, "%s:\t%s\t%40r\t\r\n",
				status.Ref,
				status.Status,
				bar)
		default:
			bar := progress.Bar(1.0)
			fmt.Fprintf(w, "%s:\t%s\t%40r\t\r\n",
				status.Ref,
				status.Status,
				bar)
		}
	}

	fmt.Fprintf(w, "elapsed: %-4.1fs\ttotal: %7.6v\t(%v)\t\r\n",
		time.Since(start).Seconds(),
		// TODO(stevvooe): These calculations are actually way off.
		// Need to account for previously downloaded data. These
		// will basically be right for a download the first time
		// but will be skewed if restarting, as it includes the
		// data into the start time before.
		progress.Bytes(total),
		progress.NewBytesPerSecond(total, time.Since(start)))
}

func showProgress(ctx context.Context, ongoing *jobs, cs content.Store, out io.Writer) {
	var (
		ticker   = time.NewTicker(100 * time.Millisecond)
		fw       = progress.NewWriter(out)
		start    = time.Now()
		statuses = map[string]StatusInfo{}
		done     bool
	)
	defer ticker.Stop()

outer:
	for {
		select {
		case <-ticker.C:
			fw.Flush()

			tw := tabwriter.NewWriter(fw, 1, 8, 1, ' ', 0)

			resolved := "resolved"
			if !ongoing.isResolved() {
				resolved = "resolving"
			}
			statuses[ongoing.name] = StatusInfo{
				Ref:    ongoing.name,
				Status: resolved,
			}
			keys := []string{ongoing.name}

			activeSeen := map[string]struct{}{}
			if !done {
				active, err := cs.ListStatuses(ctx, "")
				if err != nil {
					glog.G(ctx).WithError(err).Error("active check failed")
					continue
				}
				// update status of active entries!
				for _, active := range active {
					statuses[active.Ref] = StatusInfo{
						Ref:       active.Ref,
						Status:    "downloading",
						Offset:    active.Offset,
						Total:     active.Total,
						StartedAt: active.StartedAt,
						UpdatedAt: active.UpdatedAt,
					}
					activeSeen[active.Ref] = struct{}{}
				}
			}

			// now, update the items in jobs that are not in active
			for _, j := range ongoing.jobs() {
				key := remotes.MakeRefKey(ctx, j)
				keys = append(keys, key)
				if _, ok := activeSeen[key]; ok {
					continue
				}

				status, ok := statuses[key]
				if !done && (!ok || status.Status == "downloading") {
					info, err := cs.Info(ctx, j.Digest)
					if err != nil {
						if !errdefs.IsNotFound(err) {
							glog.G(ctx).WithError(err).Errorf("failed to get content info")
							continue outer
						} else {
							statuses[key] = StatusInfo{
								Ref:    key,
								Status: "waiting",
							}
						}
					} else if info.CreatedAt.After(start) {
						statuses[key] = StatusInfo{
							Ref:       key,
							Status:    "done",
							Offset:    info.Size,
							Total:     info.Size,
							UpdatedAt: info.CreatedAt,
						}
					} else {
						statuses[key] = StatusInfo{
							Ref:    key,
							Status: "exists",
						}
					}
				} else if done {
					if ok {
						if status.Status != "done" && status.Status != "exists" {
							status.Status = "done"
							statuses[key] = status
						}
					} else {
						statuses[key] = StatusInfo{
							Ref:    key,
							Status: "done",
						}
					}
				}
			}

			var ordered []StatusInfo
			for _, key := range keys {
				ordered = append(ordered, statuses[key])
			}

			Display(tw, ordered, start)
			tw.Flush()

			if done {
				fw.Flush()
				return
			}
		case <-ctx.Done():
			done = true // allow ui to update once more
		}
	}
}

func (c *ContainerdContainerRuntime) PullImage(
	ctx context.Context, image string, skipTLS bool,
	authStr string,
	stdout io.WriteCloser) error {

	ctx = namespaces.WithNamespace(ctx, KubectlDebugNS)

	ongoing := newJobs(image)
	pctx, stopProgress := context.WithCancel(ctx)
	progress := make(chan struct{})
	go func() {
		if stdout != nil {
			// no progress bar, because it hides some debug logs
			showProgress(pctx, ongoing, c.client.ContentStore(), stdout)
		}
		close(progress)
	}()

	rslvrOpts := docker.ResolverOptions{
		Tracker: PushTracker,
	}

	rmtOpts := []containerd.RemoteOpt{
		containerd.WithPullUnpack,
	}

	crds := strings.Split(authStr, ":")
	var useCrds = len(crds) == 2
	if useCrds || skipTLS {
		tr := &http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
				DualStack: true,
			}).DialContext,
			MaxIdleConns:        10,
			IdleConnTimeout:     30 * time.Second,
			TLSHandshakeTimeout: 10 * time.Second,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: skipTLS,
			},
			ExpectContinueTimeout: 5 * time.Second,
		}

		rslvrOpts.Client = &http.Client{
			Transport: tr,
		}

		if useCrds {
			crdsClbck := func(host string) (string, string, error) {
				return crds[0], crds[1], nil
			}

			authOpts := []docker.AuthorizerOpt{
				docker.WithAuthClient(rslvrOpts.Client), docker.WithAuthCreds(crdsClbck),
			}

			rslvrOpts.Authorizer = docker.NewDockerAuthorizer(authOpts...)
		}
		rmtOpts = append(rmtOpts, containerd.WithResolver(docker.NewResolver(rslvrOpts)))
	}

	var err error
	c.image, err = c.client.Pull(ctx, image, rmtOpts...)
	stopProgress()

	if err != nil {
		log.Printf("Failed to download image: %v\r\n", err)
		return err
	}
	return err
}

func (c *ContainerdContainerRuntime) ContainerInfo(
	ctx context.Context, targetContainerId string) (ContainerInfo, error) {
	ctx = namespaces.WithNamespace(ctx, K8NS)
	cntnr, err := c.client.LoadContainer(ctx, targetContainerId)
	if err != nil {
		log.Printf("Failed to access target container %s : %v\r\n",
			targetContainerId, err)

		return ContainerInfo{}, err
	}
	tsk, err := cntnr.Task(ctx, nil)
	if err != nil {
		log.Printf("Failed to get task of target container %s : %v\r\n",
			targetContainerId, err)

		return ContainerInfo{}, err
	}
	pids, err := tsk.Pids(ctx)
	if err != nil {
		log.Printf("Failed to get pids of target container %s : %v\r\n",
			targetContainerId, err)

		return ContainerInfo{}, err
	}

	info, err := cntnr.Info(ctx, containerd.WithoutRefreshedMetadata)
	if err != nil {
		log.Printf("Failed to load target container info %s : %v\r\n",
			targetContainerId, err)

		return ContainerInfo{}, err
	}

	ret := ContainerInfo{Pid: int64(pids[0].Pid)}
	if info.Spec != nil && info.Spec.Value != nil {
		v, err := typeurl.UnmarshalAny(info.Spec)
		if err != nil {
			log.Printf("Error unmarshalling spec for container %s : %v\r\n",
				targetContainerId, err)
		}
		for _, mnt := range v.(*specs.Spec).Mounts {
			ret.MountDestinations = append(ret.MountDestinations, mnt.Destination)
			fmt.Printf("%+v\r\n", mnt)
		}
	}

	c.pid = ret.Pid
	return ret, nil
}

const (
	// netNSFormat is the format of network namespace of a process.
	netNSFormat = "/proc/%v/ns/net"
	// ipcNSFormat is the format of ipc namespace of a process.
	ipcNSFormat = "/proc/%v/ns/ipc"
	// utsNSFormat is the format of uts namespace of a process.
	userNSFormat = "/proc/%v/ns/user"
	// pidNSFormat is the format of pid namespace of a process.
	pidNSFormat = "/proc/%v/ns/pid"
)

func GetNetworkNamespace(pid int64) string {
	return fmt.Sprintf(netNSFormat, pid)
}
func GetIPCNamespace(pid int64) string {
	return fmt.Sprintf(ipcNSFormat, pid)
}
func GetUserNamespace(pid int64) string {
	return fmt.Sprintf(userNSFormat, pid)
}
func GetPIDNamespace(pid int64) string {
	return fmt.Sprintf(pidNSFormat, pid)
}

func (c *ContainerdContainerRuntime) RunDebugContainer(cfg RunConfig) error {
	ctx := namespaces.WithNamespace(cfg.context, KubectlDebugNS)

	var spcOpts []oci.SpecOpts
	spcOpts = append(spcOpts, oci.WithImageConfig(c.image))
	spcOpts = append(spcOpts, oci.WithPrivileged)
	spcOpts = append(spcOpts, oci.WithLinuxNamespace(specs.LinuxNamespace{
		Type: specs.NetworkNamespace,
		Path: GetNetworkNamespace(c.pid),
	}))
	spcOpts = append(spcOpts, oci.WithLinuxNamespace(specs.LinuxNamespace{
		Type: specs.UserNamespace,
		Path: GetUserNamespace(c.pid),
	}))
	spcOpts = append(spcOpts, oci.WithLinuxNamespace(specs.LinuxNamespace{
		Type: specs.IPCNamespace,
		Path: GetIPCNamespace(c.pid),
	}))
	spcOpts = append(spcOpts, oci.WithLinuxNamespace(specs.LinuxNamespace{
		Type: specs.PIDNamespace,
		Path: GetPIDNamespace(c.pid),
	}))
	cntnr, err := c.client.NewContainer(
		ctx,
		"debug-"+cfg.idOfContainerToDebug,
		containerd.WithImage(c.image),
		containerd.WithNewSnapshot("netshoot-snapshot", c.image), // Had hoped this would fix 2020/04/17 17:04:31 runtime.go:672: Failed to create container for debugging 3d4059893a086fc7c59991fde9835ac7e35b754cd017a300292af9c721a4e6b9 : rootfs absolute path is required but it did not
		containerd.WithNewSpec(spcOpts...),
	)
	if err != nil {
		log.Printf("Failed to create container for debugging %s\r\n",
			cfg.idOfContainerToDebug)
		return err
	}

	defer cntnr.Delete(ctx, containerd.WithSnapshotCleanup)

	tsk, err := cntnr.NewTask(ctx,
		cio.NewCreator(
			cio.WithStreams(cfg.stdin, cfg.stdout, cfg.stderr),
		))
	if err != nil {
		log.Printf("Failed to create task for debugging %s : %v\r\n",
			cfg.idOfContainerToDebug, err)
		return err
	}
	defer tsk.Delete(ctx)

	exitStatusC, err := tsk.Wait(ctx)
	if err != nil {
		log.Printf("Failed to get exit channel for task for debugging %s : %v\r\n",
			cfg.idOfContainerToDebug, err)
		return err
	}

	status := <-exitStatusC
	_, _, err = status.Result()
	if err != nil {
		log.Printf("Failed to get exit status for task for debugging %s : %v\r\n",
			cfg.idOfContainerToDebug, err)
		return err
	}

	return nil
}

// DebugAttacher implements Attacher
// we use this struct in order to inject debug info (image, command) in the debug procedure
type DebugAttacher struct {
	containerRuntime     ContainerRuntime
	image                string
	authStr              string
	registrySkipTLS      bool
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
	log.Printf("Accept new debug request:\n\t target container: %s \n\t image: %s \n\t command: %v \n", m.idOfContainerToDebug, m.image, m.command)

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
	cfg.stdout.Write([]byte(fmt.Sprintf("pulling image %s, skip TLS %v... \n\r", m.image, m.registrySkipTLS)))
	err := m.containerRuntime.PullImage(m.context, m.image,
		m.registrySkipTLS, m.authStr, cfg.stdout)
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
func (m *RuntimeManager) GetAttacher(image, authStr string,
	lxcfsEnabled, registrySkipTLS bool,
	command []string, context context.Context,
	cancel context.CancelFunc) kubeletremote.Attacher {
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
		registrySkipTLS:      registrySkipTLS,
		command:              command,
		context:              context,
		idOfContainerToDebug: m.idOfContainerToDebug,
		verbosity:            m.verbosity,
		timeout:              m.timeout,
		cancel:               cancel,
		stopListenEOF:        make(chan struct{}),
	}
}
