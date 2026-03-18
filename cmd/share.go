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
	"liveshare/config"
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
	shareCmd.Flags().Bool("persist", false, "Deterministic share ID (same URL on reconnect)")
	shareCmd.Flags().Bool("dir", false, "Serve directory as static files")
	shareCmd.Flags().Bool("dir-list", false, "Enable directory listing (requires --dir)")
	shareCmd.Flags().Bool("html", false, "Content-Type: text/html")
	shareCmd.Flags().Bool("text", false, "Content-Type: text/plain")
	shareCmd.Flags().String("mime", "", "Content-Type: <type>")
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
	persist, _ := cmd.Flags().GetBool("persist")
	dirMode, _ := cmd.Flags().GetBool("dir")
	dirList, _ := cmd.Flags().GetBool("dir-list")
	useHTML, _ := cmd.Flags().GetBool("html")
	useText, _ := cmd.Flags().GetBool("text")
	mimeFlag, _ := cmd.Flags().GetString("mime")

	if dirMode && (useTar || useTgz) {
		return fmt.Errorf("--dir cannot be combined with --tar/--tgz")
	}
	if dirMode && len(args) != 1 {
		return fmt.Errorf("--dir requires exactly one directory argument")
	}
	if dirMode && oneTime {
		return fmt.Errorf("--dir cannot be combined with --once")
	}
	if dirMode && (useHTML || useText) {
		return fmt.Errorf("--dir auto-detects MIME per file; cannot combine with --html/--text")
	}
	if dirList && !dirMode {
		return fmt.Errorf("--dir-list requires --dir")
	}
	mimeCount := 0
	if useHTML {
		mimeCount++
	}
	if useText {
		mimeCount++
	}
	if mimeFlag != "" {
		mimeCount++
	}
	if mimeCount > 1 {
		return fmt.Errorf("--html, --text, and --mime are mutually exclusive")
	}

	mimeType := ""
	if useHTML {
		mimeType = "text/html"
	} else if useText {
		mimeType = "text/plain"
	} else if mimeFlag != "" {
		mimeType = mimeFlag
	}

	if serverRaw != "" {
		cc := &config.ClientConfig{Server: serverRaw}
		if err := cc.Save(); err != nil {
			slog.Warn("failed to save client config", "error", err)
		}
	} else {
		cc, err := config.LoadClientConfig()
		if err != nil {
			return fmt.Errorf("load client config: %w", err)
		}
		if cc.Server == "" {
			return fmt.Errorf("no server specified; use --server or run once with --server to save it")
		}
		serverRaw = cc.Server
	}

	wsURL, err := parseServerURL(serverRaw)
	if err != nil {
		return err
	}

	var c *client.Client
	if dirMode {
		dirPath := args[0]
		info, err := os.Stat(dirPath)
		if err != nil {
			return fmt.Errorf("stat %s: %w", dirPath, err)
		}
		if !info.IsDir() {
			return fmt.Errorf("%s is not a directory", dirPath)
		}
		if displayName == "" {
			displayName = filepath.Base(dirPath)
		}
		slog.Info("connecting (dir mode)", "url", wsURL, "dir", dirPath, "name", displayName)
		c, err = client.NewFolder(wsURL, dirPath, displayName, dirList, persist)
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	} else {
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

		if archiveMode != "" && (useHTML || useText) {
			return fmt.Errorf("--html/--text cannot be used with archive mode; use --mime instead")
		}

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
			c, err = client.NewArchive(wsURL, args, displayName, archiveMode, persist, mimeType)
		} else {
			filePath := args[0]
			if displayName == "" {
				displayName = filepath.Base(filePath)
			}
			slog.Info("connecting", "url", wsURL, "file", filePath, "name", displayName)
			c, err = client.New(wsURL, filePath, displayName, oneTime, noCache, persist, mimeType)
		}
		if err != nil {
			return fmt.Errorf("connect: %w", err)
		}
	}

	httpURL := wsURL
	httpURL = strings.Replace(httpURL, "wss://", "https://", 1)
	httpURL = strings.Replace(httpURL, "ws://", "http://", 1)
	if u, err := url.Parse(httpURL); err == nil {
		dlPath := fmt.Sprintf("/d/%s/%s", c.ShareID, displayName)
		if dirMode {
			dlPath += "/"
		}
		u.Path = dlPath
		downloadURL := u.String()
		fmt.Printf("Download URL: %s\n", downloadURL)

		if showQR {
			qrterminal.GenerateHalfBlock(downloadURL, qrterminal.L, os.Stdout)
		}
	}

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)

	var timeoutCh <-chan time.Time
	if timeout > 0 {
		timeoutCh = time.After(timeout)
	}

	for {
		errCh := make(chan error, 1)
		go func() {
			errCh <- c.Run()
		}()

		select {
		case err := <-errCh:
			if persist && err != nil {
				slog.Warn("disconnected, reconnecting...", "err", err)
				backoff := time.Second
				for {
					time.Sleep(backoff)
					if reconnErr := c.Reconnect(); reconnErr != nil {
						if strings.Contains(reconnErr.Error(), "share ID already active") {
							return reconnErr
						}
						slog.Warn("reconnect failed, retrying...", "err", reconnErr)
						backoff *= 2
						if backoff > 30*time.Second {
							backoff = 30 * time.Second
						}
						continue
					}
					slog.Info("reconnected", "share_id", c.ShareID)
					break
				}
				continue
			}
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
}
