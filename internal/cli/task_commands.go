package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/spf13/cobra"
)

type taskSummary struct {
	ID       string `json:"id"`
	Number   int64  `json:"number"`
	Title    string `json:"title"`
	Version  int64  `json:"version"`
	Phase    string `json:"phase"`
	Activity string `json:"activity"`
	Assignee *struct {
		ID string `json:"id"`
	} `json:"assignee"`
}

type taskPage struct {
	Items      []taskSummary `json:"items"`
	NextCursor string        `json:"next_cursor,omitempty"`
}

func (a *App) taskCommand() *cobra.Command {
	command := &cobra.Command{Use: "task", Short: "Discover and claim execution Tasks"}
	command.AddCommand(a.taskListCommand(), a.taskShowCommand(), a.taskClaimCommand())
	return command
}

func (a *App) taskListCommand() *cobra.Command {
	return &cobra.Command{
		Use: "list", Short: "List execution Tasks assigned to you and ready to claim",
		Long: "Lists assigned Tasks in ready or in_progress.available. Review work is intentionally omitted from CLI v0.1.",
		Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			principalBody, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/me", nil, 0, "", false)
			if err != nil {
				return err
			}
			var principal struct {
				Subject struct {
					ID string `json:"id"`
				} `json:"subject"`
			}
			if err := json.Unmarshal(principalBody, &principal); err != nil {
				return err
			}
			query := url.Values{"limit": {"200"}, "assignee": {principal.Subject.ID}, "archived": {"exclude"}}
			allTasks := []taskSummary{}
			for {
				body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/tasks?"+query.Encode(), nil, 0, "", false)
				if err != nil {
					return err
				}
				var page taskPage
				if err := json.Unmarshal(body, &page); err != nil {
					return err
				}
				allTasks = append(allTasks, page.Items...)
				if page.NextCursor == "" {
					break
				}
				query.Set("cursor", page.NextCursor)
			}
			filtered := make([]taskSummary, 0, len(allTasks))
			for _, task := range allTasks {
				if task.Phase == "ready" || (task.Phase == "in_progress" && task.Activity == "available") {
					filtered = append(filtered, task)
				}
			}
			return a.output(map[string]any{"items": filtered}, func(w io.Writer) {
				if len(filtered) == 0 {
					fmt.Fprintln(w, "No claimable execution Tasks assigned to you.")
					return
				}
				for _, task := range filtered {
					fmt.Fprintf(w, "#%d  v%d  %-11s  %s\n", task.Number, task.Version, lifecycle(task.Phase, task.Activity), task.Title)
				}
			})
		},
	}
}

func (a *App) taskShowCommand() *cobra.Command {
	return &cobra.Command{
		Use: "show <task-number>", Short: "Show a Task with criteria, Threads, and delivery",
		Long: "Reads a complete work packet without claiming, reserving, or mutating the Task.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			number, err := parsePositive(args[0], "task-number")
			if err != nil {
				return err
			}
			data, err := a.taskPacket(command, number)
			if err != nil {
				return err
			}
			return a.output(data, func(w io.Writer) { printTaskPacket(w, data) })
		},
	}
}

func (a *App) taskClaimCommand() *cobra.Command {
	var version int64
	command := &cobra.Command{
		Use: "claim <task-number>", Short: "Claim an available execution Task",
		Long: `Creates one execution Claim for the Task version you inspected.

The response Claim ID is the explicit target for every later command. This
does not submit or complete work. CLI v0.1 refuses review-stage Claims.`,
		Example: "pactline task claim 142 --task-version 4",
		Args:    cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			number, err := parsePositive(args[0], "task-number")
			if err != nil {
				return err
			}
			if err := requiredPositive("task-version", version); err != nil {
				return err
			}
			inspectedBody, _, err := a.client.request(command.Context(), http.MethodGet,
				fmt.Sprintf("/api/v1/tasks/%d", number), nil, 0, "", false)
			if err != nil {
				return err
			}
			var inspected taskSummary
			if err := json.Unmarshal(inspectedBody, &inspected); err != nil {
				return err
			}
			if inspected.Phase == "in_review" && inspected.Activity == "available" {
				return &APIError{
					Code: "REVIEW_NOT_SUPPORTED", Message: "CLI v0.1 does not claim Task review work",
					Hint: "Claim and review this Task in the Web UI.",
				}
			}
			body, _, err := a.client.request(command.Context(), http.MethodPost,
				fmt.Sprintf("/api/v1/tasks/%d/claims", number), nil, version, a.idempotencyKey, true)
			if err != nil {
				return err
			}
			var result claimCommandResult
			if err := json.Unmarshal(body, &result); err != nil {
				return err
			}
			if result.Claim.Stage != "execution" {
				return &APIError{Code: "UNEXPECTED_CLAIM_STAGE", Message: "server returned a non-execution Claim", Hint: "Release the Claim in the Web UI and inspect the Task state."}
			}
			return a.output(result, func(w io.Writer) {
				fmt.Fprintf(w, "Claim ID: %s\nTask: #%d\nStage: %s\nTask version: %d\n", result.Claim.ID, result.Task.TaskNumber, result.Claim.Stage, result.Task.Version)
			})
		},
	}
	command.Flags().Int64Var(&version, "task-version", 0, "Task version previously inspected (required)")
	return command
}

