// Submit flow: handle Enter (HandleSubmit), build the SubmitDeps the input
// package needs, kick off a provider turn (StartProviderTurn), and the
// matching skill-invocation path that's the same flow but seeded by a
// skill button instead of typed text.
package app

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/genai-io/gen-code/internal/app/input"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/log"
	"github.com/genai-io/gen-code/internal/plugin"
)

func (m *model) handleSubmit() tea.Cmd {
	return input.HandleSubmit(m.submitDeps())
}

func (m *model) submitDeps() input.SubmitDeps {
	return input.SubmitDeps{
		Actions:         m,
		Input:           &m.userInput,
		Conversation:    &m.conv.ConversationModel,
		CheckPromptHook: m.checkPromptHook,
		Cwd:             m.env.CWD,
		HandleCommand: func(text string) (tea.Cmd, bool) {
			ctrl := input.NewCommandController(m.commandDeps())
			return ctrl.HandleSubmit(text)
		},
		ClearPluginRoot: plugin.ClearActivePluginRoot,
	}
}

func (m *model) StartProviderTurn(content string) tea.Cmd {
	log.QueueLog("StartProviderTurn: %q", truncate(content, 60))
	if m.env.LLMProvider == nil {
		m.conv.Append(core.ChatMessage{
			Role:    core.RoleNotice,
			Content: "No provider connected. Use /model to connect.",
		})
		return tea.Batch(m.CommitMessages()...)
	}

	startCmd, err := m.ensureAgentSession(content)
	if err != nil {
		m.conv.Append(core.ChatMessage{
			Role:    core.RoleNotice,
			Content: "Failed to start agent: " + err.Error(),
		})
		return tea.Batch(m.CommitMessages()...)
	}

	m.env.DetectThinkingKeywords(content)

	var images []core.Image
	if len(m.conv.Messages) > 0 {
		lastMsg := m.conv.Messages[len(m.conv.Messages)-1]
		images = lastMsg.Images
	}

	sendCmd := m.sendToAgent(content, images)
	if startCmd != nil {
		return tea.Batch(startCmd, sendCmd)
	}
	return sendCmd
}

func (m *model) HandleSkillInvocation() tea.Cmd {
	if m.env.LLMProvider == nil {
		m.conv.AddNotice("No provider connected. Use /model to connect.")
		m.userInput.Skill.ClearPending()
		return tea.Batch(m.CommitMessages()...)
	}

	startCmd, err := m.ensureAgentSession("")
	if err != nil {
		m.conv.AddNotice("Failed to start agent: " + err.Error())
		m.userInput.Skill.ClearPending()
		return tea.Batch(m.CommitMessages()...)
	}

	displayMsg, fullMsg := m.userInput.Skill.ConsumeInvocation()
	m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: fullMsg, DisplayContent: displayMsg})
	sendCmd := m.sendToAgent(fullMsg, nil)
	if startCmd != nil {
		return tea.Batch(startCmd, sendCmd)
	}
	return sendCmd
}
