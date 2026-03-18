package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

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
	if args := os.Args[1:]; len(args) > 0 && args[0] != "" && args[0][0] != '-' {
		firstArg := args[0]
		known := false
		for _, cmd := range rootCmd.Commands() {
			if cmd.Name() == firstArg {
				known = true
				break
			}
			for _, alias := range cmd.Aliases {
				if alias == firstArg {
					known = true
					break
				}
			}
		}
		if !known && firstArg != "help" && firstArg != "completion" {
			rootCmd.SetArgs(append([]string{"share"}, args...))
		}
	}
	return rootCmd.Execute()
}
