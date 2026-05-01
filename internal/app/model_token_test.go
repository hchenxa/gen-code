package app

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/genai-io/gen-code/internal/app/conv"
	"github.com/genai-io/gen-code/internal/app/input"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/task/tracker"
)

func TestSetTokenUsageTracksLatestTurnUsage(t *testing.T) {
	m := &model{}
	m.BeginInferTurn()

	m.SetTokenUsage(&core.InferResponse{TokensIn: 1200, TokensOut: 80})
	if m.env.InputTokens != 1200 || m.env.OutputTokens != 80 {
		t.Fatalf("first token update = in:%d out:%d, want in:1200 out:80", m.env.InputTokens, m.env.OutputTokens)
	}
	if m.env.TurnInputTokens != 1200 || m.env.TurnOutputTokens != 80 {
		t.Fatalf("first turn totals = in:%d out:%d, want in:1200 out:80", m.env.TurnInputTokens, m.env.TurnOutputTokens)
	}

	m.SetTokenUsage(&core.InferResponse{TokensIn: 400, TokensOut: 25})
	if m.env.InputTokens != 400 || m.env.OutputTokens != 25 {
		t.Fatalf("latest token update = in:%d out:%d, want in:400 out:25", m.env.InputTokens, m.env.OutputTokens)
	}
	if m.env.TurnInputTokens != 1600 || m.env.TurnOutputTokens != 105 {
		t.Fatalf("accumulated turn totals = in:%d out:%d, want in:1600 out:105", m.env.TurnInputTokens, m.env.TurnOutputTokens)
	}
}

func TestResumeCommandForSessionRequiresPersistedTranscript(t *testing.T) {
	transcriptPath := filepath.Join(t.TempDir(), "session.jsonl")

	if got := resumeCommandForSession("session-1", transcriptPath); got != "" {
		t.Fatalf("resumeCommandForSession() = %q, want empty command for missing transcript", got)
	}

	if err := os.WriteFile(transcriptPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatalf("write transcript: %v", err)
	}

	if got := resumeCommandForSession("session-1", transcriptPath); got != "gen -r session-1" {
		t.Fatalf("resumeCommandForSession() = %q, want gen -r session-1", got)
	}
}

func TestSetTokenUsageClearsCompactedStatusOnNextInfer(t *testing.T) {
	m := &model{}
	m.userInput.Provider.StatusMessage = "compacted"

	m.SetTokenUsage(&core.InferResponse{TokensIn: 400, TokensOut: 25})

	if m.userInput.Provider.StatusMessage != "" {
		t.Fatalf("StatusMessage = %q, want compacted badge cleared on next infer", m.userInput.Provider.StatusMessage)
	}
}

func TestBeginInferTurnResetsTurnTotalsOnlyForNewTurn(t *testing.T) {
	m := &model{}
	m.env.TurnInputTokens = 1600
	m.env.TurnOutputTokens = 105

	m.env.turnUsageActive = true
	m.BeginInferTurn()
	if m.env.TurnInputTokens != 1600 || m.env.TurnOutputTokens != 105 {
		t.Fatalf("existing turn totals changed unexpectedly: in:%d out:%d", m.env.TurnInputTokens, m.env.TurnOutputTokens)
	}

	m.env.turnUsageActive = false
	m.BeginInferTurn()
	if m.env.TurnInputTokens != 0 || m.env.TurnOutputTokens != 0 {
		t.Fatalf("new turn reset = in:%d out:%d, want zeros", m.env.TurnInputTokens, m.env.TurnOutputTokens)
	}
}

func TestResetContextDisplayPreservesTurnTotals(t *testing.T) {
	m := &model{}
	m.env.InputTokens = 1200
	m.env.OutputTokens = 80
	m.env.TurnInputTokens = 1600
	m.env.TurnOutputTokens = 105

	m.env.ResetContextDisplay()

	if m.env.InputTokens != 0 || m.env.OutputTokens != 0 {
		t.Fatalf("context display reset = in:%d out:%d, want zeros", m.env.InputTokens, m.env.OutputTokens)
	}
	if m.env.TurnInputTokens != 1600 || m.env.TurnOutputTokens != 105 {
		t.Fatalf("turn totals changed unexpectedly: in:%d out:%d", m.env.TurnInputTokens, m.env.TurnOutputTokens)
	}
}

func TestHandleAgentMessageRemovesInjectedQueuedInput(t *testing.T) {
	m := &model{
		userInput: input.Model{Queue: input.NewQueue()},
		conv:      conv.NewModel(80),
		services:  services{Tracker: tracker.NewStore()},
	}
	m.userInput.Queue.Enqueue("queued message", nil)
	m.userInput.Queue.MarkSentToInbox(0)

	_ = m.HandleAgentMessage(core.UserMessage("queued message", nil))

	if m.userInput.Queue.Len() != 0 {
		t.Fatalf("queue len = %d, want 0", m.userInput.Queue.Len())
	}
	if len(m.conv.Messages) != 1 {
		t.Fatalf("conversation messages = %d, want 1", len(m.conv.Messages))
	}
	if got := m.conv.Messages[0].Content; got != "queued message" {
		t.Fatalf("message content = %q, want queued message", got)
	}
}

func TestDrainTurnQueuesWaitsForSentQueuedInputInjection(t *testing.T) {
	m := &model{userInput: input.Model{Queue: input.NewQueue()}}
	m.userInput.Queue.Enqueue("already sent", nil)
	m.userInput.Queue.MarkSentToInbox(0)

	cmd, found := m.drainTurnQueues()

	if !found {
		t.Fatal("expected waiting queued input to hold the turn boundary")
	}
	if cmd != nil {
		t.Fatalf("cmd = %#v, want nil", cmd)
	}
	if m.userInput.Queue.Len() != 1 {
		t.Fatalf("queue len = %d, want waiting item retained", m.userInput.Queue.Len())
	}
}
