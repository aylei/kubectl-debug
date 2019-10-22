package plugin

import (
	"io/ioutil"

	"gopkg.in/yaml.v2"
)

type RegistryAuthConfig struct {
	Username string `yaml:"username,omitempty"`
	Password string `yaml:"password,omitempty"`
}
type Config struct {
	AgentPort               int      `yaml:"agentPort,omitempty"`
	Image                   string   `yaml:"image,omitempty"`
	RegistrySecretName      string   `yaml:"registrySecretName,omitempty"`
	RegistrySecretNamespace string   `yaml:"registrySecretNamespace,omitempty"`
	ForkPodRetainLabels     []string `yaml:"forkPodRetainLabels,omitempty"`
	DebugAgentDaemonSet     string   `yaml:"debugAgentDaemonset,omitempty"`
	DebugAgentNamespace     string   `yaml:"debugAgentNamespace,omitempty"`
	Command                 []string `yaml:"command,omitempty"`
	PortForward             bool     `yaml:"portForward,omitempty"`
	Agentless               bool     `yaml:"agentless,omitempty"`
	AgentPodNamePrefix      string   `yaml:"agentPodNamePrefix,omitempty"`
	AgentPodNamespace       string   `yaml:"agentPodNamespace,omitempty"`
	AgentImage              string   `yaml:"agentImage,omitempty"`
	AgentPodCpuRequests     string   `yaml:"agentCpuRequests,omitempty"`
	AgentPodMemoryRequests  string   `yaml:"agentMemoryRequests,omitempty"`
	AgentPodCpuLimits       string   `yaml:"agentCpuLimits,omitempty"`
	AgentPodMemoryLimits    string   `yaml:"agentMemoryLimits,omitempty"`

	// deprecated
	AgentPortOld int `yaml:"agent_port,omitempty"`
}

func Load(s string) (*Config, error) {
	cfg := &Config{}
	err := yaml.Unmarshal([]byte(s), cfg)
	if err != nil {
		return nil, err
	}
	// be compatible with old configuration key
	if cfg.AgentPort == 0 {
		cfg.AgentPort = cfg.AgentPortOld
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
