package agent

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"time"

	term "github.com/aylei/kubectl-debug/pkg/util"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	kubetype "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubernetes/pkg/kubelet/dockershim/libdocker"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
)

// RuntimeManager is responsible for docker operation
type RuntimeManager struct {
	client  *dockerclient.Client
	timeout time.Duration
}

func NewRuntimeManager(host string, timeout time.Duration) (*RuntimeManager, error) {
	client, err := dockerclient.NewClient(host, "", nil, nil)
	if err != nil {
		return nil, err
	}
	return &RuntimeManager{
		client:  client,
		timeout: timeout,
	}, nil
}

// DebugAttacher implements Attacher
// we use this struct in order to inject debug info (image, command) in the debug procedure
type DebugAttacher struct {
	runtime *RuntimeManager
	image   string
	authStr string
	command []string
	client  *dockerclient.Client

	// control the preparing of debug container
	stopListenEOF chan struct{}
	context       context.Context
	cancel        context.CancelFunc
}

func (a *DebugAttacher) AttachContainer(name string, uid kubetype.UID, container string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	return a.DebugContainer(container, a.image, a.authStr, a.command, in, out, err, tty, resize)
}

// GetAttacher returns an implementation of Attacher
func (m *RuntimeManager) GetAttacher(image string, authStr string, command []string, context context.Context, cancel context.CancelFunc) kubeletremote.Attacher {
	return &DebugAttacher{
		runtime:       m,
		image:         image,
		authStr:       authStr,
		command:       command,
		context:       context,
		client:        m.client,
		cancel:        cancel,
		stopListenEOF: make(chan struct{}),
	}
}

