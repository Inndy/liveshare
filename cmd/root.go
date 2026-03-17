package cmd

import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{
	Use:   "liveshare",
	Short: "Share files over HTTP via WebSocket relay",
}

func init() {
	rootCmd.AddCommand(hostCmd)
	rootCmd.AddCommand(createCmd)
	rootCmd.AddCommand(shareCmd)
}

func Execute() error {
	return rootCmd.Execute()
}
