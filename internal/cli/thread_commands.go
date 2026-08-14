package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/spf13/cobra"
)

type actorSummary struct {
	Type   string `json:"type"`
	UserID string `json:"user_id,omitempty"`
	Ref    string `json:"ref,omitempty"`
}

type taskThread struct {
	ID              string        `json:"id"`
	TaskID          string        `json:"task_id"`
	Role            string        `json:"role"`
	IssueType       string        `json:"issue_type,omitempty"`
	IssueStatus     string        `json:"issue_status,omitempty"`
	OpenedFromPhase string        `json:"opened_from_phase,omitempty"`
	OpenedBy        *actorSummary `json:"opened_by,omitempty"`
	ResolvedBy      *actorSummary `json:"resolved_by,omitempty"`
	Version         int64         `json:"version"`
	CreatedAt       string        `json:"created_at"`
	UpdatedAt       string        `json:"updated_at"`
	ResolvedAt      string        `json:"resolved_at,omitempty"`
}

type threadItem struct {
	ID               string          `json:"id"`
	ThreadID         string          `json:"thread_id"`
	Kind             string          `json:"kind"`
	Author           actorSummary    `json:"author"`
	Body             string          `json:"body,omitempty"`
	IssueResolution  json.RawMessage `json:"issue_resolution,omitempty"`
	TaskStageClaimID string          `json:"task_stage_claim_id,omitempty"`
	TaskReviewCycle  int64           `json:"task_review_cycle,omitempty"`
	ReplyToItemID    string          `json:"reply_to_item_id,omitempty"`
	MentionedUserIDs []string        `json:"mentioned_user_ids"`
	Version          int64           `json:"version"`
	CreatedAt        string          `json:"created_at"`
	UpdatedAt        string          `json:"updated_at"`
	DeletedAt        string          `json:"deleted_at,omitempty"`
}

type issueResolutionSummary struct {
	IssueType  string `json:"issue_type"`
	Request    string `json:"request"`
	Resolution string `json:"resolution"`
}

type threadPage struct {
	Items      []threadItem `json:"items"`
	NextCursor string       `json:"next_cursor,omitempty"`
}

func (a *App) taskThreadsCommand() *cobra.Command {
	return &cobra.Command{
		Use: "threads <task-number>", Short: "List the Main Thread and Issue Threads for one Task",
		Long: "Lists durable Thread identities and versions. Use thread items with an explicit Thread ID to inspect one history.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			number, err := positiveInt64(args[0], "task-number")
			if err != nil {
				return err
			}
			body, _, err := a.client.request(command.Context(), http.MethodGet, fmt.Sprintf("/api/v1/tasks/%d/threads", number), nil, 0, "", false)
			if err != nil {
				return err
			}
			var page struct {
				Items []taskThread `json:"items"`
			}
			if err := json.Unmarshal(body, &page); err != nil {
				return err
			}
			return a.output(page, func(w io.Writer) {
				if len(page.Items) == 0 {
					fmt.Fprintln(w, "No Threads.")
					return
				}
				for _, thread := range page.Items {
					printThread(w, thread)
				}
			})
		},
	}
}

func (a *App) threadCommand() *cobra.Command {
	command := &cobra.Command{Use: "thread", Short: "Read and write explicit Task Threads"}
	command.AddCommand(
		a.threadItemsCommand(), a.threadPostCommand(), a.threadEditCommand(), a.threadDeleteCommand(),
	)
	return command
}

