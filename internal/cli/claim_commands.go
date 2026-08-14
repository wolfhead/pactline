package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
)

type claimSummary struct {
	ID         string `json:"id"`
	TaskNumber int64  `json:"task_number"`
	Stage      string `json:"stage"`
	Status     string `json:"status"`
	Outcome    string `json:"outcome,omitempty"`
	Version    int64  `json:"version"`
}

type workflowSummary struct {
	TaskNumber int64  `json:"task_number"`
	Version    int64  `json:"version"`
	Phase      string `json:"phase"`
	Activity   string `json:"activity"`
}
type claimCommandResult struct {
	Task  workflowSummary `json:"task"`
	Claim claimSummary    `json:"claim"`
}

func (a *App) claimCommand() *cobra.Command {
	command := &cobra.Command{Use: "claim", Short: "Continue an explicit Claim"}
	command.AddCommand(
		a.claimListCommand(), a.claimShowCommand(), a.claimProgressCommand(),
		a.claimVerifyCommand(), a.claimBodyCommand("submit"),
		a.claimBodyCommand("complete"), a.claimBodyCommand("release"),
		a.claimBodyCommand("request-changes"), a.claimBodyCommand("accept"),
		a.claimResolutionCommand(), a.claimCodeChangeCommand(),
	)
	return command
}

func (a *App) claimListCommand() *cobra.Command {
	return &cobra.Command{Use: "list", Short: "List active Claims owned by this logical principal", Long: "Lists all active Claims owned by the authenticated Token or Agent Run, regardless of Client Session ID.", Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
		page := struct {
			Items []claimSummary `json:"items"`
		}{}
		path := "/api/v1/claims?status=active&limit=200"
		for {
			body, _, err := a.client.request(command.Context(), http.MethodGet, path, nil, 0, "", false)
			if err != nil {
				return err
			}
			var current struct {
				Items      []claimSummary `json:"items"`
				NextCursor string         `json:"next_cursor"`
			}
			if err := json.Unmarshal(body, &current); err != nil {
				return err
			}
			page.Items = append(page.Items, current.Items...)
			if current.NextCursor == "" {
				break
			}
			path = "/api/v1/claims?status=active&limit=200&cursor=" + url.QueryEscape(current.NextCursor)
		}
		return a.output(page, func(w io.Writer) {
			if len(page.Items) == 0 {
				fmt.Fprintln(w, "No active Claims.")
				return
			}
			for _, claim := range page.Items {
				fmt.Fprintf(w, "%s  Task #%d  %-9s  %s\n", claim.ID, claim.TaskNumber, claim.Stage, claim.Status)
			}
		})
	}}
}

func (a *App) claimShowCommand() *cobra.Command {
	var compact bool
	var threadItemsLimit int
	command := &cobra.Command{Use: "show <claim-id>", Short: "Show one Claim and its Task work packet", Long: "The Claim ID selects work explicitly. This command never chooses a Claim from Client Session ID. --compact asks the server for bounded recent Thread context and Claim-specific checks.", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if !compact && command.Flags().Changed("thread-items-limit") {
			return &APIError{Code: "USAGE", Message: "--thread-items-limit requires --compact"}
		}
		if threadItemsLimit < 1 || threadItemsLimit > 100 {
			return &APIError{Code: "USAGE", Message: "--thread-items-limit must be between 1 and 100"}
		}
		claimID, err := parseUUID(args[0], "claim-id")
		if err != nil {
			return err
		}
		if compact {
			packet, err := a.compactPacket(command, fmt.Sprintf("/api/v1/claims/%s/work-packet?thread_items_limit=%d", claimID, threadItemsLimit))
			if err != nil {
				return err
			}
			return a.output(packet, func(w io.Writer) {
				claim, _ := packet["claim"].(map[string]any)
				fmt.Fprintf(w, "Claim ID: %v\nStage: %v\nStatus: %v\n", claim["id"], claim["stage"], claim["status"])
				printTaskPacket(w, packet)
			})
		}
		body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/claims/"+claimID, nil, 0, "", false)
		if err != nil {
			return err
		}
		var claim claimSummary
		if err := json.Unmarshal(body, &claim); err != nil {
			return err
		}
		packet, err := a.taskPacket(command, claim.TaskNumber)
		if err != nil {
			return err
		}
		data := map[string]any{"claim": claim, "work": packet}
		return a.output(data, func(w io.Writer) {
			fmt.Fprintf(w, "Claim ID: %s\nStage: %s\nStatus: %s\n", claim.ID, claim.Stage, claim.Status)
			printTaskPacket(w, packet)
		})
	}}
	command.Flags().BoolVar(&compact, "compact", false, "read a bounded server-aggregated work packet")
	command.Flags().IntVar(&threadItemsLimit, "thread-items-limit", 20, "recent Items per included Thread (1-100; requires --compact)")
	return command
}