// DebugContainer executes the main debug flow
func (m *DebugAttacher) DebugContainer(container, image string, authStr string, command []string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {

	log.Printf("Accept new debug reqeust:\n\t target container: %s \n\t image: %s \n\t command: %v \n", container, image, command)

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

	// step 1: pull image
	stdout.Write([]byte(fmt.Sprintf("pulling image %s... \n\r", image)))
	err := m.PullImage(image, authStr, stdout)
	if err != nil {
		return err
	}

	// step 2: run debug container (join the namespaces of target container)
	stdout.Write([]byte("starting debug container...\n\r"))
	id, err := m.RunDebugContainer(container, image, command)
	if err != nil {
		return err
	}
	defer m.CleanContainer(id)

	// step 3: attach tty
	stdout.Write([]byte("container created, open tty...\n\r"))

	// from now on, should pipe stdin to the container and no long read stdin
	// close(m.stopListenEOF)

	if err := m.AttachToContainer(id, stdin, stdout, stderr, tty, resize); err != nil {
		return err
	}
	return nil
}

// Run a new container, this container will join the network,
// mount, and pid namespace of the given container
func (m *DebugAttacher) RunDebugContainer(targetId string, image string, command []string) (string, error) {

	createdBody, err := m.CreateContainer(targetId, image, command)
	if err != nil {
		return "", err
	}
	if err := m.StartContainer(createdBody.ID); err != nil {
		return "", err
	}
	return createdBody.ID, nil
}

func (m *DebugAttacher) StartContainer(id string) error {
	ctx, cancel := m.getContextWithTimeout()
	defer cancel()
	err := m.client.ContainerStart(ctx, id, types.ContainerStartOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (m *DebugAttacher) CreateContainer(targetId string, image string, command []string) (*container.ContainerCreateCreatedBody, error) {

	config := &container.Config{
		Entrypoint: strslice.StrSlice(command),
		Image:      image,
		Tty:        true,
		OpenStdin:  true,
		StdinOnce:  true,
	}
	hostConfig := &container.HostConfig{
		NetworkMode: container.NetworkMode(m.containerMode(targetId)),
		UsernsMode:  container.UsernsMode(m.containerMode(targetId)),
		IpcMode:     container.IpcMode(m.containerMode(targetId)),
		PidMode:     container.PidMode(m.containerMode(targetId)),
		CapAdd:      strslice.StrSlice([]string{"SYS_PTRACE", "SYS_ADMIN"}),
	}
	ctx, cancel := m.getContextWithTimeout()
	defer cancel()
	body, err := m.client.ContainerCreate(ctx, config, hostConfig, nil, "")
	if err != nil {
		return nil, err
	}
	return &body, nil
}

func (m *DebugAttacher) PullImage(image string, authStr string, stdout io.WriteCloser) error {
	authBytes := base64.URLEncoding.EncodeToString([]byte(authStr))
	out, err := m.client.ImagePull(m.context, image, types.ImagePullOptions{RegistryAuth: string(authBytes)})
	if err != nil {
		return err
	}
	defer out.Close()
	// write pull progress to user
	term.DisplayJSONMessagesStream(out, stdout, 1, true, nil)
	return nil
}

func (m *DebugAttacher) CleanContainer(id string) {
	// cleanup procedure should use background context
	ctx, cancel := context.WithTimeout(context.Background(), m.runtime.timeout)
	defer cancel()
	// wait the container gracefully exit
	statusCh, errCh := m.client.ContainerWait(ctx, id, container.WaitConditionNotRunning)
	var rmErr error
	select {
	case err := <-errCh:
		if err != nil {
			log.Println("error waiting container exit, kill with --force")
			// timeout or error occurs, try force remove anywawy
			rmErr = m.RmContainer(id, true)
		}
	case <-statusCh:
		rmErr = m.RmContainer(id, false)
	}
	if rmErr != nil {
		log.Printf("error remove container: %s \n", id)
	} else {
		log.Printf("Debug session end, debug container %s removed", id)
	}
}

func (m *DebugAttacher) RmContainer(id string, force bool) error {
	// cleanup procedure should use background context
	ctx, cancel := context.WithTimeout(context.Background(), m.runtime.timeout)
	defer cancel()
	err := m.client.ContainerRemove(ctx, id,
		types.ContainerRemoveOptions{
			Force: true,
		})
	if err != nil {
		return err
	}
	return nil
}

// AttachToContainer do `docker attach`
func (m *DebugAttacher) AttachToContainer(container string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	HandleResizing(resize, func(size remotecommand.TerminalSize) {
		m.resizeContainerTTY(container, uint(size.Height), uint(size.Width))
	})

	opts := types.ContainerAttachOptions{
		Stream: true,
		Stdin:  stdin != nil,
		Stdout: stdout != nil,
		Stderr: stderr != nil,
	}
	sopts := libdocker.StreamOptions{
		InputStream:  stdin,
		OutputStream: stdout,
		ErrorStream:  stderr,
		RawTerminal:  tty,
	}
	ctx, cancel := m.getContextWithTimeout()
	defer cancel()
	resp, err := m.client.ContainerAttach(ctx, container, opts)
	if err != nil {
		return err
	}
	defer resp.Close()

	return m.holdHijackedConnection(sopts.RawTerminal, sopts.InputStream, sopts.OutputStream, sopts.ErrorStream, resp)
}

// holdHijackedConnection hold the HijackedResponse, redirect the inputStream to the connection, and redirect the response
// stream to stdout and stderr. NOTE: If needed, we could also add context in this function.
func (m *DebugAttacher) holdHijackedConnection(tty bool, inputStream io.Reader, outputStream, errorStream io.Writer, resp types.HijackedResponse) error {
	receiveStdout := make(chan error)
	if outputStream != nil || errorStream != nil {
		go func() {
			receiveStdout <- m.redirectResponseToOutputStream(tty, outputStream, errorStream, resp.Reader)
		}()
	}

	stdinDone := make(chan struct{})
	go func() {
		if inputStream != nil {
			io.Copy(resp.Conn, inputStream)
		}
		resp.CloseWrite()
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		return err
	case <-stdinDone:
		if outputStream != nil || errorStream != nil {
			return <-receiveStdout
		}
	}
	return nil
}

func (m *DebugAttacher) redirectResponseToOutputStream(tty bool, outputStream, errorStream io.Writer, resp io.Reader) error {
	if outputStream == nil {
		outputStream = ioutil.Discard
	}
	if errorStream == nil {
		errorStream = ioutil.Discard
	}
	var err error
	if tty {
		_, err = io.Copy(outputStream, resp)
	} else {
		_, err = stdcopy.StdCopy(outputStream, errorStream, resp)
	}
	return err
}

func (m *DebugAttacher) resizeContainerTTY(id string, height, width uint) error {
	ctx, cancel := m.getContextWithTimeout()
	defer cancel()
	return m.client.ContainerResize(ctx, id, types.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

func (m *DebugAttacher) getContextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(m.context, m.runtime.timeout)
}

func (m *DebugAttacher) containerMode(id string) string {
	return fmt.Sprintf("container:%s", id)
}
