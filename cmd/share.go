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
	"time"

	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"

	"liveshare/client"
)

var shareCmd = &cobra.Command{
	Use:   "share [files...]",
	Short: "Share a file via an existing token",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runShare,
}

func init() {
	shareCmd.Flags().String("server", "", "Server URL with token (e.g., host/ws/TOKEN)")
	shareCmd.Flags().String("name", "", "Display name for the file")
	shareCmd.Flags().BoolP("once", "1", false, "One-time share: disconnect after first download")
	shareCmd.Flags().Bool("no-cache", false, "Disable server-side caching")
	shareCmd.Flags().Bool("tar", false, "Archive as tar")
	shareCmd.Flags().Bool("tgz", false, "Archive as gzipped tar")
	shareCmd.Flags().Duration("timeout", 0, "Auto-disconnect after duration (e.g., 30m, 1h)")
	shareCmd.Flags().Bool("qr", false, "Display QR code for the download URL")
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
	serverRaw, _ := cmd.Flags().GetString("server")
	displayName, _ := cmd.Flags().GetString("name")
	oneTime, _ := cmd.Flags().GetBool("once")
	noCache, _ := cmd.Flags().GetBool("no-cache")
	useTar, _ := cmd.Flags().GetBool("tar")
	useTgz, _ := cmd.Flags().GetBool("tgz")
	timeout, _ := cmd.Flags().GetDuration("timeout")
	showQR, _ := cmd.Flags().GetBool("qr")

	wsURL, err := parseServerURL(serverRaw)
	if err != nil {
		return err
	}

	archiveMode := ""
	if useTgz {
		archiveMode = "tgz"
	} else if useTar {
		archiveMode = "tar"
	} else if len(args) > 1 {
		archiveMode = "zip"
	} else {
		info, err := os.Stat(args[0])
		if err != nil {
			return fmt.Errorf("stat %s: %w", args[0], err)
		}
		if info.IsDir() {
			archiveMode = "zip"
		}
	}

	var c *client.Client
	if archiveMode != "" {
		if displayName == "" {
			if len(args) == 1 {
				displayName = filepath.Base(args[0])
			} else {
				displayName = "files"
			}
			switch archiveMode {
			case "zip":
				displayName += ".zip"
			case "tar":
				displayName += ".tar"
			case "tgz":
				displayName += ".tar.gz"
			}
		}
		slog.Info("connecting (archive mode)", "mode", archiveMode, "url", wsURL, "paths", args, "name", displayName)
		c, err = client.NewArchive(wsURL, args, displayName, archiveMode)
	} else {
		filePath := args[0]
		if displayName == "" {
			displayName = filepath.Base(filePath)
		}
		slog.Info("connecting", "url", wsURL, "file", filePath, "name", displayName)
		c, err = client.New(wsURL, filePath, displayName, oneTime, noCache)
	}
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}

	httpURL := wsURL
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
	if u, err := url.Parse(httpURL); err == nil {
		u.Path = fmt.Sprintf("/d/%s/%s", c.ShareID, displayName)
		downloadURL := u.String()
		fmt.Printf("Download URL: %s\n", downloadURL)

		if showQR {
			qrterminal.GenerateHalfBlock(downloadURL, qrterminal.L, os.Stdout)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	errCh := make(chan error, 1)
	go func() {
		errCh <- c.Run()
	}()

	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timeoutCh = time.After(timeout)
	}

	select {
	case err := <-errCh:
		return err
	case sig := <-sigCh:
		slog.Info("shutting down", "signal", sig)
		c.Conn.Close()
		return nil
	case <-timeoutCh:
		slog.Info("timeout reached, disconnecting", "timeout", timeout)
		c.Conn.Close()
		return nil
	}
}
