package agent

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	remoteapi "k8s.io/apimachinery/pkg/util/remotecommand"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
)

const (
	dockerContainerPrefix = "docker://"
)

type Server struct {
	config     *Config
	runtimeApi *RuntimeManager
}

func NewServer(config *Config) (*Server, error) {
	runtime, err := NewRuntimeManager(config.DockerEndpoint, config.DockerTimeout)
	if err != nil {
		return nil, err
	}
	return &Server{config: config, runtimeApi: runtime}, nil
}

func (s *Server) Run() error {

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, os.Interrupt)

	mux := http.NewServeMux()
	mux.HandleFunc("/api/v1/debug", s.ServeDebug)
	mux.HandleFunc("/healthz", s.Healthz)
	server := &http.Server{Addr: s.config.ListenAddress, Handler: mux}

	go func() {
		log.Printf("Listening on %s \n", s.config.ListenAddress)

		if err := server.ListenAndServe(); err != nil {
			log.Fatal(err)
		}
	}()
	<-stop

	log.Println("shutting done server...")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	server.Shutdown(ctx)

	return nil
}

// ServeDebug serves the debug request.
// first, it will upgrade the connection to SPDY.
// then, server will try to create the debug container, and sent creating progress to user via SPDY.
// after the debug container running, server attach to the debug container and pipe the streams to user.
// once connection closed, server killed the debug container and release related resources
// if any error occurs above, an error status were written to the user's stderr.
func (s *Server) ServeDebug(w http.ResponseWriter, req *http.Request) {

	log.Println("receive debug request")
	containerId := req.FormValue("container")
	if len(containerId) < 1 {
		http.Error(w, "target container id must be provided", 400)
		return
	}
	if !strings.HasPrefix(containerId, dockerContainerPrefix) {
		http.Error(w, "only docker container is suppored right now", 400)
	}
	dockerContainerId := containerId[len(dockerContainerPrefix):]

	image := req.FormValue("image")
	if len(image) < 1 {
		http.Error(w, "image must be provided", 400)
		return
	}
	command := req.FormValue("command")
	var commandSlice []string
	err := json.Unmarshal([]byte(command), &commandSlice)
	if err != nil || len(commandSlice) < 1 {
		http.Error(w, "cannot parse command", 400)
		return
	}
	authStr := req.FormValue("authStr")
	streamOpts := &kubeletremote.Options{
		Stdin:  true,
		Stdout: true,
		Stderr: false,
		TTY:    true,
	}

	context, cancel := context.WithCancel(req.Context())
	defer cancel()

	// replace Attacher implementation to hook the ServeAttach procedure
	kubeletremote.ServeAttach(
		w,
		req,
		s.runtimeApi.GetAttacher(image, authStr, commandSlice, context, cancel),
		"",
		"",
		dockerContainerId,
		streamOpts,
		s.config.StreamIdleTimeout,
		s.config.StreamCreationTimeout,
		remoteapi.SupportedStreamingProtocols)
}

func (s *Server) Healthz(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("I'm OK!"))
}
