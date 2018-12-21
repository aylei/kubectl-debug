package main

import (
	"flag"
	"github.com/aylei/kubectl-debug/pkg/agent"
	"log"
	"os"
)

func main() {

	var configFile string
	flag.StringVar(&configFile, "config.file", "", "Config file location.")
	flag.Parse()

	config, err := agent.LoadFile(configFile)
	if err != nil {
		log.Fatalf("error reading config %v", err)
		os.Exit(1)
	}

	server, err := agent.NewServer(config)
	if err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	if err := server.Run(); err != nil {
		log.Fatal(err)
		os.Exit(1)
	}

	log.Println("sever stopped, see you next time!")
}
