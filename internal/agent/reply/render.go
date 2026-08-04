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
		if selection.CreatedTask == nil || strings.TrimSpace(selection.Summary) == "" {
			return "", ErrInvalidResponseSelection
		}
		return r.taskCreated(runID, selection.Summary, *selection.CreatedTask), nil
	case agenttools.ResponseTaskDetail:
		if selection.TaskDetail == nil || strings.TrimSpace(selection.Summary) == "" {
			return "", ErrInvalidResponseSelection
		}
		return r.taskDetail(runID, selection.Summary, *selection.TaskDetail), nil
	case agenttools.ResponseProjectStatus:
		if selection.ProjectOverview == nil || strings.TrimSpace(selection.Summary) == "" {
			return "", ErrInvalidResponseSelection
		}
		return r.projectStatus(runID, selection.Summary, *selection.ProjectOverview), nil
	case agenttools.ResponseMilestoneStatus:
		if selection.MilestoneOverview == nil || strings.TrimSpace(selection.Summary) == "" {
			return "", ErrInvalidResponseSelection
		}
		return r.milestoneStatus(runID, selection.Summary, *selection.MilestoneOverview), nil
	case agenttools.ResponseConversationConfig:
		if selection.ConversationConfiguration == nil || strings.TrimSpace(selection.Summary) == "" {
			return "", ErrInvalidResponseSelection
		}
		return r.conversationConfiguration(runID, selection.Summary, *selection.ConversationConfiguration), nil
	case agenttools.ResponseError:
		if strings.TrimSpace(selection.Message) == "" {
			return "", ErrInvalidResponseSelection
		}
		return fmt.Sprintf(
			"# ⚠️ 请求未完成\n\n%s\n\n---\n`Run %s`",
			sanitizeModelMarkdown(selection.Message, 4_000),
			pactagent.ShortRunReference(runID),
		), nil
	case agenttools.ResponseGeneral:
		if strings.TrimSpace(selection.Message) == "" {
			return "", ErrInvalidResponseSelection
		}
		return fmt.Sprintf(
			"# 💬 Pactline Agent\n\n%s\n\n---\n`Run %s`",
			sanitizeModelMarkdown(selection.Message, 4_000),
			pactagent.ShortRunReference(runID),
		), nil
	default:
		return "", ErrInvalidResponseSelection
	}
}

