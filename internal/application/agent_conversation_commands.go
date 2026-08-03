package application

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	pactagent "github.com/wolfhead/pactline/internal/agent"
	"github.com/wolfhead/pactline/internal/domain"
	"github.com/wolfhead/pactline/internal/store"
)

type AgentConversationCommandService struct {
	Conversations *AgentConversationService
	Users         *store.UserStore
}

func (s *AgentConversationCommandService) Execute(
	ctx context.Context,
	run pactagent.Run,
	command string,
) (string, error) {
	user, err := s.Users.GetByID(ctx, run.InitiatingUserID)
	if err != nil {
		return "", err
	}
	subject := domain.ProjectAccessSubject{UserID: user.ID, PlatformRole: user.PlatformRole}
	snapshot, err := s.Conversations.Conversations.GetByExternal(
		ctx, run.Provider, run.TenantID, run.ConversationID,
	)
	if err != nil {
		return "", err
	}
	command = strings.TrimSpace(command)
	switch strings.ToLower(command) {
	case "本群配置", "查看本群配置":
		visible, err := s.Conversations.Get(ctx, snapshot.Conversation.ID, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return renderAgentConversationConfiguration(visible), nil
	case "清除本群背景":
		empty := ""
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{BusinessContext: &empty}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群业务背景已清除。\n\n" + renderAgentConversationConfiguration(updated), nil
	case "解除本群项目绑定":
		active := false
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{BindingActive: &active}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群默认项目绑定已解除；原有业务背景会保留。\n\n" + renderAgentConversationConfiguration(updated), nil
	case "启用本群agent":
		enabled := true
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{Enabled: &enabled}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群 Agent 已启用。\n\n" + renderAgentConversationConfiguration(updated), nil
	case "停用本群agent":
		enabled := false
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{Enabled: &enabled}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群 Agent 已停用；配置命令仍然可用。\n\n" + renderAgentConversationConfiguration(updated), nil
	}
	if contextValue, ok := configurationCommandValue(command, "设置本群背景:", "设置本群背景："); ok {
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{BusinessContext: &contextValue}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群业务背景已更新。\n\n" + renderAgentConversationConfiguration(updated), nil
	}
	if reference, ok := configurationCommandValue(command, "将本群绑定到", "绑定本群到", "绑定项目"); ok {
		projectNumber, resolutionMessage, err := s.resolveProject(ctx, subject, reference)
		if err != nil {
			return "", err
		}
		if resolutionMessage != "" {
			return resolutionMessage, nil
		}
		updated, err := s.Conversations.Update(ctx, snapshot.Conversation.ID, snapshot.Conversation.Version,
			AgentConversationUpdate{DefaultProjectNumber: &projectNumber, DefaultProjectSet: true}, subject)
		if err != nil {
			return expectedConversationCommandError(err)
		}
		return "本群默认项目已绑定。\n\n" + renderAgentConversationConfiguration(updated), nil
	}
	return "无法识别群配置命令。可使用：查看本群配置、绑定项目、设置本群背景、清除本群背景、解除本群项目绑定、启用本群Agent或停用本群Agent。", nil
}

func (s *AgentConversationCommandService) resolveProject(
	ctx context.Context,
	subject domain.ProjectAccessSubject,
	reference string,
) (int64, string, error) {
	reference = strings.Trim(strings.TrimSpace(reference), "「」『』\"' ")
	projects, err := s.Conversations.Projects.ListForSubject(ctx, false, subject)
	if err != nil {
		return 0, "", err
	}
	if number, parseErr := strconv.ParseInt(strings.TrimPrefix(reference, "#"), 10, 64); parseErr == nil {
		for _, project := range projects {
			if project.Project.Number == number {
				return number, "", nil
			}
		}
		return 0, "没有找到你可管理的对应项目。", nil
	}
	var exact, partial []store.ProjectWithRelations
	for _, project := range projects {
		if strings.EqualFold(project.Project.Name, reference) {
			exact = append(exact, project)
		} else if strings.Contains(strings.ToLower(project.Project.Name), strings.ToLower(reference)) {
			partial = append(partial, project)
		}
	}
	candidates := exact
	if len(candidates) == 0 {
		candidates = partial
	}
	if len(candidates) == 1 {
		return candidates[0].Project.Number, "", nil
	}
	if len(candidates) == 0 {
		return 0, "没有找到匹配的可访问项目。请使用完整项目名或项目编号。", nil
	}
	labels := make([]string, 0, len(candidates))
	for _, project := range candidates {
		labels = append(labels, fmt.Sprintf("#%d %s", project.Project.Number, project.Project.Name))
	}
	return 0, "匹配到多个项目，请使用项目编号：\n\n- " + strings.Join(labels, "\n- "), nil
}

func configurationCommandValue(command string, prefixes ...string) (string, bool) {
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(command), strings.ToLower(prefix)) {
			value := strings.TrimSpace(command[len(prefix):])
			return value, value != ""
		}
	}
	return "", false
}

func expectedConversationCommandError(err error) (string, error) {
	switch {
	case errors.Is(err, domain.ErrForbidden):
		return "只有项目管理员可以修改本群 Agent 配置。", nil
	case errors.Is(err, domain.ErrNotFound):
		return "当前配置或项目不存在，或者你没有访问权限。", nil
	case errors.Is(err, domain.ErrConflict):
		return "配置已被其他人更新，请查看本群配置后重试。", nil
	case errors.Is(err, domain.ErrInvalidInput):
		return "配置未修改。请确认项目未归档、背景不超过 4000 字，并重试。", nil
	default:
		return "", err
	}
}

func renderAgentConversationConfiguration(snapshot store.AgentConversationSnapshot) string {
	conversation := snapshot.Conversation
	project := "未绑定"
	if conversation.BindingActive && snapshot.Project != nil && snapshot.Project.ArchivedAt == nil {
		project = fmt.Sprintf("#%d %s", snapshot.Project.Number, snapshot.Project.Name)
	} else if conversation.BindingActive && snapshot.Project != nil {
		project = fmt.Sprintf("#%d %s（已归档，绑定已失效）", snapshot.Project.Number, snapshot.Project.Name)
	}
	background := strings.TrimSpace(conversation.BusinessContext)
	if background == "" {
		background = "未设置"
	}
	enabled := "已启用"
	if !conversation.Enabled {
		enabled = "已停用"
	}
	return fmt.Sprintf(
		"**Agent 状态**：%s\n**默认项目**：%s\n**群业务背景**：\n\n%s\n\n`配置版本 %d`",
		enabled, project, background, conversation.Version,
	)
}
