package main

import (
	"errors"
	"fmt"
	"os"

	"agt/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		var exitErr cli.ExitCodeError
		if errors.As(err, &exitErr) {
			if exitErr.Message != "" {
				fmt.Fprintln(os.Stderr, exitErr.Message)
			}
			os.Exit(exitErr.ExitCode())
		}
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
