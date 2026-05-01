package agent

import (
	"context"
	"testing"

	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/tool"
)

type stubAgentExecutor struct {
	lastRun tool.AgentExecRequest
}

func (s *stubAgentExecutor) Run(ctx context.Context, req tool.AgentExecRequest) (*tool.AgentExecResult, error) {
	s.lastRun = req
	return &tool.AgentExecResult{
		AgentName: req.Agent,
		Success:   true,
		Content:   "done",
	}, nil
}

func (s *stubAgentExecutor) RunBackground(req tool.AgentExecRequest) (tool.AgentTaskInfo, error) {
	return tool.AgentTaskInfo{TaskID: "task-1", AgentName: req.Agent}, nil
}

func (s *stubAgentExecutor) GetAgentConfig(agentType string) (tool.AgentConfigInfo, bool) {
	return tool.AgentConfigInfo{Name: agentType}, true
}

func (s *stubAgentExecutor) GetParentModelID() string {
	return "test-model"
}

func TestAgentToolForkUsesContextMessagesGetter(t *testing.T) {
	executor := &stubAgentExecutor{}
	toolInst := NewAgentTool()
	toolInst.SetExecutor(executor)

	parentMessages := []core.Message{
		{Role: core.RoleUser, Content: "parent context"},
	}
	ctx := tool.WithMessagesGetter(context.Background(), func() []core.Message {
		return parentMessages
	})

	result := toolInst.Execute(ctx, map[string]any{
		"subagent_type": "general-purpose",
		"description":   "test fork",
		"prompt":        "use context",
		"fork":          true,
	}, "/repo")

	if !result.Success {
		t.Fatalf("expected success, got error: %s", result.Error)
	}
	if len(executor.lastRun.ParentMessages) != 1 || executor.lastRun.ParentMessages[0].Content != "parent context" {
		t.Fatalf("fork parent messages were not forwarded: %#v", executor.lastRun.ParentMessages)
	}
}
