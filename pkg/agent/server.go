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

func maxInt(lhs, rhs int) int {
	if lhs >= rhs {
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
	containerUri := req.FormValue("container")

	sverbosity := req.FormValue("verbosity")
	if sverbosity == "" {
		sverbosity = "0"
	}
	iverbosity, _ := strconv.Atoi(sverbosity)

	imageFromPlugin := req.FormValue("image")
	imageFromEnv := os.Getenv("KCTLDBG_RESTRICT_IMAGE_TO")
	var image string
	if len(imageFromEnv) > 0 {
		image = imageFromEnv
		if imageFromPlugin != imageFromEnv && iverbosity > 0 {
			log.Printf("Using image %v, specified by env var KCTLDBG_RESTRICT_IMAGE_TO on agent, instead of image %v specified by client.\r\n",
				imageFromEnv, imageFromPlugin)
		}
	} else {
		image = imageFromPlugin
	}
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
	var registrySkipTLS bool
	registrySkipTLSParam := req.FormValue("registrySkipTLS")
	if registrySkipTLSParam == "" || registrySkipTLSParam == "false" {
		registrySkipTLS = false
	} else if registrySkipTLSParam == "true" {
		registrySkipTLS = true
	}

	context, cancel := context.WithCancel(req.Context())
	defer cancel()

	runtime, err := NewRuntimeManager(*s.config, containerUri,
		maxInt(iverbosity, s.config.Verbosity),
		req.FormValue("hostname"),
		req.FormValue("username"))
	if err != nil {
		msg := fmt.Sprintf("Failed to construct RuntimeManager.  Error: %s", err.Error())
		log.Println(msg)
		// 2020-04-15 d :
		// The client will be in SPDY roundtripper when we return this.  This passes the response to
		// statusCodecs.UniversalDecoder().Decode.  Decode will see any ":" as indication that the
		// response bytes are an object to be deserialized and consequently our message to the client
		// will be lost.
		http.Error(w, strings.ReplaceAll(msg, ":", "-"), 400)
		return
	}

	// replace Attacher implementation to hook the ServeAttach procedure
	if s.config.Verbosity > 0 {
		log.Println("Invoking kubeletremote.ServeAttach")
	}

	kubeletremote.ServeAttach(
		w,
		req,
		runtime.GetAttacher(image, authStr, LxcfsEnabled, registrySkipTLS,
			commandSlice, context, cancel),
		"",
		"",
		"",
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
