package plugin

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type Config struct {
	AgentPort int      `yaml:"agent_port,omitempty"`
	Image     string   `yaml:"image,omitempty"`
	AppName   string   `yaml:"app_name,omitempty"`
	Command   []string `yaml:"command,omitempty"`
}

func Load(s string) (*Config, error) {
	cfg := &Config{}

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
