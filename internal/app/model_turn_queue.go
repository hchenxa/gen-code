// Turn-boundary inbox drain and prompt injection. After every agent turn
// ends we drain (in priority order) queued user messages, cron-fired
// prompts, async-hook continuations, and the main eventHub buffer. Each
// drained item is converted to a notice + optional re-send to the agent.
// Also handles the Stop hook result that gates session persistence.
package app

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	"go.uber.org/zap"

	"github.com/genai-io/gen-code/internal/app/hub"
	"github.com/genai-io/gen-code/internal/app/input"
	"github.com/genai-io/gen-code/internal/app/trigger"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/log"
)

const maxEventsPerDrain = 8

func (m *model) handleStopHookResult(msg stopHookResultMsg) tea.Cmd {
	if msg.Blocked {
		log.QueueLog("handleStopHookResult: hooks BLOCKED reason=%q", msg.Reason)
		blockMsg := "Stop hook blocked: " + msg.Reason
		m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: blockMsg})
		return m.sendToAgent(blockMsg, nil)
	}
	log.QueueLog("handleStopHookResult: hooks done, persisting")
	var cmds []tea.Cmd
	if m.services.Session.ID() != "" {
		cmds = append(cmds, m.persistSessionCmd())
	} else {
		if err := m.PersistSession(); err != nil {
			log.Logger().Warn("failed to save session", zap.Error(err))
		}
	}
	if cmd := input.StartPromptSuggestion(m.promptSuggestionDeps()); cmd != nil {
		cmds = append(cmds, cmd)
	}
	if msg.Result.StopReason != "" && msg.Result.StopReason != core.StopEndTurn {
		m.conv.AddNotice(fmt.Sprintf("Agent stopped: %s", msg.Result.StopReason))
		if msg.Result.StopDetail != "" {
			m.conv.AddNotice(msg.Result.StopDetail)
		}
	}
	if len(cmds) > 0 {
		return tea.Batch(cmds...)
	}
	return nil
}

func (m *model) drainTurnQueues() (tea.Cmd, bool) {
	// Drain ONE user message per call so each gets its own agent response.
	// The agent's inner loop also drains one inbox message at a time,
	// producing one TurnEvent per queued message.
	if item, ok := m.userInput.Queue.Dequeue(); ok {
		log.QueueLog("drainTurnQueues: dequeued %q remaining=%d", truncate(item.Content, 60), m.userInput.Queue.Len())
		m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: item.Content, Images: item.Images})
		m.services.Agent.Send(item.Content, item.Images)
		return nil, true
	}

	if len(m.systemInput.CronQueue) > 0 {
		prompt := m.systemInput.CronQueue[0]
		m.systemInput.CronQueue = m.systemInput.CronQueue[1:]
		return m.injectCronPrompt(prompt), true
	}

	if m.systemInput.AsyncHookQueue != nil {
		if item, ok := m.systemInput.AsyncHookQueue.Pop(); ok {
			return m.injectAsyncHookContinuation(item), true
		}
	}

	if events := drainEvents(m.mainEvents, maxEventsPerDrain); len(events) > 0 {
		msgs := eventsToMessages(events)
		return m.injectNotification(hub.Merge(msgs)), true
	}

	return nil, false
}

func (m *model) injectNotification(msg hub.Message) tea.Cmd {
	if msg.Notice != "" {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: msg.Notice})
	}
	if m.env.LLMProvider == nil {
		if msg.Notice == "" {
			m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: "A background task completed, but no provider is connected."})
		}
		return tea.Batch(m.CommitMessages()...)
	}
	if msg.Content == "" {
		return tea.Batch(m.CommitMessages()...)
	}
	return m.sendToAgent(msg.Content, nil)
}

func drainEvents(ch <-chan hub.Event, max int) []hub.Event {
	var out []hub.Event
	for range max {
		select {
		case e := <-ch:
			out = append(out, e)
		default:
			return out
		}
	}
	return out
}

func eventsToMessages(events []hub.Event) []hub.Message {
	msgs := make([]hub.Message, len(events))
	for i, e := range events {
		msgs[i] = hub.Message{Notice: e.Subject, Content: e.Data}
	}
	return msgs
}

func (m *model) injectCronPrompt(prompt string) tea.Cmd {
	if m.env.LLMProvider == nil {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: fmt.Sprintf("Cron fired but no provider connected: %s", prompt)})
		return tea.Batch(m.CommitMessages()...)
	}
	m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: "Scheduled task fired"})
	m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: prompt})
	return m.sendToAgent(prompt, nil)
}

func (m *model) injectAsyncHookContinuation(item trigger.AsyncHookRewake) tea.Cmd {
	if item.Notice != "" {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: item.Notice})
	}
	if len(item.Context) == 0 {
		return tea.Batch(m.CommitMessages()...)
	}
	if m.env.LLMProvider == nil {
		m.conv.Append(core.ChatMessage{Role: core.RoleNotice, Content: "Async hook requested a follow-up, but no provider is connected."})
		return tea.Batch(m.CommitMessages()...)
	}
	for _, ctx := range item.Context {
		m.conv.Append(core.ChatMessage{Role: core.RoleUser, Content: ctx})
	}
	return m.sendToAgent(item.ContinuationPrompt, nil)
}
