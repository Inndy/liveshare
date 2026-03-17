package cmd

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"liveshare/config"
)

var createCmd = &cobra.Command{
	Use:   "create [name]",
	Short: "Create a new share token",
	Args:  cobra.MaximumNArgs(1),
	RunE:  runCreate,
}

func init() {
	createCmd.Flags().String("config", config.DefaultConfigFile, "Config file path")
}

func runCreate(cmd *cobra.Command, args []string) error {
	cfgPath, _ := cmd.Flags().GetString("config")
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	cfg.ApplyDefaults()

	name := ""
	if len(args) > 0 {
		name = args[0]
	}

	tokenBytes := make([]byte, 16)
	if _, err := rand.Read(tokenBytes); err != nil {
		return fmt.Errorf("generate token: %w", err)
	}
	token := hex.EncodeToString(tokenBytes)

	f, err := os.OpenFile(cfg.TokenFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("open token file: %w", err)
	}
	defer f.Close()

	line := token
	if name != "" {
		line = token + "\t" + name
	}
	if _, err := fmt.Fprintln(f, line); err != nil {
		return fmt.Errorf("write token: %w", err)
	}

	host := cfg.Hostname
	if host == "" {
		host = "<SERVER>"
	}
	fmt.Printf("Token: %s\n", token)
	fmt.Printf("liveshare share --server %s/ws/%s <file>\n", host, token)
	return nil
}
