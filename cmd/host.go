package cmd

import (
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"

	"liveshare/config"
	"liveshare/server"
	"liveshare/tunnel"
)

var hostCmd = &cobra.Command{
	Use:   "host",
	Short: "Start the liveshare server",
	RunE:  runHost,
}

func init() {
	hostCmd.Flags().String("hostname", "", "Public hostname")
	hostCmd.Flags().String("listen", "", "Listen address (e.g., localhost, 0.0.0.0, 192.168.1.100)")
	hostCmd.Flags().Int("port", 0, "Listen port")
	hostCmd.Flags().String("cf-token", "", "Cloudflare tunnel token")
	hostCmd.Flags().String("token-file", "", "Path to token file")
	hostCmd.Flags().Bool("tunnel", false, "Start a cloudflared quick tunnel")
	hostCmd.Flags().String("config", config.DefaultConfigFile, "Config file path")
}

func runHost(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	if v, _ := cmd.Flags().GetString("hostname"); v != "" {
		cfg.Hostname = v
	}
	if cmd.Flags().Changed("listen") {
		cfg.Listen, _ = cmd.Flags().GetString("listen")
	}
	if v, _ := cmd.Flags().GetInt("port"); v != 0 {
		cfg.Port = v
	}
	if v, _ := cmd.Flags().GetString("cf-token"); v != "" {
		cfg.CfToken = v
	}
	if v, _ := cmd.Flags().GetString("token-file"); v != "" {
		cfg.TokenFile = v
	}
	cfg.ApplyDefaults()

	if cmd.Flags().Changed("hostname") || cmd.Flags().Changed("listen") ||
		cmd.Flags().Changed("port") || cmd.Flags().Changed("cf-token") ||
		cmd.Flags().Changed("token-file") {
		if err := cfg.Save(cfgPath); err != nil {
			slog.Warn("could not save config", "err", err)
		}
	}

	srv := server.New()
	if err := srv.LoadTokens(cfg.TokenFile); err != nil {
		return fmt.Errorf("load tokens: %w", err)
	}
	slog.Info("loaded tokens", "count", srv.TokenCount(), "file", cfg.TokenFile)

	useTunnel, _ := cmd.Flags().GetBool("tunnel")
	if useTunnel || cfg.CfToken != "" {
		t, err := tunnel.Start(cfg.Port, cfg.CfToken)
		if err != nil {
			return fmt.Errorf("start tunnel: %w", err)
		}
		defer t.Stop()
		if t.URL != "" {
			slog.Info("tunnel started", "url", "https://"+t.URL)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe(srv.Addr(cfg.Listen, cfg.Port))
	}()

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		slog.Info("shutting down", "signal", sig)
		return nil
	}
}
