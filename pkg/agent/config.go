package agent

import (
	"fmt"
	"io/ioutil"
	"time"

	"gopkg.in/yaml.v2"
)

var (
	DefaultConfig = Config{
		DockerEndpoint:        "unix:///var/run/docker.sock",
		ContainerdEndpoint:    "/run/containerd/containerd.sock",
		RuntimeTimeout:        30 * time.Second,
		StreamIdleTimeout:     10 * time.Minute,
		StreamCreationTimeout: 15 * time.Second,

		ListenAddress: "0.0.0.0:10027",

		AuditFifo: "/var/data/kubectl-debug-audit-fifo/KCTLDBG-CONTAINER-ID",
		AuditShim: []string{"/usr/bin/strace", "-o", "KCTLDBG-FIFO", "-f", "-e", "trace=/exec"},
	}
)

type Config struct {
	DockerEndpoint        string        `yaml:"docker_endpoint,omitempty"`
	ContainerdEndpoint    string        `yaml:"containerd_endpoint,omitempty"`
	RuntimeTimeout        time.Duration `yaml:"runtime_timeout,omitempty"`
	StreamIdleTimeout     time.Duration `yaml:"stream_idle_timeout,omitempty"`
	StreamCreationTimeout time.Duration `yaml:"stream_creation_timeout,omitempty"`

	ListenAddress string `yaml:"listen_address,omitempty"`
	Verbosity     int    `yaml:"verbosity,omitempty"`

	Audit     bool     `yaml:"audit,omitempty"`
	AuditFifo string   `yaml:"audit_fifo,omitempty"`
	AuditShim []string `yaml:"audit_shim,omitempty"`
}

func Load(s string) (*Config, error) {
	cfg := &Config{}
	// If the entire config body is empty the UnmarshalYAML method is
	// never called. We thus have to set the DefaultConfig at the entry
	// point as well.
	*cfg = DefaultConfig

	err := yaml.UnmarshalStrict([]byte(s), cfg)
	fmt.Printf("Config after reading from file %v\r\n", cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadFile(filename string) (*Config, error) {
	if len(filename) < 1 {
		fmt.Println("No config file provided.  Using all default values.")
		return &DefaultConfig, nil
	}
	fmt.Printf("Reading config file %v.\r\n", filename)
	c, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Load(string(c))
}