func (a *App) claimCodeChangeCommand() *cobra.Command {
	command := &cobra.Command{Use: "change", Short: "Inspect and change repository delivery for a Claim"}
	command.AddCommand(
		a.claimCodeChangeListCommand(),
		a.claimCodeChangeLinkCommand(),
		a.claimCodeChangeUnlinkCommand(),
	)
	return command
}

func (a *App) claimCodeChangeListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list <claim-id>", Short: "List current and frozen code-change delivery for a Claim",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			claimID, err := parseUUID(args[0], "claim-id")
			if err != nil {
				return err
			}
			body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/claims/"+claimID, nil, 0, "", false)
			if err != nil {
				return err
			}
			var claim claimSummary
			if err := json.Unmarshal(body, &claim); err != nil {
				return err
			}
			delivery, _, err := a.client.request(command.Context(), http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/code-changes", claim.TaskNumber), nil, 0, "", false)
			if err != nil {
				return err
			}
			var value map[string]any
			if err := json.Unmarshal(delivery, &value); err != nil {
				return err
			}
			return a.output(value, func(w io.Writer) { printCodeChangeDelivery(w, value) })
		},
	}
}

func (a *App) claimCodeChangeLinkCommand() *cobra.Command {
	var codeChangeURL string
	var version int64
	command := &cobra.Command{
		Use: "link <claim-id>", Short: "Link one repository code change to a Claim",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			claimID, err := parseUUID(args[0], "claim-id")
			if err != nil {
				return err
			}
			if err := requiredPositive("task-version", version); err != nil {
				return err
			}
			parsed, err := url.Parse(codeChangeURL)
			if err != nil || parsed.Host == "" || (parsed.Scheme != "http" && parsed.Scheme != "https") {
				return &APIError{Code: "USAGE", Message: "--url must be an absolute HTTP(S) code change URL"}
			}
			response, _, err := a.client.request(
				command.Context(), http.MethodPost, "/api/v1/claims/"+claimID+"/code-changes",
				map[string]any{"code_change_url": codeChangeURL}, version, a.idempotencyKey, true,
			)
			if err != nil {
				return err
			}
			return a.outputRaw(response, "Code change linked")
		},
	}
	command.Flags().StringVar(&codeChangeURL, "url", "", "absolute Pull Request or Merge Request URL (required)")
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	return command
}

func (a *App) claimCodeChangeUnlinkCommand() *cobra.Command {
	var version int64
	command := &cobra.Command{
		Use: "unlink <claim-id> <link-id>", Short: "Unlink one code change by its Pactline link ID",
		Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			claimID, err := parseUUID(args[0], "claim-id")
			if err != nil {
				return err
			}
			linkID, err := parseUUID(args[1], "link-id")
			if err != nil {
				return err
			}
			if err := requiredPositive("task-version", version); err != nil {
				return err
			}
			response, _, err := a.client.request(
				command.Context(), http.MethodDelete,
				"/api/v1/claims/"+claimID+"/code-changes/"+linkID,
				nil, version, a.idempotencyKey, true,
			)
			if err != nil {
				return err
			}
			return a.outputRaw(response, "Code change unlinked")
		},
	}
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	return command
}

func (a *App) claimProgressCommand() *cobra.Command {
	var message, file string
	command := &cobra.Command{Use: "progress <claim-id>", Short: "Append progress without ending the Claim", Long: "Adds immutable progress to the Task Main Thread. It preserves Task phase, Task version, Claim status, and Claim version.", Example: `pactline claim progress <claim-id> --message "Focused tests pass"`, Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := a.requireMutationProvenance(); err != nil {
			return err
		}
		claimID, err := parseUUID(args[0], "claim-id")
		if err != nil {
			return err
		}
		body, err := contentWithFlags(message, file, a.stdin, "message", "message", "file")
		if err != nil {
			return err
		}
		response, _, err := a.client.request(command.Context(), http.MethodPost, "/api/v1/claims/"+claimID+"/progress", map[string]any{"body": body}, 0, a.idempotencyKey, true)
		if err != nil {
			return err
		}
		return a.outputRaw(response, "Progress recorded")
	}}
	command.Flags().StringVar(&message, "message", "", "inline progress text")
	command.Flags().StringVar(&file, "file", "", "read progress from file; use - for stdin")
	return command
}

