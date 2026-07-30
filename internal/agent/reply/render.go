// Package reply renders all user-visible first-party Agent messages. Model
// final text is never sent directly to a channel.
package reply

import (
	"fmt"
	"net/url"
	"strings"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/google/uuid"
)

type Renderer struct {
	AppBaseURL *url.URL
}

func (r Renderer) Success(runID uuid.UUID, task agenttools.CreatedTask) string {
	location := "Backlog"
	if task.MilestoneName != "" {
		location = task.MilestoneName
	}
	assignee := "未指派"
	if task.AssigneeName != "" {
		assignee = task.AssigneeName
	}
	dueDate := "未设置"
	if task.DueDate != nil {
		dueDate = *task.DueDate
	}
	taskURL := fmt.Sprintf("/tasks/%d", task.Number)
	if r.AppBaseURL != nil {
		taskURL = strings.TrimRight(r.AppBaseURL.String(), "/") + taskURL
	}
	return fmt.Sprintf(
		"✅ 已创建 Task #%d：%s\n项目：%s\n位置：%s\n负责人：%s\n截止日期：%s\n状态：%s\n链接：%s\nRun：%s",
		task.Number,
		task.Title,
		task.ProjectName,
		location,
		assignee,
		dueDate,
		task.Status,
		taskURL,
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Clarification(runID uuid.UUID, question string, candidates []string) string {
	var body strings.Builder
	body.WriteString("❓ 尚未创建 Task。\n")
	if len(candidates) > 0 {
		body.WriteString("可能的方向：\n")
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" {
				fmt.Fprintf(&body, "• %s\n", candidate)
			}
		}
	}
	fmt.Fprintf(
		&body,
		"%s\n请直接回复此消息，并明确描述一个具体 Task。\nRun：%s",
		strings.TrimSpace(question),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (Renderer) PermissionFailure(runID uuid.UUID, operation string) string {
	return fmt.Sprintf(
		"⛔ 未创建 Task：当前用户无权执行 %s。\nRun：%s",
		strings.TrimSpace(operation),
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Failure(runID uuid.UUID) string {
	return fmt.Sprintf(
		"⚠️ 未创建 Task：本次请求无法完成，请稍后重试。\nRun：%s",
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Expired(runID uuid.UUID) string {
	return fmt.Sprintf(
		"⌛ 未创建 Task：澄清请求已超过 24 小时，请重新 @Pactline 发起请求。\nRun：%s",
		pactagent.ShortRunReference(runID),
	)
}
