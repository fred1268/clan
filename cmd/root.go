package cmd

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "clan",
	Short: "A CLI tool for analyzing Claude conversation logs",
	Long:  "clan is a command-line tool for analyzing and processing Claude conversation logs.",
}

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		os.Exit(1)
	}
}
