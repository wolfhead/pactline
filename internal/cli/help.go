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
2. task list --stage execution finds assigned execution work.
3. task show --compact provides bounded context, Task version, and criteria.
4. task claim --stage execution creates a Claim and returns its explicit ID.
5. claim show --compact returns bounded Claim-specific context and checks.
6. claim progress and claim submit may be repeated without ending the Claim.
7. claim verify records execution verification against an explicit revision.
8. claim mr link records each GitLab Merge Request as delivery evidence.
9. claim complete ends execution and moves the Task to in_review.available.

Review workflow

1. task list --stage review finds visible in_review.available work.
2. task show --compact provides the frozen delivery and acceptance contract.
3. task claim --stage review makes the Review assertion explicit.
4. claim show --compact returns current-cycle Claim-specific context.
5. The reviewer performs Code Review and verification through repository tools.
6. claim verify records acceptance evidence; the server derives its purpose.
7. claim request-changes returns the Task to in_progress.available, or
   claim accept completes it as done.

Neither MR state, work submission, nor Claim release implicitly accepts a Task.`,
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
