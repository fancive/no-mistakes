package cli

import (
	"errors"
	"fmt"

	"github.com/spf13/cobra"
)

type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string {
	if e.err != nil {
		return e.err.Error()
	}
	return ""
}

func (e *exitError) Unwrap() error { return e.err }

// Execute runs the stateless lean CLI and returns its process exit code.
func Execute() int {
	root := newRootCmd()
	if err := root.Execute(); err != nil {
		var exit *exitError
		if errors.As(err, &exit) {
			if exit.err != nil {
				fmt.Fprintln(root.ErrOrStderr(), exit.err)
			}
			return exit.code
		}
		fmt.Fprintln(root.ErrOrStderr(), err)
		return 1
	}
	return 0
}

func newRootCmd() *cobra.Command { return newLeanRootCmd(newLeanRuntime(".")) }
