package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var listCmd = &cobra.Command{
	Use:   "list",
	Short: "List available Claude projects",
	Long:  "List all available Claude projects found in ~/.claude/projects directory.",
	Run: func(cmd *cobra.Command, args []string) {
		listProjects()
	},
}

func init() {
	rootCmd.AddCommand(listCmd)
}

func listProjects() {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		fmt.Printf("Error getting home directory: %v\n", err)
		return
	}

	projectsDir := filepath.Join(homeDir, ".claude", "projects")
	
	entries, err := os.ReadDir(projectsDir)
	if err != nil {
		fmt.Printf("Error reading projects directory: %v\n", err)
		return
	}

	fmt.Println("Available Claude projects:")
	for _, entry := range entries {
		if entry.IsDir() {
			fmt.Printf("  %s\n", entry.Name())
		}
	}
}