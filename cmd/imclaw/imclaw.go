package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	flag "github.com/spf13/pflag"
	"github.com/smallnest/imclaw/internal/agent"
	"github.com/smallnest/imclaw/internal/config"
	"github.com/smallnest/imclaw/internal/gateway"
	"github.com/smallnest/imclaw/internal/session"
)

var (
	configPath  = flag.String("config", "", "Path to config file")
	showVersion = flag.Bool("version", false, "Show version information")

	// 版本信息，通过构建时注入
	Version   = "dev"
	BuildTime = "unknown"
)

func main() {
	flag.Parse()

	// 显示版本信息
	if *showVersion {
		fmt.Printf("IMClaw %s\n", Version)
		fmt.Printf("Build Time: %s\n", BuildTime)
		os.Exit(0)
	}

	// Get config path
	cfgPath := *configPath
	if cfgPath == "" {
		var err error
		cfgPath, err = config.GetDefaultConfigPath()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Failed to get default config path: %v\n", err)
			os.Exit(1)
		}
	}

	fmt.Printf("Loading config from: %s\n", cfgPath)

	// Load config
	cfgMgr, err := config.NewManager(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to load config: %v\n", err)
		os.Exit(1)
	}

	cfg := cfgMgr.Get()

	// Print banner
	printBanner()

	// Create context
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Setup signal handling
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-sigCh
		fmt.Println("\nShutting down...")
		cancel()
	}()

	// Create session manager
	sessionMgr := session.NewManager()

	// Create agent manager
	agentMgr := agent.NewManager()
	defer agentMgr.Close()

	// Create and start gateway server
	srv := gateway.NewServer(cfg, sessionMgr, agentMgr)
	if err := srv.Start(ctx); err != nil {
		fmt.Fprintf(os.Stderr, "Failed to start gateway: %v\n", err)
		os.Exit(1)
	}

	// Watch config for changes
	cfgMgr.SetOnChange(func(newCfg *config.Config) {
		log.Println("Config updated (hot reload not fully implemented)")
	})
	cfgMgr.Watch()

	fmt.Printf("\nGateway started on %s:%d\n", cfg.Host, cfg.Port)
	fmt.Printf("  HTTP:      http://%s:%d\n", cfg.Host, cfg.Port)
	fmt.Printf("  WebSocket: ws://%s:%d/ws\n", cfg.Host, cfg.Port)
	fmt.Printf("\nUse 'imclaw-cli' to interact with the server.\n\n")

	// Wait for context cancellation
	<-ctx.Done()

	// Cleanup
	_ = srv.Stop()
	cfgMgr.Stop()

	fmt.Println("Goodbye!")
}

func printBanner() {
	fmt.Printf(`
╔═══════════════════════════════════════╗
║          IMClaw %-10s            ║
║   AI Agent Gateway with ACP Protocol  ║
╚═══════════════════════════════════════╝

`, Version)
}
