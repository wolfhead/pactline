package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/spf13/cobra"
)

func (a *App) issueCommand() *cobra.Command {
	command := &cobra.Command{Use: "issue", Short: "Resolve explicit blocking Issue Threads"}
	command.AddCommand(a.issueResolveCommand())
	return command
}

func (a *App) issueResolveCommand() *cobra.Command {
	var taskVersion, threadVersion int64
	var message, file string
	command := &cobra.Command{
		Use: "resolve <task-number> <issue-thread-id>", Short: "Resolve the active Issue and return its Task phase to available",
		Long: "Targets the Task and Issue Thread explicitly after the old Claim has ended. Resolution never revives or infers a Claim; another worker must claim the available phase explicitly.",
		Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			number, err := positiveInt64(args[0], "task-number")
			if err != nil {
				return err
			}
			issueID, err := parseUUID(args[1], "issue-thread-id")
			if err != nil {
				return err
			}
			if err := requiredPositive("task-version", taskVersion); err != nil {
				return err
			}
			if err := requiredPositive("thread-version", threadVersion); err != nil {
				return err
			}
			resolution, err := contentWithFlags(message, file, a.stdin, "resolution", "message", "file")
			if err != nil {
				return err
			}
			response, _, err := a.client.request(
				command.Context(), http.MethodPost,
				fmt.Sprintf("/api/v1/tasks/%d/issues/%s/resolve", number, issueID),
				map[string]any{"thread_version": threadVersion, "resolution": resolution},
				taskVersion, a.idempotencyKey, true,
			)
			if err != nil {
				return err
			}
			var result struct {
				Task  workflowSummary `json:"task"`
				Issue taskThread      `json:"issue"`
			}
			if err := json.Unmarshal(response, &result); err != nil {
				return err
			}
			return a.output(result, func(w io.Writer) {
				fmt.Fprintln(w, "Issue resolved")
				fmt.Fprintf(w, "Task: #%d\nTask version: %d\nState: %s\n", result.Task.TaskNumber, result.Task.Version, lifecycle(result.Task.Phase, result.Task.Activity))
				fmt.Fprintf(w, "Issue Thread: %s\nIssue version: %d\nStatus: %s\n", result.Issue.ID, result.Issue.Version, result.Issue.IssueStatus)
				if result.Issue.ResolvedBy != nil {
					fmt.Fprintf(w, "Resolved by: %s\n", actorLabel(*result.Issue.ResolvedBy))
				}
			})
		},
	}
	command.Flags().Int64Var(&taskVersion, "task-version", 0, "Task version previously inspected (required)")
	command.Flags().Int64Var(&threadVersion, "thread-version", 0, "Issue Thread version previously inspected (required)")
	command.Flags().StringVar(&message, "message", "", "inline resolution conclusion")
	command.Flags().StringVar(&file, "file", "", "read resolution from file; use - for stdin")
	return command
}
