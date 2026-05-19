// Slash-command execution: builds CommandDeps from services + env state,
// runs commands through input.NewCommandController.
package app

import (
	"context"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/genai-io/gen-code/internal/app/input"
	"github.com/genai-io/gen-code/internal/session"
)

func (m *model) commandDeps() input.CommandDeps {
	return input.CommandDeps{
		Input:        &m.userInput,
		Conversation: &m.conv.ConversationModel,
		Tool:         &m.conv.Tool,
		Width:        m.env.Width,
		Height:       m.env.Height,
		Cwd:          m.env.CWD,

		DisabledTools: m.services.Setting.DisabledTools(),
		ProviderStore: m.services.LLM.Store(),
		LLMProvider:   m.env.LLMProvider,
		InputTokens:   m.env.InputTokens,
		CurrentModel:  m.env.CurrentModel,

		Command: m.services.Command,
		Skill:   m.services.Skill,
		Plugin:  m.services.Plugin,
		MCP:     m.services.MCP,
		Tracker: m.services.Tracker,
		Cron:    m.services.Cron,
		ToolSvc: m.services.Tool,

		GetSessionID:      func() string { return m.services.Session.ID() },
		GetSessionStore:   func() *session.Store { return m.services.Session.GetStore() },
		GetThinkingEffort: func() string { return m.env.EffectiveThinkingEffort() },

		ResetTokens:        m.env.ResetTokens,
		SetThinkingEffort:  func(effort string) { m.env.ThinkingEffort = effort },
		EnsureSessionStore: func(cwd string) error { return m.services.Session.EnsureStore(cwd) },
		ForkSession:        m.forkSession,

		CommitMessages:          m.CommitMessages,
		StartProviderTurn:       m.StartProviderTurn,
		HandleSkillInvocation:   m.HandleSkillInvocation,
		StartExternalEditor:     m.StartExternalEditor,
		ReloadPluginBackedState: m.ReloadPluginBackedState,
		PersistSession:          m.PersistSession,
		InitTaskStorage:         m.InitTaskStorage,
		ReconfigureAgentTool:    m.ReconfigureAgentTool,
		StopAgentSession:        m.StopAgentSession,
		FireSessionEnd:          m.FireSessionEnd,
		BuildCompactRequest:     m.BuildCompactRequest,
		SpinnerTickCmd:          m.SpinnerTickCmd,
		ResetCronQueue:          m.ResetCronQueue,
	}
}

func (m *model) executeCommand(ctx context.Context, inputText string) (string, tea.Cmd, bool) {
	return input.NewCommandController(m.commandDeps()).Execute(ctx, inputText)
}
