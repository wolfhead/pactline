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
	command := &cobra.Command{Use: "task", Short: "Discover, inspect, and claim execution or review work"}
	command.AddCommand(a.taskListCommand(), a.taskShowCommand(), a.taskClaimCommand(), a.taskThreadsCommand())
	return command
}

func (a *App) taskListCommand() *cobra.Command {
	var stage string
	var projectNumber int64
	var limit int
	command := &cobra.Command{
		Use: "list", Short: "List Tasks currently available for a Claim",
		Long: "Execution discovery is assigned to you. Review discovery is Project-visible because Task assignee is not reviewer assignment. Discovery never claims work.",
		Args: cobra.NoArgs, RunE: func(command *cobra.Command, _ []string) error {
			if stage != "execution" && stage != "review" {
				return &APIError{Code: "USAGE", Message: "--stage must be execution or review"}
			}
			if limit < 1 || limit > 200 {
				return &APIError{Code: "USAGE", Message: "--limit must be between 1 and 200"}
			}
			if command.Flags().Changed("project") && projectNumber < 1 {
				return &APIError{Code: "USAGE", Message: "--project must be a positive integer"}
			}
			query := url.Values{
				"limit": {fmt.Sprint(limit)}, "archived": {"exclude"},
				"claimable_stage": {stage}, "sort": {"number"}, "order": {"asc"},
			}
			if projectNumber > 0 {
				query.Set("project_number", fmt.Sprint(projectNumber))
			}
			if stage == "execution" {
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
				query.Set("assignee", principal.Subject.ID)
			}
			body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/tasks?"+query.Encode(), nil, 0, "", false)
			if err != nil {
				return err
			}
			var page taskPage
			if err := json.Unmarshal(body, &page); err != nil {
				return err
			}
			return a.output(map[string]any{"items": page.Items}, func(w io.Writer) {
				if len(page.Items) == 0 {
					fmt.Fprintf(w, "No claimable %s Tasks.\n", stage)
					return
				}
				for _, task := range page.Items {
					fmt.Fprintf(w, "#%d  v%d  %-11s  %s\n", task.Number, task.Version, lifecycle(task.Phase, task.Activity), task.Title)
				}
			})
		},
	}
	command.Flags().StringVar(&stage, "stage", "execution", "Claim stage to discover: execution or review")
	command.Flags().Int64Var(&projectNumber, "project", 0, "limit discovery to one Project number")
	command.Flags().IntVar(&limit, "limit", 50, "maximum Tasks to return (1-200)")
	return command
}

func (a *App) taskShowCommand() *cobra.Command {
	var compact bool
	var threadItemsLimit int
	command := &cobra.Command{
		Use: "show <task-number>", Short: "Show a Task with criteria, Threads, and delivery",
		Long: "Reads a work packet without claiming, reserving, or mutating the Task. --compact asks the server for bounded recent Thread context.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if !compact && command.Flags().Changed("thread-items-limit") {
				return &APIError{Code: "USAGE", Message: "--thread-items-limit requires --compact"}
			}
			if threadItemsLimit < 1 || threadItemsLimit > 100 {
				return &APIError{Code: "USAGE", Message: "--thread-items-limit must be between 1 and 100"}
			}
			number, err := parsePositive(args[0], "task-number")
			if err != nil {
				return err
			}
			var data map[string]any
			if compact {
				data, err = a.compactPacket(command, fmt.Sprintf("/api/v1/tasks/%d/work-packet?thread_items_limit=%d", number, threadItemsLimit))
			} else {
				data, err = a.taskPacket(command, number)
			}
			if err != nil {
				return err
			}
			return a.output(data, func(w io.Writer) { printTaskPacket(w, data) })
		},
	}
	command.Flags().BoolVar(&compact, "compact", false, "read a bounded server-aggregated work packet")
	command.Flags().IntVar(&threadItemsLimit, "thread-items-limit", 20, "recent Items per included Thread (1-100; requires --compact)")
	return command
}

func (a *App) compactPacket(command *cobra.Command, path string) (map[string]any, error) {
	body, _, err := a.client.request(command.Context(), http.MethodGet, path, nil, 0, "", false)
	if err != nil {
		return nil, err
	}
	var packet map[string]any
	if err := json.Unmarshal(body, &packet); err != nil {
		return nil, err
	}
	return packet, nil
}