func (r Renderer) conversationConfiguration(
	runID uuid.UUID,
	summary string,
	configuration agenttools.ConversationConfigurationResult,
) string {
	status := "已启用"
	if !configuration.Enabled {
		status = "已停用（只能从 Pactline Web 重新启用）"
	}
	project := "未绑定"
	if configuration.DefaultProject != nil {
		project = fmt.Sprintf("#%d %s", configuration.DefaultProject.Number, configuration.DefaultProject.Name)
		if !configuration.BindingActive {
			project += "（绑定已暂停）"
		}
	}
	background := strings.TrimSpace(configuration.BusinessContext)
	if background == "" {
		background = "未设置"
	}
	var body strings.Builder
	body.WriteString("# ⚙️ 本群 Agent 配置\n\n")
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"**Agent 状态**：%s\n**默认项目**：%s\n**业务背景**：\n\n%s\n\n---\n`配置版本 %d · Run %s`",
		inlineMarkdown(status),
		inlineMarkdown(project),
		sanitizeModelMarkdown(background, 4_000),
		configuration.Version,
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (r Renderer) Success(runID uuid.UUID, task agenttools.CreatedTask) string {
	return r.taskCreated(runID, "", task)
}

func (r Renderer) taskCreated(
	runID uuid.UUID,
	summary string,
	task agenttools.CreatedTask,
) string {
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
	var body strings.Builder
	fmt.Fprintf(
		&body,
		"# ✅ Task #%d 已创建 · %s\n\n",
		task.Number,
		inlineMarkdown(task.Title),
	)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"**项目**：%s\n**位置**：%s\n**负责人**：%s\n**截止日期**：%s\n**状态**：%s\n\n[在 Pactline 中打开 Task](%s)\n\n---\n`Run %s`",
		inlineMarkdown(task.ProjectName),
		inlineMarkdown(location),
		inlineMarkdown(assignee),
		inlineMarkdown(dueDate),
		inlineMarkdown(task.Status),
		taskURL,
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (r Renderer) taskDetail(
	runID uuid.UUID,
	summary string,
	task agenttools.TaskDetail,
) string {
	var body strings.Builder
	fmt.Fprintf(
		&body,
		"# 📋 Task #%d · %s\n\n",
		task.Number,
		inlineMarkdown(task.Title),
	)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"**状态**：%s\n**优先级**：%s\n**项目**：%s\n**位置**：%s\n**负责人**：%s\n**截止日期**：%s\n**阻塞**：%s",
		inlineMarkdown(task.Status),
		inlineMarkdown(task.Priority),
		inlineMarkdown(task.ProjectName),
		inlineMarkdown(valueOr(task.MilestoneName, "Backlog")),
		inlineMarkdown(valueOr(task.AssigneeName, "未指派")),
		inlineMarkdown(optionalString(task.DueDate, "未设置")),
		inlineMarkdown(boolLabel(task.Blocked)),
	)
	if task.Context != "" {
		fmt.Fprintf(
			&body,
			"\n\n**背景**\n\n%s",
			escapeMarkdown(truncateText(task.Context, 400)),
		)
	}
	if task.ExpectedResult != "" {
		fmt.Fprintf(
			&body,
			"\n\n**预期结果**\n\n%s",
			escapeMarkdown(truncateText(task.ExpectedResult, 400)),
		)
	}
	fmt.Fprintf(
		&body,
		"\n\n[在 Pactline 中打开 Task](%s)\n\n---\n`Run %s`",
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
	fmt.Fprintf(
		&body,
		"# 📊 Project #%d · %s\n\n",
		project.ProjectNumber,
		inlineMarkdown(project.ProjectName),
	)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"**任务概览**\n\n- 全部：**%d**\n- Todo：%d\n- 进行中：%d\n- 评审中：%d\n- 完成：%d\n- 已取消：%d\n- Backlog：%d\n- 逾期：%d\n- 阻塞：%d",
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
		body.WriteString("\n\n**Milestones**")
		for _, milestone := range project.Milestones {
			fmt.Fprintf(
				&body,
				"\n\n- **%s** · %s · 完成 %.0f%% · 任务 %d · 逾期 %d · 阻塞 %d",
				inlineMarkdown(milestone.Name),
				inlineMarkdown(milestone.Status),
				milestone.CompletionRatio*100,
				milestone.TaskCount,
				milestone.OverdueCount,
				milestone.BlockedCount,
			)
		}
		if project.MilestonesTruncated {
			body.WriteString("\n\n- _仅显示前 10 个 Milestone_")
		}
	}
	writeAttentionTasks(&body, project.AttentionTasks)
	fmt.Fprintf(
		&body,
		"\n\n[在 Pactline 中打开 Project](%s)\n\n---\n`Run %s`",
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
		"# 🎯 Milestone · %s\n\n**项目**：%s\n\n",
		inlineMarkdown(milestone.Name),
		inlineMarkdown(overview.ProjectName),
	)
	writeSummary(&body, summary)
	fmt.Fprintf(
		&body,
		"**状态**：%s\n\n**目标日期**：%s\n\n**完成度**：**%.0f%%**\n\n**任务概览**\n\n- 全部：%d\n- Todo：%d\n- 进行中：%d\n- 评审中：%d\n- 完成：%d\n- 已取消：%d\n- 逾期：%d\n- 阻塞：%d",
		inlineMarkdown(milestone.Status),
		inlineMarkdown(optionalString(milestone.TargetDate, "未设置")),
		milestone.CompletionRatio*100,
		milestone.TaskCount,
		milestone.StatusCounts.Todo,
		milestone.StatusCounts.InProgress,
		milestone.StatusCounts.InReview,
		milestone.StatusCounts.Done,
		milestone.StatusCounts.Cancelled,
		milestone.OverdueCount,
		milestone.BlockedCount,
	)
	if overview.Outcome != "" {
		fmt.Fprintf(
			&body,
			"\n\n**目标成果**\n\n%s",
			escapeMarkdown(truncateText(overview.Outcome, 400)),
		)
	}
	writeAttentionTasks(&body, overview.AttentionTasks)
	fmt.Fprintf(
		&body,
		"\n\n[在 Pactline 中打开 Milestone](%s)\n\n---\n`Run %s`",
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
		fmt.Fprintf(
			body,
			"**Agent 总结**\n\n%s\n\n",
			sanitizeModelMarkdown(summary, 1_000),
		)
	}
}

func writeAttentionTasks(body *strings.Builder, tasks []agenttools.TaskSummary) {
	if len(tasks) == 0 {
		return
	}
	body.WriteString("\n\n**需要关注**")
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
			"\n\n- **#%d %s**（%s）",
			task.Number,
			inlineMarkdown(task.Title),
			inlineMarkdown(strings.Join(reasons, "、")),
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
	body.WriteString("# ❓ 需要更多信息\n\n尚未执行请求。")
	if len(candidates) > 0 {
		body.WriteString("\n\n**可能的方向**")
		for _, candidate := range candidates {
			candidate = strings.TrimSpace(candidate)
			if candidate != "" {
				fmt.Fprintf(
					&body,
					"\n\n- %s",
					escapeMarkdown(truncateText(candidate, 200)),
				)
			}
		}
	}
	fmt.Fprintf(
		&body,
		"\n\n%s\n\n请直接回复此消息并补充信息。\n\n---\n`Run %s`",
		escapeMarkdown(truncateText(question, 500)),
		pactagent.ShortRunReference(runID),
	)
	return body.String()
}

func (Renderer) PermissionFailure(runID uuid.UUID, operation string) string {
	return fmt.Sprintf(
		"# ⛔ 无法完成请求\n\n当前用户无权执行 **%s**。\n\n---\n`Run %s`",
		inlineMarkdown(operation),
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Failure(runID uuid.UUID, reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		reason = "执行过程中发生内部错误。"
	}
	return fmt.Sprintf(
		"# ⚠️ 请求未完成\n\n**原因**：%s\n\n请重试；如果问题持续出现，请向管理员提供下方 Run 编号。\n\n---\n`Run %s`",
		inlineMarkdown(reason),
		pactagent.ShortRunReference(runID),
	)
}

func (Renderer) Expired(runID uuid.UUID) string {
	return fmt.Sprintf(
		"# ⌛ 请求已过期\n\n澄清请求已超过 24 小时，请重新 @Pactline 发起请求。\n\n---\n`Run %s`",
		pactagent.ShortRunReference(runID),
	)
}

func inlineMarkdown(value string) string {
	value = strings.Join(strings.Fields(strings.TrimSpace(value)), " ")
	return escapeMarkdown(value)
}

func escapeMarkdown(value string) string {
	value = strings.ReplaceAll(value, "\x00", "")
	return strings.NewReplacer(
		`\`, `\\`,
		"`", "\\`",
		"*", `\*`,
		"_", `\_`,
		"~", `\~`,
		"[", `\[`,
		"]", `\]`,
		"(", `\(`,
		")", `\)`,
		"#", `\#`,
		"-", `\-`,
		"+", `\+`,
		"!", `\!`,
		"|", `\|`,
		"{", `\{`,
		"}", `\}`,
		"<", "&lt;",
		">", "&gt;",
	).Replace(value)
}

func sanitizeModelMarkdown(value string, limit int) string {
	value = truncateText(strings.ReplaceAll(value, "\x00", ""), limit)
	return strings.NewReplacer("<", "&lt;", ">", "&gt;").Replace(value)
}
