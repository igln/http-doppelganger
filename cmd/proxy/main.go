package main

import (
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/http-doppelganger/internal/config"
	"github.com/http-doppelganger/internal/server"
)

var (
	version   = "dev"
	buildTime = "unknown"
)

func main() {
	configPath := flag.String("config", "config.yaml", "Path to configuration file")
	showVersion := flag.Bool("version", false, "Show version information")
	flag.Parse()

	if *showVersion {
		fmt.Printf("gitlab-proxy version %s (built %s)\n", version, buildTime)
		os.Exit(0)
	}

	log.SetFlags(log.LstdFlags | log.Lshortfile)
	log.Printf("Starting GitLab Proxy...")

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	log.Printf("Configuration loaded successfully")
	log.Printf("  GitLab Host: %s", cfg.GitLab.Host)
	log.Printf("  Log Level: %s", cfg.Logging.Level)

	srv := server.New(cfg)
	if err := srv.Run(); err != nil {
		log.Fatalf("Server error: %v", err)
	}
}
