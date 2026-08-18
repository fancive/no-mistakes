package cli

import (
	"strings"

	toon "github.com/toon-format/toon-go"

	"github.com/kunchenguid/no-mistakes/internal/commitprep"
	"github.com/kunchenguid/no-mistakes/internal/telemetry"
	"github.com/spf13/cobra"
)

func newAxiCommitCmd() *cobra.Command {
	var files []string
	var message string
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit an exact file list under provider-specific submission rules",
		Long: "Stages only repeated --file values and commits them after validating the\n" +
			"provider-specific author and message policy. Paths are repository-relative,\n" +
			"must name individual files, and must include every already-staged file.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return trackAxiSurface("axi-commit", "/axi/commit", telemetry.Fields{
				"file_count": len(files),
			}, func() error {
				if strings.TrimSpace(message) == "" {
					return emitError(cmd, 2, "--message is required",
						`Pass the complete commit message with --message "..."`)
				}
				if len(files) == 0 {
					return emitError(cmd, 2, "at least one --file is required",
						"Repeat --file for every repository-relative file that belongs in the commit")
				}
				return runAxiCommit(cmd, files, message)
			})
		},
	}
	cmd.Flags().StringArrayVar(&files, "file", nil, "repository-relative file to stage; repeat for every file")
	cmd.Flags().StringVarP(&message, "message", "m", "", "complete provider-compliant commit message")
	return cmd
}

func runAxiCommit(cmd *cobra.Command, files []string, message string) error {
	result, err := commitprep.Commit(cmd.Context(), commitprep.Options{
		Dir:     ".",
		Files:   files,
		Message: message,
	})
	if err != nil {
		return emitError(cmd, 1, err.Error(),
			"Inspect `git status --short`; correct the explicit file list or message, then retry")
	}
	emitDoc(cmd,
		toon.Field{Key: "committed", Value: true},
		toon.Field{Key: "sha", Value: result.SHA},
		toon.Field{Key: "branch", Value: result.Branch},
		toon.Field{Key: "provider", Value: string(result.Provider)},
		toon.Field{Key: "files", Value: result.Files},
		toon.Field{Key: "next", Value: `no-mistakes axi run --intent "the user's goal"`},
	)
	return nil
}
