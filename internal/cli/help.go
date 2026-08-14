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

1. capabilities --json verifies the installed offline integration contract.
2. task list finds assigned execution work; --stage review finds review work.
3. task show --compact provides bounded context, Task version, and criteria.
4. task claim creates an execution Claim and returns its explicit Claim ID.
5. claim show --compact returns bounded Claim-specific context and checks.
6. claim progress and claim submit may be repeated without ending the Claim.
7. claim verify records criterion evidence against an explicit revision.
8. claim mr link records each GitLab Merge Request as delivery evidence.
9. claim complete ends execution and moves the Task to in_review.available.

Code review and Task acceptance remain review work in the Web UI for v0.1.
Neither MR status nor work submission implicitly releases a Claim.`,
		"identity": `Identity and continuation

A Claim belongs to the authenticated logical principal: exact API Token,
delegated Agent Run, or human session identity. Client Session ID is only audit
provenance. An authorized subagent may continue the same explicit Claim ID from
a different Session ID; a different Token cannot.`,
		"output": `Output and recovery

Human-readable text is the default. --json writes exactly one JSON document to
stdout. Successful JSON may include request_id, ETag, and the exact mutation
idempotency key under meta. --verbose writes redacted request diagnostics only to stderr and never
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
