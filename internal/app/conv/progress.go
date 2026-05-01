package conv

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/genai-io/gen-code/internal/app/kit"
	"github.com/genai-io/gen-code/internal/core"
	"github.com/genai-io/gen-code/internal/tool"
)

// ProgressUpdateMsg carries a task progress update from an agent.
type ProgressUpdateMsg struct {
	Index   int
	Message string
}

// ProgressQuestionMsg carries an agent question request to the TUI.
type ProgressQuestionMsg struct {
	Index   int
	Request *tool.QuestionRequest
	Reply   chan *tool.QuestionResponse
}

// ProgressCheckTickMsg triggers a check for new progress updates.
type ProgressCheckTickMsg struct{}

// ProgressHub is an instance-scoped progress transport.
type ProgressHub struct {
	ch  chan ProgressUpdateMsg
	qch chan ProgressQuestionMsg
}

// NewProgressHub creates a new progress hub with the given buffer size.
func NewProgressHub(buffer int) *ProgressHub {
	if buffer <= 0 {
		buffer = 100
	}
	return &ProgressHub{
		ch:  make(chan ProgressUpdateMsg, buffer),
		qch: make(chan ProgressQuestionMsg, buffer),
	}
}

// SendForAgent enqueues a progress message for a specific agent index.
func (h *ProgressHub) SendForAgent(index int, msg string) {
	select {
	case h.ch <- ProgressUpdateMsg{Index: index, Message: msg}:
	default:
	}
}

// Ask enqueues an interactive question and waits for the user's response.
func (h *ProgressHub) Ask(ctx context.Context, index int, req *tool.QuestionRequest) (*tool.QuestionResponse, error) {
	if h == nil {
		return nil, fmt.Errorf("progress hub not initialized")
	}

	reply := make(chan *tool.QuestionResponse, 1)
	select {
	case h.qch <- ProgressQuestionMsg{Index: index, Request: req, Reply: reply}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}

	select {
	case resp := <-reply:
		if resp == nil {
			return nil, fmt.Errorf("question prompt closed without a response")
		}
		return resp, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// Check returns a tea.Cmd that polls this hub for the next update.
func (h *ProgressHub) Check() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(t time.Time) tea.Msg {
		select {
		case q := <-h.qch:
			return q
		case u := <-h.ch:
			return u
		default:
			return ProgressCheckTickMsg{}
		}
	})
}

// DrainPendingQuestions cancels any pending questions left in the channel.
// Called when the agent stops to prevent orphaned questions from appearing later.
func (h *ProgressHub) DrainPendingQuestions() {
	if h == nil {
		return
	}
	for {
		select {
		case q := <-h.qch:
			select {
			case q.Reply <- &tool.QuestionResponse{Cancelled: true}:
			default:
			}
		default:
			return
		}
	}
}

// Drain pulls all pending updates into taskProgress.
func (h *ProgressHub) Drain(taskProgress map[int][]string) map[int][]string {
	for {
		select {
		case u := <-h.ch:
			if taskProgress == nil {
				taskProgress = make(map[int][]string)
			}
			taskProgress[u.Index] = append(taskProgress[u.Index], u.Message)
			if len(taskProgress[u.Index]) > maxAgentProgressHistory {
				taskProgress[u.Index] = taskProgress[u.Index][len(taskProgress[u.Index])-maxAgentProgressHistory:]
			}
		default:
			return taskProgress
		}
	}
}

// maxAgentProgressHistory is the maximum number of progress lines retained per agent.
const maxAgentProgressHistory = 12

// maxAgentProgressLines is the maximum number of progress lines to display.
// Older lines scroll off the top, keeping the view compact.
const maxAgentProgressLines = 8

// maxCompactAgentToolLines is the number of recent tool calls shown while collapsed.
const maxCompactAgentToolLines = 3

type AgentRuntimeMeta struct {
	ModelName    string
	InputTokens  int
	OutputTokens int
}

// renderAgentProgress renders the most recent agent progress lines,
// capped at maxAgentProgressLines to keep the view height bounded.
func renderAgentProgress(progress []string) string {
	if len(progress) == 0 {
		return ""
	}

	// Only show the most recent lines
	visible := progress
	if len(visible) > maxAgentProgressLines {
		visible = visible[len(visible)-maxAgentProgressLines:]
	}

	var sb strings.Builder
	for _, p := range visible {
		sb.WriteString(toolResultStyle.Render(fmt.Sprintf("  ⎿  %s", p)) + "\n")
	}
	return sb.String()
}

