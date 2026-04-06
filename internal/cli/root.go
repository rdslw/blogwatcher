package cli

import (
	"fmt"
	"os"

	"github.com/rdslw/blogwatcher/internal/version"
	"github.com/spf13/cobra"
)

func NewRootCommand() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:           "blogwatcher",
		Short:         fmt.Sprintf("BlogWatcher %s - Track blog articles and detect new posts.", version.Version),
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	rootCmd.Version = version.Version
	rootCmd.SetVersionTemplate("{{.Version}}\n")
	rootCmd.AddCommand(newAddCommand())
	rootCmd.AddCommand(newRemoveCommand())
	rootCmd.AddCommand(newBlogsCommand())
	rootCmd.AddCommand(newExportCommand())
	rootCmd.AddCommand(newScanCommand())
	rootCmd.AddCommand(newArticlesCommand())
	rootCmd.AddCommand(newReadCommand())
	rootCmd.AddCommand(newUnreadCommand())
	rootCmd.AddCommand(newSummaryCommand())
	rootCmd.AddCommand(newInterestCommand())
	rootCmd.AddCommand(newSkillCommand())
	return rootCmd
}

func Execute() {
	if err := NewRootCommand().Execute(); err != nil {
		if !isPrinted(err) {
			fmt.Fprintln(os.Stderr, err)
		}
		os.Exit(1)
	}
}
