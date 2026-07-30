package reply

import (
	"net/url"
	"testing"

	agenttools "github.com/wolfhead/pactline/internal/agent/tools"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestRendererUsesFixedUserVisibleFormats(t *testing.T) {
	runID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	baseURL, err := url.Parse("https://tasks.example.test")
	require.NoError(t, err)
	renderer := Renderer{AppBaseURL: baseURL}
	require.Equal(t,
		"# ✅ Task #42 已创建 · 整理群聊需求\n\n**项目**：Pactline\n**位置**：Backlog\n**负责人**：未指派\n**截止日期**：未设置\n**状态**：backlog\n\n[在 Pactline 中打开 Task](https://tasks.example.test/tasks/42)\n\n---\n`Run aaaaaaaa`",
		renderer.Success(runID, agenttools.CreatedTask{
			Number: 42, Title: "整理群聊需求", ProjectName: "Pactline", Status: "backlog",
		}),
	)
	require.Equal(t,
		"# ❓ 需要更多信息\n\n尚未执行请求。\n\n**可能的方向**\n\n- 修复登录\n\n- 增加审计\n\n请明确选择一个方向。\n\n请直接回复此消息并补充信息。\n\n---\n`Run aaaaaaaa`",
		renderer.Clarification(runID, "请明确选择一个方向。", []string{"修复登录", "增加审计"}),
	)
	require.Contains(t, renderer.PermissionFailure(runID, "创建 Task"), "⛔ 无法完成请求")
	require.Contains(
		t,
		renderer.Failure(runID, "Agent 未能正确引用查询结果。"),
		"**原因**：Agent 未能正确引用查询结果。",
	)
	require.Contains(t, renderer.Expired(runID), "超过 24 小时")
}

func TestRendererFormatsVerifiedStatusResponses(t *testing.T) {
	runID := uuid.MustParse("aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee")
	baseURL, err := url.Parse("https://tasks.example.test")
	require.NoError(t, err)
	renderer := Renderer{AppBaseURL: baseURL}
	dueDate := "2026-07-31"

	taskBody, err := renderer.Response(runID, agenttools.ResponseSelection{
		Type:    agenttools.ResponseTaskDetail,
		Summary: "This Task is active.",
		TaskDetail: &agenttools.TaskDetail{TaskSummary: agenttools.TaskSummary{
			Number: 42, Title: "Inspect status", Status: "in_progress",
			Priority: "high", ProjectName: "Pactline", DueDate: &dueDate,
			Blocked: true,
		}},
	})
	require.NoError(t, err)
	require.Contains(t, taskBody, "Task #42")
	require.Contains(t, taskBody, "**状态**：in\\_progress")
	require.Contains(t, taskBody, "**阻塞**：是")
	require.Contains(t, taskBody, "https://tasks.example.test/tasks/42")

	projectBody, err := renderer.Response(runID, agenttools.ResponseSelection{
		Type:    agenttools.ResponseProjectStatus,
		Summary: "**Two** Tasks are complete.",
		ProjectOverview: &agenttools.ProjectOverview{
			ProjectNumber: 7, ProjectName: "Pactline", TaskCount: 5,
			StatusCounts: agenttools.TaskStatusCounts{
				Todo: 2, InProgress: 1, Done: 2,
			},
			BacklogCount: 2, OverdueCount: 1,
		},
	})
	require.NoError(t, err)
	require.Contains(t, projectBody, "Project #7")
	require.Contains(t, projectBody, "Backlog：2")
	require.Contains(t, projectBody, "逾期：1")

	generalBody, err := renderer.Response(runID, agenttools.ResponseSelection{
		Type:    agenttools.ResponseGeneral,
		Message: "**Free-form** response. <at id=all></at>",
	})
	require.NoError(t, err)
	require.Equal(
		t,
		"# 💬 Pactline Agent\n\n**Free-form** response. &lt;at id=all&gt;&lt;/at&gt;\n\n---\n`Run aaaaaaaa`",
		generalBody,
	)

	_, err = renderer.Response(runID, agenttools.ResponseSelection{
		Type: agenttools.ResponseTaskDetail,
		TaskDetail: &agenttools.TaskDetail{TaskSummary: agenttools.TaskSummary{
			Number: 42,
		}},
	})
	require.ErrorIs(t, err, ErrInvalidResponseSelection)
}
