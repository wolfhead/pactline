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
		"✅ 已创建 Task #42：整理群聊需求\n项目：Pactline\n位置：Backlog\n负责人：未指派\n截止日期：未设置\n状态：backlog\n链接：https://tasks.example.test/tasks/42\nRun：aaaaaaaa",
		renderer.Success(runID, agenttools.CreatedTask{
			Number: 42, Title: "整理群聊需求", ProjectName: "Pactline", Status: "backlog",
		}),
	)
	require.Equal(t,
		"❓ 尚未创建 Task。\n可能的方向：\n• 修复登录\n• 增加审计\n请明确选择一个方向。\n请直接回复此消息，并明确描述一个具体 Task。\nRun：aaaaaaaa",
		renderer.Clarification(runID, "请明确选择一个方向。", []string{"修复登录", "增加审计"}),
	)
	require.Contains(t, renderer.PermissionFailure(runID, "创建 Task"), "⛔ 未创建 Task")
	require.Contains(t, renderer.Failure(runID), "⚠️ 未创建 Task")
	require.Contains(t, renderer.Expired(runID), "超过 24 小时")
}
