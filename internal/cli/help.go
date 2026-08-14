package cli

import (
	"fmt"
	"io"

	"github.com/spf13/cobra"
)

func (a *App) helpCommand(root *cobra.Command) *cobra.Command {
	command := &cobra.Command{
		Use: "help [command]", Short: "Explain commands, workflow, identity, and output contracts",
		Args: cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if len(args) == 0 {
				return root.Help()
			}
			target, _, err := root.Find(args)
			if err != nil || target == root {
				return &APIError{Code: "USAGE", Message: "unknown help topic or command: " + args[0]}
			}
			return target.Help()
		},
	}
	topics := map[string]string{
		"workflow": `Execution workflow

1. task list finds assigned execution work.
2. task show provides the current Task version and acceptance criteria.
3. task claim creates an execution Claim and returns its explicit Claim ID.
4. claim progress and claim submit may be repeated without ending the Claim.
5. claim verify records criterion evidence against an explicit revision.
6. claim complete ends execution and moves the Task to in_review.available.

Code review and Task acceptance remain review work in the Web UI for v0.1.
Neither MR status nor work submission implicitly releases a Claim.`,
		"identity": `Identity and continuation

A Claim belongs to the authenticated logical principal: exact API Token,
delegated Agent Run, or human session identity. Client Session ID is only audit
provenance. An authorized subagent may continue the same explicit Claim ID from
a different Session ID; a different Token cannot.`,
		"output": `Output and recovery

Human-readable text is the default. --json writes exactly one JSON document to
stdout. --verbose writes redacted request diagnostics only to stderr and never
prints the Token, request body, evidence, or raw headers.

Exit codes: 0 success, 2 usage/config, 3 authentication/authorization,
4 domain/version conflict, 5 network/provider/unexpected failure. Mutations are
not retried automatically. Reuse --idempotency-key after an uncertain outcome.`,
	}
	for _, name := range []string{"workflow", "identity", "output"} {
		text := topics[name]
		command.AddCommand(&cobra.Command{Use: name, Short: "Explain " + name, Args: cobra.NoArgs, RunE: func(*cobra.Command, []string) error {
			return a.output(map[string]string{"topic": name, "text": text}, func(w io.Writer) { fmt.Fprintln(w, text) })
		}})
	}
	return command
}
