package cmd

import (
	"os"
	"slices"

	"github.com/spf13/cobra"
)

var Version = "dev"

var rootCmd = &cobra.Command{
	Use:     "liveshare",
	Short:   "Share files over HTTP via WebSocket relay",
	Version: Version,
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

			known = known || slices.Contains(cmd.Aliases, firstArg)
		}
		if !known && firstArg != "help" && firstArg != "completion" {
			rootCmd.SetArgs(append([]string{"share"}, args...))
		}
	}
	return rootCmd.Execute()
}
