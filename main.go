package main

import (
	"log/slog"
	"os"

	"liveshare/cmd"
)

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