func (a *App) taskClaimCommand() *cobra.Command {
	var version int64
	var stage string
	command := &cobra.Command{
		Use: "claim <task-number>", Short: "Claim an available execution or review Task",
		Long: `Creates one Claim for the Task version you inspected.

The response Claim ID is the explicit target for every later command. This
does not submit, review, or complete work. --stage is a local safety assertion;
the server derives the Claim stage from the authoritative Task state.`,
		Example: `pactline task claim 142 --stage execution --task-version 4
pactline task claim 142 --stage review --task-version 8`,
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			if stage != "execution" && stage != "review" {
				return &APIError{Code: "USAGE", Message: "--stage must be execution or review"}
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
			if stage == "review" && (inspected.Phase != "in_review" || inspected.Activity != "available") {
				return &APIError{
					Code: "USAGE", Message: "--stage review requires in_review.available",
					Hint: "Inspect the Task again and choose the stage matching its current state.",
				}
			}
			if stage == "execution" && inspected.Phase == "in_review" {
				return &APIError{
					Code: "USAGE", Message: "--stage execution does not match an in_review Task",
					Hint: "Use --stage review only when the Task is in_review.available.",
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
			if result.Claim.Stage != stage {
				return &APIError{Code: "UNEXPECTED_CLAIM_STAGE", Message: fmt.Sprintf("server returned an unexpected %s Claim after --stage %s", result.Claim.Stage, stage), Hint: fmt.Sprintf("Release Claim %s and inspect the Task state.", result.Claim.ID)}
			}
			return a.output(result, func(w io.Writer) {
				fmt.Fprintf(w, "Claim ID: %s\nTask: #%d\nStage: %s\nTask version: %d\n", result.Claim.ID, result.Task.TaskNumber, result.Claim.Stage, result.Task.Version)
			})
		},
	}
	command.Flags().StringVar(&stage, "stage", "execution", "expected Claim stage: execution or review")
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
	if _, compact := data["main_thread"]; compact {
		printCompactTaskPacket(w, data)
		return
	}
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

func printCompactTaskPacket(w io.Writer, data map[string]any) {
	task, _ := data["task"].(map[string]any)
	fmt.Fprintf(w, "Task: #%v\nTitle: %v\nVersion: %v\nState: %s\n", task["number"], task["title"], task["version"], lifecycle(stringValue(task["phase"]), stringValue(task["activity"])))
	fmt.Fprintf(w, "Context: %v\nExpected result: %v\n", task["context"], task["expected_result"])
	if description := stringValue(task["description"]); description != "" {
		fmt.Fprintf(w, "Description: %s\n", description)
	}
	criteria, _ := data["criteria"].([]any)
	fmt.Fprintf(w, "Acceptance criteria: %d\n", len(criteria))
	for _, raw := range criteria {
		item, _ := raw.(map[string]any)
		fmt.Fprintf(w, "  - %v (id=%v revision=%v)\n", item["criterion"], item["id"], item["revision"])
		fmt.Fprintf(w, "    Verify: %v\n", item["verification_instructions"])
		if check, ok := item["current_check"].(map[string]any); ok {
			fmt.Fprintf(w, "    Current check: %v — %v\n", check["outcome"], check["evidence"])
		}
	}
	printCompactThread := func(label string, value any) {
		thread, ok := value.(map[string]any)
		if !ok {
			return
		}
		items, _ := thread["items"].([]any)
		fmt.Fprintf(w, "%s: %v/%v items", label, thread["returned_count"], thread["total_count"])
		if truncated, _ := thread["truncated"].(bool); truncated {
			fmt.Fprint(w, " (truncated)")
		}
		fmt.Fprintln(w)
		for _, rawItem := range items {
			item, _ := rawItem.(map[string]any)
			fmt.Fprintf(w, "  [%v] %v\n", item["kind"], item["body"])
		}
	}
	printCompactThread("Main Thread", data["main_thread"])
	printCompactThread("Active Issue Thread", data["active_issue_thread"])
	fmt.Fprintf(w, "Resolved Issue Threads omitted: %v\n", data["resolved_issue_thread_count"])
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
