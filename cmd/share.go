package cmd

import (
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"liveshare/client"
)

var shareCmd = &cobra.Command{
	Use:   "share [file]",
	Short: "Share a file via an existing token",
	Args:  cobra.ExactArgs(1),
	RunE:  runShare,
}

func init() {
	shareCmd.Flags().String("server", "", "Server URL with token (e.g., host/ws/TOKEN)")
	shareCmd.Flags().String("name", "", "Display name for the file")
	shareCmd.Flags().BoolP("once", "1", false, "One-time share: disconnect after first download")
	shareCmd.MarkFlagRequired("server")
}

func parseServerURL(raw string) (string, error) {
	if !strings.Contains(raw, "://") {
		if strings.HasPrefix(raw, "localhost") || strings.HasPrefix(raw, "127.0.0.1") || strings.HasPrefix(raw, "[::1]") {
			raw = "ws://" + raw
		} else {
			raw = "wss://" + raw
		}
	} else {
		raw = strings.Replace(raw, "https://", "wss://", 1)
		raw = strings.Replace(raw, "http://", "ws://", 1)
	}

	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("parse server URL: %w", err)
	}

	return u.String(), nil
}

func runShare(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	serverRaw, _ := cmd.Flags().GetString("server")
	displayName, _ := cmd.Flags().GetString("name")
	oneTime, _ := cmd.Flags().GetBool("once")

	if displayName == "" {
		displayName = filepath.Base(filePath)
	}

	wsURL, err := parseServerURL(serverRaw)
	if err != nil {
		return err
	}

	slog.Info("connecting", "url", wsURL, "file", filePath, "name", displayName)

	c, err := client.New(wsURL, filePath, displayName, oneTime)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	// Derive HTTP download URL from the WS URL
	httpURL := wsURL
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
	if u, err := url.Parse(httpURL); err == nil {
		u.Path = fmt.Sprintf("/d/%s/%s", c.ShareID, displayName)
		fmt.Printf("Download URL: %s\n", u.String())
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Run()
	}()

	select {
	case err := <-errCh:
		if err != nil {
			return err
		}
		return nil
	case sig := <-sigCh:
		slog.Info("shutting down", "signal", sig)
		c.Conn.Close()
		return nil
	}
}