func (a *App) threadItemsCommand() *cobra.Command {
	var limit int
	var cursor string
	command := &cobra.Command{
		Use: "items <thread-id>", Short: "Read one bounded page of Thread Items",
		Long: "Reads exactly one server page. Use the returned next_cursor explicitly for deeper history.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			threadID, err := parseUUID(args[0], "thread-id")
			if err != nil {
				return err
			}
			if limit < 1 || limit > 200 {
				return &APIError{Code: "USAGE", Message: "--limit must be between 1 and 200"}
			}
			query := url.Values{"limit": {strconv.Itoa(limit)}}
			if cursor != "" {
				query.Set("cursor", cursor)
			}
			body, _, err := a.client.request(command.Context(), http.MethodGet, "/api/v1/threads/"+threadID+"/items?"+query.Encode(), nil, 0, "", false)
			if err != nil {
				return err
			}
			var page threadPage
			if err := json.Unmarshal(body, &page); err != nil {
				return err
			}
			return a.output(page, func(w io.Writer) {
				if len(page.Items) == 0 {
					fmt.Fprintln(w, "No Thread Items.")
				} else {
					for _, item := range page.Items {
						printThreadItem(w, item)
					}
				}
				if page.NextCursor != "" {
					fmt.Fprintf(w, "Next cursor: %s\n", page.NextCursor)
				}
			})
		},
	}
	command.Flags().IntVar(&limit, "limit", 50, "Items in this server page (1-200)")
	command.Flags().StringVar(&cursor, "cursor", "", "opaque next_cursor from the previous page")
	return command
}

func (a *App) threadPostCommand() *cobra.Command {
	var message, file, replyTo string
	var mentions []string
	command := &cobra.Command{
		Use: "post <thread-id>", Short: "Post an ordinary message to one explicit Thread",
		Long: "Posts a message without changing Task lifecycle. Replies and mentions use explicit UUIDs; the CLI never parses names from message text.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			threadID, err := parseUUID(args[0], "thread-id")
			if err != nil {
				return err
			}
			body, err := contentWithFlags(message, file, a.stdin, "message", "message", "file")
			if err != nil {
				return err
			}
			payload := map[string]any{"kind": "message", "body": body, "mentioned_user_ids": []string{}}
			if replyTo != "" {
				value, err := parseUUID(replyTo, "reply-to")
				if err != nil {
					return err
				}
				payload["reply_to_item_id"] = value
			}
			validatedMentions, err := uniqueUUIDs(mentions, "mention")
			if err != nil {
				return err
			}
			payload["mentioned_user_ids"] = validatedMentions
			response, _, err := a.client.request(command.Context(), http.MethodPost, "/api/v1/threads/"+threadID+"/items", payload, 0, a.idempotencyKey, true)
			if err != nil {
				return err
			}
			return a.outputThreadItem(response, "Thread message posted")
		},
	}
	command.Flags().StringVar(&message, "message", "", "inline message text")
	command.Flags().StringVar(&file, "file", "", "read message from file; use - for stdin")
	command.Flags().StringVar(&replyTo, "reply-to", "", "Thread Item UUID being replied to")
	command.Flags().StringSliceVar(&mentions, "mention", nil, "mentioned user UUID; repeat for several users")
	return command
}

func (a *App) threadEditCommand() *cobra.Command {
	var version int64
	var message, file string
	var mentions []string
	command := &cobra.Command{
		Use: "edit <item-id>", Short: "Replace a message you own",
		Long: "Replaces the body and complete mention set of an editable message. Omitting --mention clears mentions.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			itemID, err := parseUUID(args[0], "item-id")
			if err != nil {
				return err
			}
			if err := requiredPositive("item-version", version); err != nil {
				return err
			}
			body, err := contentWithFlags(message, file, a.stdin, "message", "message", "file")
			if err != nil {
				return err
			}
			validatedMentions, err := uniqueUUIDs(mentions, "mention")
			if err != nil {
				return err
			}
			response, _, err := a.client.request(command.Context(), http.MethodPatch, "/api/v1/thread-items/"+itemID, map[string]any{
				"body": body, "mentioned_user_ids": validatedMentions,
			}, version, a.idempotencyKey, true)
			if err != nil {
				return err
			}
			return a.outputThreadItem(response, "Thread message updated")
		},
	}
	command.Flags().Int64Var(&version, "item-version", 0, "Thread Item version previously inspected (required)")
	command.Flags().StringVar(&message, "message", "", "inline replacement message")
	command.Flags().StringVar(&file, "file", "", "read replacement message from file; use - for stdin")
	command.Flags().StringSliceVar(&mentions, "mention", nil, "complete mentioned-user UUID set; repeat for several users")
	return command
}