func (a *App) claimVerifyCommand() *cobra.Command {
	var version, revision int64
	var outcome, evidence, evidenceFile string
	command := &cobra.Command{Use: "verify <claim-id> <criterion-id>", Short: "Record Claim-owned criterion evidence", Long: "Records execution verification for an execution Claim or acceptance evidence for a review Claim. The server derives purpose and review cycle from the explicit Claim. Criterion revision and the Task version you inspected are explicit concurrency inputs.", Args: cobra.ExactArgs(2), RunE: func(command *cobra.Command, args []string) error {
		if err := a.requireMutationProvenance(); err != nil {
			return err
		}
		claimID, err := parseUUID(args[0], "claim-id")
		if err != nil {
			return err
		}
		criterionID, err := parseUUID(args[1], "criterion-id")
		if err != nil {
			return err
		}
		if err := requiredPositive("task-version", version); err != nil {
			return err
		}
		if revision < 1 {
			return &APIError{Code: "USAGE", Message: "--criterion-revision must be positive"}
		}
		if outcome != "passed" && outcome != "failed" && outcome != "unable" && outcome != "waived" {
			return &APIError{Code: "USAGE", Message: "--outcome must be passed, failed, unable, or waived"}
		}
		text, err := content(evidence, evidenceFile, a.stdin, "evidence")
		if err != nil {
			return err
		}
		response, _, err := a.client.request(command.Context(), http.MethodPost, fmt.Sprintf("/api/v1/claims/%s/criteria/%s/checks", claimID, criterionID), map[string]any{"criterion_revision": revision, "outcome": outcome, "evidence": text}, version, a.idempotencyKey, true)
		if err != nil {
			return err
		}
		var check map[string]any
		if err := json.Unmarshal(response, &check); err != nil {
			return err
		}
		message := "Execution verification recorded"
		if check["purpose"] == "acceptance" {
			message = "Acceptance evidence recorded"
		}
		return a.output(check, func(w io.Writer) { fmt.Fprintln(w, message) })
	}}
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	command.Flags().Int64Var(&revision, "criterion-revision", 0, "criterion revision verified (required)")
	command.Flags().StringVar(&outcome, "outcome", "", "passed, failed, unable, or waived (required)")
	command.Flags().StringVar(&evidence, "evidence", "", "inline evidence")
	command.Flags().StringVar(&evidenceFile, "evidence-file", "", "read evidence from file; use - for stdin")
	return command
}

func (a *App) claimBodyCommand(name string) *cobra.Command {
	var version int64
	var message, file string
	settings := map[string]struct {
		short, path, effect, detail, success, example, messageHelp string
	}{
		"submit": {
			"Record a repeatable work submission", "submissions", "keeps the execution Claim active",
			"This is an execution-only delivery update. It does not complete execution or release the Claim.",
			"Submission succeeded", `pactline claim submit <claim-id> --task-version 4 --message "Delivery update"`, "inline delivery summary",
		},
		"complete": {
			"Complete execution and enter Task review", "complete-execution", "ends execution and moves the Task to in_review.available",
			"This freezes the current delivery snapshot for the next review cycle. It does not accept the Task.",
			"Complete succeeded", `pactline claim complete <claim-id> --task-version 4 --message "Ready for review"`, "inline execution completion summary",
		},
		"release": {
			"Release the Claim with a durable handoff", "release", "ends the Claim and keeps the Task phase available",
			"Release works in execution or review. It does not complete execution, request changes, or accept the Task.",
			"Release succeeded", `pactline claim release <claim-id> --task-version 4 --message "Handoff for the next worker"`, "inline handoff",
		},
		"request-changes": {
			"Return reviewed work to execution", "request-changes", "ends review and moves the Task to in_progress.available",
			"This requires an active Review Claim. The message becomes the durable review outcome; this command does not open an Issue Thread.",
			"Changes requested", `pactline claim request-changes <claim-id> --task-version 9 --message "The error path still lacks coverage"`, "inline change request",
		},
		"accept": {
			"Accept the reviewed Task as done", "accept", "ends review and moves the Task to done",
			"This requires an active Review Claim and passing current-cycle acceptance evidence for every active criterion.",
			"Task accepted", `pactline claim accept <claim-id> --task-version 9 --message "Acceptance contract satisfied"`, "inline acceptance summary",
		},
	}[name]
	command := &cobra.Command{Use: name + " <claim-id>", Short: settings.short, Long: fmt.Sprintf("Targets exactly one Claim ID and %s. It never infers a Claim from Client Session ID. %s Reuse --idempotency-key only after an uncertain network outcome.", settings.effect, settings.detail), Example: settings.example, Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := a.requireMutationProvenance(); err != nil {
			return err
		}
		claimID, err := parseUUID(args[0], "claim-id")
		if err != nil {
			return err
		}
		if err := requiredPositive("task-version", version); err != nil {
			return err
		}
		body, err := contentWithFlags(message, file, a.stdin, "message", "message", "file")
		if err != nil {
			return err
		}
		response, _, err := a.client.request(command.Context(), http.MethodPost, "/api/v1/claims/"+claimID+"/"+settings.path, map[string]any{"body": body}, version, a.idempotencyKey, true)
		if err != nil {
			return err
		}
		return a.outputRaw(response, settings.success)
	}}
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	command.Flags().StringVar(&message, "message", "", settings.messageHelp)
	command.Flags().StringVar(&file, "file", "", "read text from file; use - for stdin")
	return command
}