// renderTaskProgressInline renders live progress for a parallel Agent tool call.
// Spinner is on the header line; this only renders progress lines below it.
func renderTaskProgressInline(tc core.ToolCall, pendingCalls []core.ToolCall, parallelResults map[int]bool, taskProgress map[int][]string, expanded bool, meta AgentRuntimeMeta) string {
	idx := -1
	for i, pending := range pendingCalls {
		if pending.ID == tc.ID {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}

	// Check if completed in parallel results (not yet committed to messages)
	if parallelResults != nil {
		if _, done := parallelResults[idx]; done {
			return ""
		}
	}

	progress := taskProgress[idx]
	if expanded {
		return renderAgentProgress(progress)
	}
	return renderCompactAgentProgress(tc.Input, progress, meta)
}

func renderCompactAgentProgress(input string, progress []string, meta AgentRuntimeMeta) string {
	if len(progress) == 0 {
		return ""
	}

	var sb strings.Builder
	if summary := formatAgentRuntimeSummary(input, progress, meta); summary != "" {
		sb.WriteString(toolResultStyle.Render("  ⎿  "+summary) + "\n")
	}

	toolLines := recentAgentToolProgress(progress, maxCompactAgentToolLines)
	for _, line := range toolLines {
		sb.WriteString(toolResultStyle.Render("  ⎿  "+line) + "\n")
	}
	return sb.String()
}

func formatAgentRuntimeSummary(input string, progress []string, meta AgentRuntimeMeta) string {
	parts := make([]string, 0, 4)
	if model := agentModelFromInput(input, meta.ModelName); model != "" {
		parts = append(parts, "model: "+model)
	}
	if mode := agentModeFromInput(input, progress); mode != "" {
		parts = append(parts, "mode: "+mode)
	}
	if n := len(recentAgentToolProgress(progress, 0)); n > 0 {
		parts = append(parts, fmt.Sprintf("tools: %d", n))
	}
	if tokens := formatRuntimeTokens(meta.InputTokens, meta.OutputTokens); tokens != "" {
		parts = append(parts, "tokens: "+tokens)
	}
	return strings.Join(parts, "   ")
}

func agentModelFromInput(input, fallback string) string {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err == nil {
		if model, ok := params["model"].(string); ok && model != "" {
			return model
		}
	}
	return fallback
}

func agentModeFromInput(input string, progress []string) string {
	var params map[string]any
	if err := json.Unmarshal([]byte(input), &params); err == nil {
		if mode, ok := params["mode"].(string); ok && mode != "" {
			return mode
		}
	}
	for _, line := range progress {
		if after, ok := strings.CutPrefix(line, "Mode: "); ok {
			mode, _, _ := strings.Cut(after, " · ")
			return strings.TrimSpace(mode)
		}
	}
	return "default"
}

func formatRuntimeTokens(inputTokens, outputTokens int) string {
	if inputTokens <= 0 && outputTokens <= 0 {
		return ""
	}
	return fmt.Sprintf("↑%s ↓%s", kit.FormatTokenCount(inputTokens), kit.FormatTokenCount(outputTokens))
}

func recentAgentToolProgress(progress []string, limit int) []string {
	lines := make([]string, 0, len(progress))
	for _, line := range progress {
		if isAgentToolProgressLine(line) {
			lines = append(lines, line)
		}
	}
	if limit > 0 && len(lines) > limit {
		lines = lines[len(lines)-limit:]
	}
	return lines
}

func isAgentToolProgressLine(line string) bool {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "Mode: ") || line == "Thinking..." {
		return false
	}
	return true
}

// PendingToolSpinnerParams holds the parameters for rendering a pending tool spinner.
type PendingToolSpinnerParams struct {
	// InteractivePromptActive indicates if an interactive prompt is currently active.
	InteractivePromptActive bool
	// ParallelMode indicates parallel tool execution.
	ParallelMode bool
	// HasParallelTaskTools indicates if any parallel tools are Task tools.
	HasParallelTaskTools bool
	// BuildingTool is the tool name being built during streaming.
	BuildingTool string
	// PendingCalls are the pending tool calls.
	PendingCalls []core.ToolCall
	// CurrentIdx is the index of the current sequential tool.
	CurrentIdx int
	// TaskProgress tracks agent progress messages by index.
	TaskProgress map[int][]string
	// SpinnerView is the current spinner frame.
	SpinnerView string
	// Width is the terminal width for label truncation.
	Width int
	// SuppressAgentLabel avoids duplicating the active agent title when the
	// assistant message already rendered it above the progress lines.
	SuppressAgentLabel bool
}

// RenderPendingToolSpinner renders the spinner for a tool being executed.
func RenderPendingToolSpinner(params PendingToolSpinnerParams) string {
	if params.InteractivePromptActive {
		return ""
	}

	// Parallel mode with Task tools: progress rendered inline by RenderToolCalls
	if params.ParallelMode && params.HasParallelTaskTools {
		return ""
	}

	// Determine which tool is active
	var toolName string
	if params.BuildingTool != "" {
		toolName = params.BuildingTool
	} else if params.PendingCalls != nil && params.CurrentIdx < len(params.PendingCalls) {
		toolName = params.PendingCalls[params.CurrentIdx].Name
	} else {
		return ""
	}

	// Agent tool: render agent label + progress lines
	if tool.IsAgentToolName(toolName) {
		var sb strings.Builder
		// Show Agent label so it remains visible after the assistant message scrolls off.
		if !params.SuppressAgentLabel && params.PendingCalls != nil && params.CurrentIdx < len(params.PendingCalls) {
			tc := params.PendingCalls[params.CurrentIdx]
			label := formatAgentLabel(tc.Input)
			sb.WriteString(renderToolLineWithIcon(label, params.Width, params.SpinnerView) + "\n")
		}
		sb.WriteString(renderAgentProgress(params.TaskProgress[params.CurrentIdx]))
		return sb.String()
	}

	// Standard tools: spinner is shown inline in the assistant message row,
	// no separate spinner line needed.
	return ""
}