func (a *App) threadDeleteCommand() *cobra.Command {
	var version int64
	command := &cobra.Command{
		Use: "delete <item-id>", Short: "Tombstone a message you own",
		Long: "Deletes an editable message body while preserving its durable Thread position and audit history.",
		Args: cobra.ExactArgs(1), RunE: func(command *cobra.Command, args []string) error {
			if err := a.requireMutationProvenance(); err != nil {
				return err
			}
			itemID, err := parseUUID(args[0], "item-id")
			if err != nil {
				return err
			}
			if err := requiredPositive("item-version", version); err != nil {
				return err
			}
			response, _, err := a.client.request(command.Context(), http.MethodDelete, "/api/v1/thread-items/"+itemID, nil, version, a.idempotencyKey, true)
			if err != nil {
				return err
			}
			return a.outputThreadItem(response, "Thread message deleted")
		},
	}
	command.Flags().Int64Var(&version, "item-version", 0, "Thread Item version previously inspected (required)")
	return command
}

func (a *App) outputThreadItem(response json.RawMessage, message string) error {
	var item threadItem
	if err := json.Unmarshal(response, &item); err != nil {
		return err
	}
	return a.output(item, func(w io.Writer) {
		fmt.Fprintln(w, message)
		printThreadItem(w, item)
	})
}

func printThread(w io.Writer, thread taskThread) {
	if thread.Role == "main" {
		fmt.Fprintf(w, "Main Thread  %s  version %d\n", thread.ID, thread.Version)
		return
	}
	fmt.Fprintf(w, "Issue Thread  %s  %s  %s  version %d\n", thread.ID, thread.IssueType, thread.IssueStatus, thread.Version)
}

func printThreadItem(w io.Writer, item threadItem) {
	body := item.Body
	if item.DeletedAt != "" {
		body = "[deleted]"
	} else if body == "" && len(item.IssueResolution) > 0 {
		var summary issueResolutionSummary
		if err := json.Unmarshal(item.IssueResolution, &summary); err == nil {
			body = fmt.Sprintf("Issue resolved (%s)\nRequest: %s\nResolution: %s", summary.IssueType, summary.Request, summary.Resolution)
		} else {
			body = "[structured issue resolution]"
		}
	}
	fmt.Fprintf(w, "%s  %s  %s  version %d", item.ID, item.Kind, actorLabel(item.Author), item.Version)
	if item.ReplyToItemID != "" {
		fmt.Fprintf(w, "  reply-to %s", item.ReplyToItemID)
	}
	fmt.Fprintln(w)
	if body != "" {
		fmt.Fprintln(w, body)
	}
}

func actorLabel(actor actorSummary) string {
	if actor.Ref != "" {
		return actor.Type + "/" + actor.Ref
	}
	if actor.UserID != "" {
		return actor.Type + "/" + actor.UserID
	}
	return actor.Type
}

func uniqueUUIDs(values []string, label string) ([]string, error) {
	result := make([]string, 0, len(values))
	seen := make(map[string]bool, len(values))
	for _, raw := range values {
		for _, candidate := range strings.Split(raw, ",") {
			value, err := parseUUID(strings.TrimSpace(candidate), label)
			if err != nil {
				return nil, err
			}
			if !seen[value] {
				seen[value] = true
				result = append(result, value)
			}
		}
	}
	return result, nil
}

func positiveInt64(raw, label string) (int64, error) {
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || value < 1 {
		return 0, &APIError{Code: "USAGE", Message: label + " must be a positive integer"}
	}
	return value, nil
}
