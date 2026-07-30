// Package reply renders all user-visible first-party Agent messages. Model
// final text is never sent directly to a channel.
package reply

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/google/uuid"
)

var ErrInvalidResponseSelection = errors.New("agent response selection is invalid")

type Renderer struct {
	AppBaseURL *url.URL
}

func (r Renderer) Response(
	runID uuid.UUID,
	selection agenttools.ResponseSelection,
) (string, error) {
	switch selection.Type {
	case agenttools.ResponseTaskCreated:
		if selection.CreatedTask == nil {
			return "", ErrInvalidResponseSelection
		}
		return r.Success(runID, *selection.CreatedTask), nil
	case agenttools.ResponseTaskDetail:
		if selection.TaskDetail == nil {
			return "", ErrInvalidResponseSelection
		}
		return r.taskDetail(runID, selection.Summary, *selection.TaskDetail), nil
	case agenttools.ResponseProjectStatus:
		if selection.ProjectOverview == nil {
			return "", ErrInvalidResponseSelection
		}
		return r.projectStatus(runID, selection.Summary, *selection.ProjectOverview), nil
	case agenttools.ResponseMilestoneStatus:
		if selection.MilestoneOverview == nil {
			return "", ErrInvalidResponseSelection
		}
		return r.milestoneStatus(runID, selection.Summary, *selection.MilestoneOverview), nil
	case agenttools.ResponseError:
		if strings.TrimSpace(selection.Message) == "" {
			return "", ErrInvalidResponseSelection
		}
		return fmt.Sprintf(
			"⚠️ %s\nRun：%s",
			strings.TrimSpace(selection.Message),
			pactagent.ShortRunReference(runID),
		), nil
	case agenttools.ResponseGeneral:
		if strings.TrimSpace(selection.Message) == "" {
			return "", ErrInvalidResponseSelection
		}
		return fmt.Sprintf(
			"%s\nRun：%s",
			strings.TrimSpace(selection.Message),
			pactagent.ShortRunReference(runID),
		), nil
	default:
		return "", ErrInvalidResponseSelection
	}
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

func (r Renderer) taskDetail(
	runID uuid.UUID,
	summary string,
	task agenttools.TaskDetail,
) string {
	var body strings.Builder
	fmt.Fprintf(&body, "📋 Task #%d：%s\n", task.Number, task.Title)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"状态：%s\n优先级：%s\n项目：%s\n位置：%s\n负责人：%s\n截止日期：%s\n阻塞：%s",
		task.Status,
		task.Priority,
		task.ProjectName,
		valueOr(task.MilestoneName, "Backlog"),
		valueOr(task.AssigneeName, "未指派"),
		optionalString(task.DueDate, "未设置"),
		boolLabel(task.Blocked),
	)
	if task.Context != "" {
		fmt.Fprintf(&body, "\n背景：%s", truncateText(task.Context, 400))
	}
	if task.ExpectedResult != "" {
		fmt.Fprintf(&body, "\n预期结果：%s", truncateText(task.ExpectedResult, 400))
	}
	fmt.Fprintf(
		&body,
		"\n链接：%s\nRun：%s",
		r.entityURL(fmt.Sprintf("/tasks/%d", task.Number)),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (r Renderer) projectStatus(
	runID uuid.UUID,
	summary string,
	project agenttools.ProjectOverview,
) string {
	var body strings.Builder
	fmt.Fprintf(&body, "📊 Project #%d：%s\n", project.ProjectNumber, project.ProjectName)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"任务：%d（todo %d / 进行中 %d / 评审中 %d / 完成 %d / 已取消 %d）\nBacklog：%d\n逾期：%d\n阻塞：%d",
		project.TaskCount,
		project.StatusCounts.Todo,
		project.StatusCounts.InProgress,
		project.StatusCounts.InReview,
		project.StatusCounts.Done,
		project.StatusCounts.Cancelled,
		project.BacklogCount,
		project.OverdueCount,
		project.BlockedCount,
	)
	if len(project.Milestones) > 0 {
		body.WriteString("\nMilestones：")
		for _, milestone := range project.Milestones {
			fmt.Fprintf(
				&body,
				"\n• %s：%s，完成 %.0f%%，任务 %d，逾期 %d，阻塞 %d",
				milestone.Name,
				milestone.Status,
				milestone.CompletionRatio*100,
				milestone.TaskCount,
				milestone.OverdueCount,
				milestone.BlockedCount,
			)
		}
		if project.MilestonesTruncated {
			body.WriteString("\n• 仅显示前 10 个 Milestone")
		}
	}
	writeAttentionTasks(&body, project.AttentionTasks)
	fmt.Fprintf(
		&body,
		"\n链接：%s\nRun：%s",
		r.entityURL(fmt.Sprintf("/projects/%d", project.ProjectNumber)),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (r Renderer) milestoneStatus(
	runID uuid.UUID,
	summary string,
	overview agenttools.MilestoneOverview,
) string {
	milestone := overview.Milestone
	var body strings.Builder
	fmt.Fprintf(
		&body,
		"🎯 Milestone：%s\n项目：%s\n",
		milestone.Name,
		overview.ProjectName,
	)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"状态：%s\n目标日期：%s\n任务：%d（todo %d / 进行中 %d / 评审中 %d / 完成 %d / 已取消 %d）\n完成度：%.0f%%\n逾期：%d\n阻塞：%d",
		milestone.Status,
		optionalString(milestone.TargetDate, "未设置"),
		milestone.TaskCount,
		milestone.StatusCounts.Todo,
		milestone.StatusCounts.InProgress,
		milestone.StatusCounts.InReview,
		milestone.StatusCounts.Done,
		milestone.StatusCounts.Cancelled,
		milestone.CompletionRatio*100,
		milestone.OverdueCount,
		milestone.BlockedCount,
	)
	if overview.Outcome != "" {
		fmt.Fprintf(&body, "\n目标成果：%s", truncateText(overview.Outcome, 400))
	}
	writeAttentionTasks(&body, overview.AttentionTasks)
	fmt.Fprintf(
		&body,
		"\n链接：%s\nRun：%s",
		r.entityURL(fmt.Sprintf(
			"/projects/%d/milestones/%s",
			overview.ProjectNumber,
			milestone.ID,
		)),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (r Renderer) entityURL(path string) string {
	if r.AppBaseURL == nil {
		return path
	}
	return strings.TrimRight(r.AppBaseURL.String(), "/") + path
}

func writeSummary(body *strings.Builder, summary string) {
	if summary = strings.TrimSpace(summary); summary != "" {
		fmt.Fprintf(body, "Agent 总结：%s\n", truncateText(summary, 1_000))
	}
}

func writeAttentionTasks(body *strings.Builder, tasks []agenttools.TaskSummary) {
	if len(tasks) == 0 {
		return
	}
	body.WriteString("\n需要关注：")
	for _, task := range tasks {
		var reasons []string
		if task.Overdue {
			reasons = append(reasons, "逾期")
		}
		if task.Blocked {
			reasons = append(reasons, "阻塞")
		}
		fmt.Fprintf(
			body,
			"\n• #%d %s（%s）",
			task.Number,
			task.Title,
			strings.Join(reasons, "、"),
		)
	}
}

func optionalString(value *string, fallback string) string {
	if value == nil || strings.TrimSpace(*value) == "" {
		return fallback
	}
	return strings.TrimSpace(*value)
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}

func boolLabel(value bool) string {
	if value {
		return "是"
	}
	return "否"
}

func truncateText(value string, limit int) string {
	value = strings.TrimSpace(value)
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit]) + "…"
}

func (Renderer) Clarification(runID uuid.UUID, question string, candidates []string) string {
	var body strings.Builder
	body.WriteString("❓ 需要更多信息，尚未执行请求。\n")
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
		"%s\n请直接回复此消息并补充信息。\nRun：%s",
		strings.TrimSpace(question),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (Renderer) PermissionFailure(runID uuid.UUID, operation string) string {
	return fmt.Sprintf(
		"⛔ 无法完成请求：当前用户无权执行 %s。\nRun：%s",
		strings.TrimSpace(operation),
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Failure(runID uuid.UUID) string {
	return fmt.Sprintf(
		"⚠️ 本次请求无法完成，请稍后重试。\nRun：%s",
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Expired(runID uuid.UUID) string {
	return fmt.Sprintf(
		"⌛ 澄清请求已超过 24 小时，请重新 @Pactline 发起请求。\nRun：%s",
		pactagent.ShortRunReference(runID),
	)
}
