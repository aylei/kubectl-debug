package agent

import (
	"context"
	"fmt"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/strslice"
	dockerclient "github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"io"
	"io/ioutil"
	kubetype "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/kubernetes/pkg/kubelet/dockershim/libdocker"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
	"log"
	"time"
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
	command []string
}

func (a *DebugAttacher) AttachContainer(name string, uid kubetype.UID, container string, in io.Reader, out, err io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
	return a.runtime.DebugContainer(container, a.image, a.command, in, out, err, tty, resize)
}

// GetAttacher returns an implementation of Attacher
func (m *RuntimeManager) GetAttacher(image string, command []string) kubeletremote.Attacher {
	return &DebugAttacher{runtime: m, image: image}
}

// DebugContainer executes the main debug flow
func (m *RuntimeManager) DebugContainer(container, image string, command []string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {

	log.Printf("Accept new debug reqeust:\n\t target container: %s \n\t image: %s \n\t command: %v \n", container, image, command)

	// step 1: pull image
	stdout.Write([]byte(fmt.Sprintf("pulling image %s ...\n\r", image)))
	err := m.PullImage(image)
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
	if err := m.AttachToContainer(id, stdin, stdout, stderr, tty, resize); err != nil {
		return err
	}
	return nil
}

// Run a new container, this container will join the network,
// mount, and pid namespace of the given container
func (m *RuntimeManager) RunDebugContainer(targetId string, image string, command []string) (string, error) {

	createdBody, err := m.CreateContainer(targetId, image, command)
	if err != nil {
		return "", err
	}
	if err := m.StartContainer(createdBody.ID); err != nil {
		return "", err
	}
	return createdBody.ID, nil
}

func (m *RuntimeManager) StartContainer(id string) error {
	ctx, cancel := m.getTimeoutContext()
	defer cancel()
	err := m.client.ContainerStart(ctx, id, types.ContainerStartOptions{})
	if err != nil {
		return err
	}
	return nil
}

func (m *RuntimeManager) CreateContainer(targetId string, image string, command []string) (*container.ContainerCreateCreatedBody, error) {

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
	}
	ctx, cancel := m.getTimeoutContext()
	defer cancel()
	body, err := m.client.ContainerCreate(ctx, config, hostConfig, nil, "")
	if err != nil {
		return nil, err
	}
	return &body, nil
}

func (m *RuntimeManager) PullImage(image string) error {
	ctx, cancel := m.getTimeoutContext()
	defer cancel()
	resp, err := m.client.ImagePull(ctx, image, types.ImagePullOptions{})
	if err != nil {
		return err
	}
	defer resp.Close()
	return nil

}

func (m *RuntimeManager) CleanContainer(id string) {
	ctx, cancel := m.getTimeoutContext()
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
		log.Printf("Debug session end, debug container %s removed")
	}
}

func (m *RuntimeManager) RmContainer(id string, force bool) error {
	ctx, cancel := m.getTimeoutContext()
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
func (m *RuntimeManager) AttachToContainer(container string, stdin io.Reader, stdout, stderr io.WriteCloser, tty bool, resize <-chan remotecommand.TerminalSize) error {
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
	ctx, cancel := m.getTimeoutContext()
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
func (m *RuntimeManager) holdHijackedConnection(tty bool, inputStream io.Reader, outputStream, errorStream io.Writer, resp types.HijackedResponse) error {
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

func (m *RuntimeManager) redirectResponseToOutputStream(tty bool, outputStream, errorStream io.Writer, resp io.Reader) error {
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

func (m *RuntimeManager) resizeContainerTTY(id string, height, width uint) error {
	ctx, cancel := m.getTimeoutContext()
	defer cancel()
	return m.client.ContainerResize(ctx, id, types.ResizeOptions{
		Height: height,
		Width:  width,
	})
}

func (m *RuntimeManager) getTimeoutContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), m.timeout)
}

func (m *RuntimeManager) containerMode(id string) string {
	return fmt.Sprintf("container:%s", id)
}
