package agent

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"time"
)

var (
	DefaultConfig = Config{
		DockerEndpoint:        "unix:///var/run/docker.sock",
		DockerTimeout:         30 * time.Second,
		StreamIdleTimeout:     10 * time.Minute,
		StreamCreationTimeout: 15 * time.Second,

		ListenAddress: "0.0.0.0:10027",
	}
)

type Config struct {
	DockerEndpoint        string        `yaml:"docker_endpoint,omitempty"`
	DockerTimeout         time.Duration `yaml:"docker_timeout,omitempty"`
	StreamIdleTimeout     time.Duration `yaml:"stream_idle_timeout,omitempty"`
	StreamCreationTimeout time.Duration `yaml:"stream_creation_timeout,omitempty"`

	ListenAddress string `yaml:"listen_address,omitempty"`
}

func Load(s string) (*Config, error) {
	cfg := &Config{}
	// If the entire config body is empty the UnmarshalYAML method is
	// never called. We thus have to set the DefaultConfig at the entry
	// point as well.
	*cfg = DefaultConfig

	err := yaml.UnmarshalStrict([]byte(s), cfg)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadFile(filename string) (*Config, error) {
	c, err := ioutil.ReadFile(filename)
	if err != nil {
		return nil, err
	}
	return Load(string(c))
}
