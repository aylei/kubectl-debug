package agent

import (
	"flag"
	"github.com/aylei/kubectl-debug/pkg/agent"
	"gopkg.in/yaml.v2"
	"io/ioutil"
	"os"
)

func main() {

	var configFile string
	flag.StringVar(&configFile, "config.file", "debug-agent.yaml", "Config file location.")
	flag.Parse()

	if configFile == "" {

	}
	var config agent.Config
	buf, err := ioutil.ReadFile(configFile)
	if err != nil {

	}

	if err := yaml.UnmarshalStrict(buf, &config); err != nil {

	}

	server, err := agent.NewServer(&config)
	if err != nil {
		os.Exit(1)
	}

	if err := server.Run(); err != nil {
		os.Exit(1)
	}

	server.Shutdown()
}
