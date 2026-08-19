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
	var amend bool
	cmd := &cobra.Command{
		Use:   "commit",
		Short: "Commit an exact file list under provider-specific submission rules",
		Long: "Stages only repeated --file values and commits them after validating the\n" +
			"provider-specific author and message policy. Pass --amend to update the\n" +
			"current commit in place; omit --message in that mode to reuse the existing\n" +
			"commit message. Paths are repository-relative, must name individual files,\n" +
			"and must include every already-staged file.",
		Args:          cobra.NoArgs,
		SilenceErrors: true,
		SilenceUsage:  true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return trackAxiSurface("axi-commit", "/axi/commit", telemetry.Fields{
				"file_count": len(files),
			}, func() error {
				if !amend && strings.TrimSpace(message) == "" {
					return emitError(cmd, 2, "--message is required",
						`Pass the complete commit message with --message "..."`)
				}
				if len(files) == 0 {
					return emitError(cmd, 2, "at least one --file is required",
						"Repeat --file for every repository-relative file that belongs in the commit")
				}
				return runAxiCommit(cmd, files, message, amend)
			})
		},
	}
	cmd.Flags().StringArrayVar(&files, "file", nil, "repository-relative file to stage; repeat for every file")
	cmd.Flags().StringVarP(&message, "message", "m", "", "complete provider-compliant commit message")
	cmd.Flags().BoolVar(&amend, "amend", false, "amend the current commit instead of creating a new one")
	return cmd
}

func runAxiCommit(cmd *cobra.Command, files []string, message string, amend bool) error {
	result, err := commitprep.Commit(cmd.Context(), commitprep.Options{
		Dir:     ".",
		Files:   files,
		Message: message,
		Amend:   amend,
	})
	if err != nil {
		return emitError(cmd, 1, err.Error(),
			"Inspect `git status --short`; correct the explicit file list or message, then retry")
	}
	emitDoc(cmd,
		toon.Field{Key: "committed", Value: true},
		toon.Field{Key: "amended", Value: result.Amended},
		toon.Field{Key: "sha", Value: result.SHA},
		toon.Field{Key: "branch", Value: result.Branch},
		toon.Field{Key: "provider", Value: string(result.Provider)},
		toon.Field{Key: "files", Value: result.Files},
		toon.Field{Key: "next", Value: `no-mistakes axi run --intent "the user's goal"`},
	)
	return nil
}