func (a *App) taskPacket(command *cobra.Command, number int64) (map[string]any, error) {
	paths := []struct{ name, path string }{
		{"task", fmt.Sprintf("/api/v1/tasks/%d", number)},
		{"threads", fmt.Sprintf("/api/v1/tasks/%d/threads", number)},
		{"delivery", fmt.Sprintf("/api/v1/tasks/%d/merge-requests", number)},
	}
	result := map[string]any{}
	for _, resource := range paths {
		body, _, err := a.client.request(command.Context(), http.MethodGet, resource.path, nil, 0, "", false)
		if err != nil {
			return nil, err
		}
		var value any
		if err := json.Unmarshal(body, &value); err != nil {
			return nil, err
		}
		result[resource.name] = value
	}
	criteria, err := a.pagedObject(command, fmt.Sprintf("/api/v1/tasks/%d/criteria?limit=200", number))
	if err != nil {
		return nil, err
	}
	result["criteria"] = criteria
	threads, _ := result["threads"].(map[string]any)
	threadValues, _ := threads["items"].([]any)
	threadItems := map[string]any{}
	for _, rawThread := range threadValues {
		thread, _ := rawThread.(map[string]any)
		threadID := stringValue(thread["id"])
		if threadID == "" {
			continue
		}
		items, err := a.pagedObject(command, "/api/v1/threads/"+threadID+"/items?limit=200")
		if err != nil {
			return nil, err
		}
		threadItems[threadID] = items
	}
	result["thread_items"] = threadItems
	return result, nil
}

func (a *App) pagedObject(command *cobra.Command, path string) (map[string]any, error) {
	items := []any{}
	endpoint, err := url.Parse(path)
	if err != nil {
		return nil, fmt.Errorf("parse page path: %w", err)
	}
	for {
		body, _, err := a.client.request(command.Context(), http.MethodGet, endpoint.String(), nil, 0, "", false)
		if err != nil {
			return nil, err
		}
		var page map[string]any
		if err := json.Unmarshal(body, &page); err != nil {
			return nil, err
		}
		pageItems, _ := page["items"].([]any)
		items = append(items, pageItems...)
		next := stringValue(page["next_cursor"])
		if next == "" {
			break
		}
		query := endpoint.Query()
		query.Set("cursor", next)
		endpoint.RawQuery = query.Encode()
	}
	return map[string]any{"items": items}, nil
}

func printTaskPacket(w io.Writer, data map[string]any) {
	task, _ := data["task"].(map[string]any)
	fmt.Fprintf(w, "Task: #%v\nTitle: %v\nVersion: %v\nState: %s\n", task["number"], task["title"], task["version"], lifecycle(stringValue(task["phase"]), stringValue(task["activity"])))
	fmt.Fprintf(w, "Context: %v\nExpected result: %v\n", task["context"], task["expected_result"])
	if description := stringValue(task["description"]); description != "" {
		fmt.Fprintf(w, "Description: %s\n", description)
	}
	criteria, _ := data["criteria"].(map[string]any)
	items, _ := criteria["items"].([]any)
	fmt.Fprintf(w, "Acceptance criteria: %d\n", len(items))
	for _, raw := range items {
		item, _ := raw.(map[string]any)
		fmt.Fprintf(w, "  - %v (id=%v revision=%v)\n", item["criterion"], item["id"], item["revision"])
		fmt.Fprintf(w, "    Verify: %v\n", item["verification_instructions"])
		if check, ok := item["current_check"].(map[string]any); ok {
			fmt.Fprintf(w, "    Current check: %v — %v\n", check["outcome"], check["evidence"])
		}
	}
	threads, _ := data["threads"].(map[string]any)
	threadValues, _ := threads["items"].([]any)
	threadItems, _ := data["thread_items"].(map[string]any)
	fmt.Fprintf(w, "Threads: %d\n", len(threadValues))
	for _, raw := range threadValues {
		thread, _ := raw.(map[string]any)
		threadID := stringValue(thread["id"])
		itemsForThread, _ := threadItems[threadID].(map[string]any)
		values, _ := itemsForThread["items"].([]any)
		label := stringValue(thread["role"])
		if issueType := stringValue(thread["issue_type"]); issueType != "" {
			label += " " + issueType + " " + stringValue(thread["issue_status"])
		}
		fmt.Fprintf(w, "  - %s: %d items (id=%s)\n", label, len(values), threadID)
		for _, rawItem := range values {
			item, _ := rawItem.(map[string]any)
			fmt.Fprintf(w, "    [%v] %v\n", item["kind"], item["body"])
		}
	}
	delivery, _ := data["delivery"].(map[string]any)
	links, _ := delivery["active_links"].([]any)
	fmt.Fprintf(w, "Delivery: %d active Merge Requests\n", len(links))
	for _, raw := range links {
		link, _ := raw.(map[string]any)
		fmt.Fprintf(w, "  - %v — %v\n", link["web_url"], link["title"])
	}
	if review, ok := delivery["review"].(map[string]any); ok {
		fmt.Fprintf(w, "Review snapshot: cycle %v\n", review["review_cycle"])
	}
}

func lifecycle(phase, activity string) string {
	if strings.TrimSpace(activity) == "" {
		return phase
	}
	return phase + "." + activity
}
func stringValue(value any) string { text, _ := value.(string); return text }