func (a *App) claimResolutionCommand() *cobra.Command {
	var version int64
	var issueType, message, file string
	command := &cobra.Command{Use: "request-resolution <claim-id>", Short: "End the Claim and open a typed blocking Issue Thread", Long: "Creates a decision_required or dependency_required Issue Thread. Resolving it returns the same Task phase to available; it never revives this Claim.", Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
		if err := a.requireMutationProvenance(); err != nil {
			return err
		}
		claimID, err := parseUUID(args[0], "claim-id")
		if err != nil {
			return err
		}
		if err := requiredPositive("task-version", version); err != nil {
			return err
		}
		if issueType != "decision_required" && issueType != "dependency_required" {
			return &APIError{Code: "USAGE", Message: "--issue-type must be decision_required or dependency_required"}
		}
		request, err := contentWithFlags(message, file, a.stdin, "message", "message", "file")
		if err != nil {
			return err
		}
		response, _, err := a.client.request(command.Context(), http.MethodPost, "/api/v1/claims/"+claimID+"/request-resolution", map[string]any{"issue_type": issueType, "request": request}, version, a.idempotencyKey, true)
		if err != nil {
			return err
		}
		return a.outputRaw(response, "Resolution requested")
	}}
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	command.Flags().StringVar(&issueType, "issue-type", "", "decision_required or dependency_required (required)")
	command.Flags().StringVar(&message, "message", "", "inline resolution request")
	command.Flags().StringVar(&file, "file", "", "read request from file; use - for stdin")
	return command
}

func (a *App) outputRaw(body json.RawMessage, message string) error {
	var value any
	if err := json.Unmarshal(body, &value); err != nil {
		return err
	}
	return a.output(value, func(w io.Writer) {
		fmt.Fprintln(w, message)
		if object, ok := value.(map[string]any); ok {
			if claim, ok := object["claim"].(map[string]any); ok {
				fmt.Fprintf(w, "Claim ID: %v\nStatus: %v\n", claim["id"], claim["status"])
			}
			if task, ok := object["task"].(map[string]any); ok {
				fmt.Fprintf(w, "Task: #%v\nTask version: %v\nState: %s\n", task["task_number"], task["version"], lifecycle(stringValue(task["phase"]), stringValue(task["activity"])))
			}
		}
	})
}

func printCodeChangeDelivery(w io.Writer, delivery map[string]any) {
	links, _ := delivery["active_links"].([]any)
	fmt.Fprintf(w, "Active code changes: %d\n", len(links))
	for _, raw := range links {
		link, _ := raw.(map[string]any)
		observation, _ := link["latest_observation"].(map[string]any)
		fmt.Fprintf(w, "  %v  %v  %v\n", link["id"], observation["state"], link["web_url"])
	}
	if review, ok := delivery["review"].(map[string]any); ok {
		comparisons, _ := review["code_changes"].([]any)
		fmt.Fprintf(w, "Frozen review snapshot: cycle %v, %d code changes\n", review["review_cycle"], len(comparisons))
	}
}

func parseUUID(value, label string) (string, error) {
	parsed, err := uuid.Parse(value)
	if err != nil {
		return "", &APIError{Code: "USAGE", Message: label + " must be a UUID"}
	}
	return parsed.String(), nil
}
