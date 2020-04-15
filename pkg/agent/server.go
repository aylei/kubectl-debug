package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"time"

	remoteapi "k8s.io/apimachinery/pkg/util/remotecommand"
	kubeletremote "k8s.io/kubernetes/pkg/kubelet/server/remotecommand"
)

const (
	dockerContainerPrefix = "docker://"
)

type Server struct {
	config *Config
}

func NewServer(config *Config) (*Server, error) {
	return &Server{config: config}, nil
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

func minInt(lhs, rhs int) int {
	if lhs <= rhs {
		return lhs
	}
	return rhs
}

// ServeDebug serves the debug request.
// first, it will upgrade the connection to SPDY.
// then, server will try to create the debug container, and sent creating progress to user via SPDY.
// after the debug container running, server attach to the debug container and pipe the streams to user.
// once connection closed, server killed the debug container and release related resources
// if any error occurs above, an error status were written to the user's stderr.
func (s *Server) ServeDebug(w http.ResponseWriter, req *http.Request) {

	log.Println("receive debug request")
	gContainerId := req.FormValue("container")
	if len(gContainerId) < 1 {
		log.Println("target container id must be provided")
		http.Error(w, "target container id must be provided", 400)
		return
	}

	// 2020-04-09 d : TODO Need to touch this in order to support containerd
	if !strings.HasPrefix(gContainerId, dockerContainerPrefix) {
		log.Println("only docker container containre runtime is suppored right now")
		http.Error(w, "only docker container runtime is suppored right now", 400)
		return
	}
	containerId := gContainerId[len(dockerContainerPrefix):]

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
	lxcfsEnabled := req.FormValue("lxcfsEnabled")
	if lxcfsEnabled == "" || lxcfsEnabled == "false" {
		LxcfsEnabled = false
	} else if lxcfsEnabled == "true" {
		LxcfsEnabled = true
	}
	sverbosity := req.FormValue("verbosity")
	if sverbosity == "" {
		sverbosity = "0"
	}
	iverbosity, _ := strconv.Atoi(sverbosity)

	context, cancel := context.WithCancel(req.Context())
	defer cancel()

	// 2020-04-09 d : TODO Need to touch this in order to support containerd
	runtime, err := NewRuntimeManager(s.config.DockerEndpoint, containerId, s.config.DockerTimeout,
		minInt(iverbosity, s.config.Verbosity))
	if err != nil {
		http.Error(w, fmt.Sprintf("Failed to construct RuntimeManager.  Error: %v", err), 400)
		return
	}

	// replace Attacher implementation to hook the ServeAttach procedure
	if s.config.Verbosity > 0 {
		log.Println("Invoking kubeletremote.ServeAttach")
	}

	kubeletremote.ServeAttach(
		w,
		req,
		runtime.GetAttacher(image, authStr, LxcfsEnabled, commandSlice, context, cancel),
		"",
		"",
		containerId,
		streamOpts,
		s.config.StreamIdleTimeout,
		s.config.StreamCreationTimeout,
		remoteapi.SupportedStreamingProtocols)
	if s.config.Verbosity > 0 {
		log.Println("kubeletremote.ServeAttach returned")
	}
}

func (s *Server) Healthz(w http.ResponseWriter, req *http.Request) {
	w.Write([]byte("I'm OK!"))
}
