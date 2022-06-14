package kubectldebug

import (
	"gopkg.in/yaml.v2"
	"io/ioutil"
)

type Config struct {
	AgentPort                int      `yaml:"agentPort,omitempty"`
	Image                    string   `yaml:"image,omitempty"`
	RegistrySecretName       string   `yaml:"registrySecretName,omitempty"`
	RegistrySecretNamespace  string   `yaml:"registrySecretNamespace,omitempty"`
	RegistrySkipTLSVerify    bool     `yaml:"registrySkipTLSVerify,omitempty"`
	ForkPodRetainLabels      []string `yaml:"forkPodRetainLabels,omitempty"`
	DebugAgentDaemonSet      string   `yaml:"debugAgentDaemonset,omitempty"`
	DebugAgentNamespace      string   `yaml:"debugAgentNamespace,omitempty"`
	Command                  []string `yaml:"command,omitempty"`
	PortForward              bool     `yaml:"portForward,omitempty"`
	CreateDebugAgentPod      bool     `yaml:"createDebugAgentPod,omitempty"`
	AgentPodNamePrefix       string   `yaml:"agentPodNamePrefix,omitempty"`
	AgentPodNamespace        string   `yaml:"agentPodNamespace,omitempty"`
	AgentImage               string   `yaml:"agentImage,omitempty"`
	AgentImagePullPolicy     string   `yaml:"agentImagePullPolicy,omitempty"`
	AgentImagePullSecretName string   `yaml:"agentImagePullSecretName,omitempty"`
	AgentPodCpuRequests      string   `yaml:"agentCpuRequests,omitempty"`
	AgentPodMemoryRequests   string   `yaml:"agentMemoryRequests,omitempty"`
	AgentPodCpuLimits        string   `yaml:"agentCpuLimits,omitempty"`
	AgentPodMemoryLimits     string   `yaml:"agentMemoryLimits,omitempty"`
	IsLxcfsEnabled           bool     `yaml:"isLxcfsEnabled,omitempty"`
	Verbosity                int      `yaml:"verbosity,omitempty"`
}

func Load(s string) (*Config, error) {
	cfg := &Config{}
	err := yaml.Unmarshal([]byte(s), cfg)
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
