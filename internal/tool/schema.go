package tool

import "github.com/genai-io/gen-code/internal/core"

// Tool name constants used in runtime comparisons across the codebase.
const (
	ToolAgent       = "Agent"
	ToolSendMessage = "SendMessage"
	ToolTaskOutput  = "TaskOutput"
	ToolTaskStop    = "TaskStop"

	// Deprecated aliases — kept for backward compatibility with cached model contexts.
	ToolAgentOutput   = ToolTaskOutput
	ToolAgentStop     = ToolTaskStop
	ToolSkill         = "Skill"
	ToolTaskCreate    = "TaskCreate"
	ToolTaskGet       = "TaskGet"
	ToolTaskUpdate    = "TaskUpdate"
	ToolTaskList      = "TaskList"
	ToolCronCreate    = "CronCreate"
	ToolCronDelete    = "CronDelete"
	ToolCronList      = "CronList"
	ToolEnterWorktree = "EnterWorktree"
	ToolExitWorktree  = "ExitWorktree"

	ToolAskUserQuestion = "AskUserQuestion"
)

// IsAgentToolName reports whether the tool name represents an agent-like worker tool.
func IsAgentToolName(name string) bool {
	return name == ToolAgent || name == ToolSendMessage
}

// GetToolSchemas returns core.ToolSchema definitions for all registered tools
func GetToolSchemas() []core.ToolSchema {
	return GetToolSchemasWithMCP(nil)
}

// GetToolSchemasWithMCP returns tool schemas including MCP tools if a getter is provided
func GetToolSchemasWithMCP(mcpToolsGetter func() []core.ToolSchema) []core.ToolSchema {
	tools := make([]core.ToolSchema, 0, 20)
	tools = append(tools, baseToolSchemas()...)
	tools = append(tools, skillToolSchema)
	tools = append(tools, agentToolSchema, sendMessageToolSchema)
	tools = append(tools, trackerToolSchemas...)
	tools = append(tools, cronToolSchemas...)
	tools = append(tools, worktreeToolSchemas...)

	if mcpToolsGetter != nil {
		tools = append(tools, mcpToolsGetter()...)
	}

	return tools
}

func filterSchemas(all []core.ToolSchema, disabled map[string]bool) []core.ToolSchema {
	if len(disabled) == 0 {
		return all
	}
	filtered := make([]core.ToolSchema, 0, len(all))
	for _, t := range all {
		if !disabled[t.Name] {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
