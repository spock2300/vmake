package main

import (
	"embed"
	"fmt"

	"github.com/spf13/cobra"
)

//go:embed doc
var docFS embed.FS

var docCmd = &cobra.Command{
	Use:   "doc",
	Short: "Print documentation for AI agents",
	Run: func(cmd *cobra.Command, args []string) {
		data, _ := docFS.ReadFile("doc/en.md")
		fmt.Print(string(data))
	},
}

func init() {
	RootCmd.AddCommand(docCmd)
}
