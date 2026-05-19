// conv.Runtime implementation: callbacks the agent's outbox event pump calls
// on the main bubbletea goroutine — turn start, token accounting, tool
// results, turn end, and stop. The actual side effects (committing
// scrollback, draining queues, firing hooks) live in adjacent model_*
// files; this file is the thin wire between agent events and those
// effects.
package app

import (
	"context"
	"errors"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/llm"
	"github.com/genai-io/gen-code/internal/llm/deepseek"
	"github.com/genai-io/gen-code/internal/llm/minmax"
	"github.com/genai-io/gen-code/internal/log"
)

func (m *model) BeginInferTurn() {
	if m.env.turnUsageActive {
		return
	}
	m.env.TurnInputTokens = 0
	m.env.TurnOutputTokens = 0
	m.env.turnUsageActive = true
}

func (m *model) SetTokenUsage(resp *core.InferResponse) {
	if resp == nil {
		return
	}

	if m.userInput.Provider.StatusMessage == "compacted" {
		m.userInput.Provider.StatusMessage = ""
	}

	// Bottom-right context usage reflects the latest prompt/output, not a
	// lifetime sum across the whole session.
	m.env.InputTokens = resp.TokensIn
	m.env.OutputTokens = resp.TokensOut
	m.env.TurnInputTokens += resp.TokensIn
	m.env.TurnOutputTokens += resp.TokensOut

	if m.env.CurrentModel != nil {
		switch m.env.CurrentModel.Provider {
		case llm.MinMax:
			cost, ok := minmax.EstimateCost(m.env.CurrentModel.ModelID, llm.Usage{
				InputTokens:              resp.TokensIn,
				OutputTokens:             resp.TokensOut,
				CacheCreationInputTokens: resp.CacheCreateTokens,
				CacheReadInputTokens:     resp.CacheReadTokens,
			})
			if ok {
				m.env.ConversationCost = m.env.ConversationCost.Add(cost)
			}
		case llm.DeepSeek:
			cost, ok := deepseek.EstimateCost(m.env.CurrentModel.ModelID, llm.Usage{
				InputTokens:              resp.TokensIn,
				OutputTokens:             resp.TokensOut,
				CacheCreationInputTokens: resp.CacheCreateTokens,
				CacheReadInputTokens:     resp.CacheReadTokens,
			})
			if ok {
				m.env.ConversationCost = m.env.ConversationCost.Add(cost)
			}
		}
	}
}

func (m *model) HasRunningTasks() bool { return m.services.Tracker.HasInProgress() }

// HandleAgentMessage observes the agent's MessageEvent echoes. Every path
// that hands a user message to the agent appends to m.conv at the call site,
// so the echo has nothing to do here — appending again would double-display.
func (m *model) HandleAgentMessage(core.Message) tea.Cmd {
	return nil
}

func (m *model) ProcessToolResult(tr core.ToolResult) *core.ToolResult {
	sideEffect := m.services.Tool.PopSideEffect(tr.ToolCallID)
	if sideEffect != nil {
		m.applyToolSideEffects(tr.ToolName, sideEffect)
	}
	m.firePostToolHook(tr, sideEffect)

	result := &core.ToolResult{
		ToolCallID: tr.ToolCallID,
		ToolName:   tr.ToolName,
		Content:    tr.Content,
		IsError:    tr.IsError,
	}
	m.persistOverflow(result)
	return result
}

func (m *model) ProcessTurnEnd(result core.Result) tea.Cmd {
	m.env.turnUsageActive = false
	if m.services.Tracker.AllDone() {
		m.services.Tracker.Reset()
	}
	log.QueueLog("ProcessTurnEnd: starting queueLen=%d", m.userInput.Queue.Len())
	commitCmds := m.CommitMessages()

	if cmd, found := m.drainTurnQueues(); found {
		log.QueueLog("ProcessTurnEnd: drained queued message, skipping hooks")
		if cmd != nil {
			commitCmds = append(commitCmds, cmd)
		}
		commitCmds = append(commitCmds, m.ContinueOutbox())
		return tea.Batch(commitCmds...)
	}

	log.QueueLog("ProcessTurnEnd: firing idle hooks async")
	commitCmds = append(commitCmds, m.fireIdleHooksCmd(result), m.ContinueOutbox())
	return tea.Batch(commitCmds...)
}

func (m *model) ProcessAgentStop(err error) tea.Cmd {
	m.env.turnUsageActive = false
	// /clear and manual stop cancel the active agent context; that is expected
	// shutdown, not an agent failure the user needs to see.
	if err != nil && !errors.Is(err, context.Canceled) {
		m.conv.AddNotice(fmt.Sprintf("Agent error: %v", err))
		m.fireStopFailureHook(core.LastAssistantChatContent(m.conv.Messages), err)
	}
	m.conv.ProgressHub.DrainPendingQuestions()
	m.conv.Modal.Question.Hide()
	commitCmds := m.CommitMessages()
	m.StopAgentSession()
	return tea.Batch(commitCmds...)
}
