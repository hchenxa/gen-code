package core

import (
	"strings"
	"testing"
)

func TestBuildConversationTextAggregatesToolCalls(t *testing.T) {
	text := BuildConversationText([]Message{
		AssistantMessage("", "", []ToolCall{
			{ID: "1", Name: "Bash"},
			{ID: "2", Name: "Bash"},
			{ID: "3", Name: "Glob"},
		}),
	})

	if !strings.Contains(text, "[Tool Calls: Bash × 2, Glob]") {
		t.Fatalf("BuildConversationText() = %q, want aggregated tool calls", text)
	}
	if strings.Count(text, "[Tool Call: Bash]") > 0 {
		t.Fatalf("BuildConversationText() = %q, should not emit repeated raw tool-call lines", text)
	}
}

func TestBuildConversationTextStripsSystemReminders(t *testing.T) {
	content := "Fix the login bug\n\n" +
		`<system-reminder source="memory-project">` + "\nproject memory\n</system-reminder>" +
		"\n\n<system-reminder>\none-time notice\n</system-reminder>"
	text := BuildConversationText([]Message{UserMessage(content, nil)})

	if !strings.Contains(text, "User: Fix the login bug") {
		t.Fatalf("BuildConversationText() = %q, want the real user prompt", text)
	}
	if strings.Contains(text, "system-reminder") || strings.Contains(text, "project memory") || strings.Contains(text, "one-time notice") {
		t.Fatalf("BuildConversationText() = %q, should strip system-reminder blocks", text)
	}
}

func TestBuildConversationTextDropsReminderOnlyMessage(t *testing.T) {
	content := "<system-reminder source=\"skills-directory\">\nuse the Skill tool\n</system-reminder>"
	text := BuildConversationText([]Message{UserMessage(content, nil)})

	if strings.Contains(text, "User:") {
		t.Fatalf("BuildConversationText() = %q, reminder-only message should not emit a User line", text)
	}
}
