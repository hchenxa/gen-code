# Feature 23: Harness Channels & System-Reminder

## Overview

GenCode delivers context to the LLM through three distinct channels (matching
Claude Code). Each has different cache/mutability/persistence behavior:

| Channel | Carrier | Mutability | What lives here |
|---------|---------|------------|-----------------|
| **A — System prompt** | API `system` field | Effectively immutable | Identity, provider quirks, policy, guidelines, environment |
| **B — Tool schema** | API `tools` field | Frozen at agent build time | Tool definitions; the **Agent tool's `description`** embeds the available-agents directory |
| **C — System-reminder** | XML inside user messages | Per-turn dynamic | Skills directory, memory (CLAUDE.md / GEN.md), hook-injected context |

Channel C is implemented by `internal/reminder` (`Service` with providers,
`Wrap`, `AttachToContent`). Reminders ride on the next user message and are
re-emitted on `SessionStart` and `PostCompact`. See
[`docs/system-prompt.md`](../system-prompt.md) for the full design.

## UI Interactions

- **`/skills`**: toggle Active/Enable/Disable — re-emits providers immediately so the next user message has the updated skills directory.
- **`/agents`**: toggle enable/disable — stops the running agent **if idle**, so `ensureAgentSession` rebuilds it (with a fresh Agent tool description) on the next user message. Mid-stream toggles wait for the current turn to finish.
- **`/compact`** (manual or auto): triggers `EnqueueAllProviders` so skills/memory reattach to the user message after the compact summary.

## Automated Tests

```bash
go test ./internal/reminder/... -v
go test ./internal/core/system/... -v
go test ./internal/tool/... -run "TestAgentToolSchema|TestAgentDirectory" -v
```

Covered:

```
# reminder package
TestWrapEmpty / TestWrapNonEmpty                          — <system-reminder> envelope
TestServiceEnqueueAndDrain                                — queue lifecycle
TestServiceProviderRegistration / ReplaceByID / EmptyOutput — provider semantics
TestServiceUnregister                                     — provider removal
TestAttachToContentNoReminders / Multiple                 — message attach shape
TestServiceFullSessionLifecycle                           — SessionStart → drain → PostCompact → re-emit
TestServiceProviderReflectsLatestState                    — providers query live state on each emit
TestServiceEnqueueAllProvidersIsIdempotent                — duplicate-emission leak guard
TestServiceConcurrentAccess                               — 50 goroutines enqueue safely

# system catalog (channel A) — capabilities/memory absent from system prompt
TestBuildPromptOmitsCapabilities                          — no <skills> or <agents>
TestBuildPromptOmitsMemory                                — no <memory>
TestBuildPromptOrder_StableBeforeVolatile                 — slot ordering preserved

# tool schema (channel B) — Agent tool description carries the directory
TestAgentToolSchemaEmbedsDirectory                        — directory body inlined
TestAgentToolSchemaOmitsDirectoryWhenEmpty                — subagent context: no directory
TestGetToolSchemasUsesDirectoryGetter                     — schema chain wires the getter
TestAgentDirectoryReevaluatedPerCall                      — getter runs fresh per build (toggle picks up)
```

Cases to add:

```go
func TestReminder_HookContextSurvivesPostCompact(t *testing.T) {
    // Ad-hoc Enqueue (hook-injected) survives EnqueueAllProviders calls;
    // only provider-emitted entries get replaced.
}

func TestReminder_DrainEmptyQueue(t *testing.T) {
    // Drain on empty returns nil with no allocation (hot-path guard).
}

func TestSubagent_FirstUserMessageHasReminders(t *testing.T) {
    // Subagent's first message includes <system-reminder> blocks for
    // skills + memory, matching the main agent's shape.
}
```

## Manual Inspection (DEV_DIR)

Every API request and response is dumped as JSON when `DEV_DIR` is set
(`internal/log/devdir.go`). Use this for manual verification — no extra
flags or rebuilds.

### Setup

```bash
cd /path/to/gencode
make build
mkdir -p /tmp/gen-dev
```

Launch with the dump dir for any scenario below:

```bash
rm -rf /tmp/gen-dev/* && DEV_DIR=/tmp/gen-dev ./bin/gen
```

Each turn produces `main-NNN-request.json` (and `-response.json`). The request
JSON has three top-level fields that map directly to the channels:

| JSON field | Channel |
|------------|---------|
| `system_prompt` | A |
| `tools[]` | B |
| `messages[]` | conversation history (where C rides) |

### Scenario 1 — Cold start: verify the three-channel split

**Operate (TUI)**:
1. Type `hello`, Enter
2. Wait for response
3. Quit

**Inspect**:

```bash
# (A) system prompt should NOT contain <memory>, <skills>, <agents>, <notice>
jq -r '.system_prompt' /tmp/gen-dev/main-001-request.json \
  | grep -E "^<(memory|skills|agents|notice)" \
  || echo "✅ system prompt clean"

# (B) Agent tool description should embed available agent types
jq -r '.tools[] | select(.name=="Agent") | .description' /tmp/gen-dev/main-001-request.json \
  | head -20

# (C) first user message should carry <system-reminder> for skills + memory
jq -r '.messages[0].content' /tmp/gen-dev/main-001-request.json | head -50
```

**Expected**: system prompt has none of the four legacy tags · Agent description includes
"Available agent types for the Agent tool:" · first user message contains
`<system-reminder>` blocks with skills directory and `<memory scope="user">`/`<memory scope="project">`.

### Scenario 2 — Multi-turn: reminders only on the first message

**Operate**: send `hello`, then `hello again`, then quit.

**Inspect**:

```bash
# turn 1's user message: should include <system-reminder>
jq -r '.messages[] | select(.role=="user") | .content' /tmp/gen-dev/main-001-request.json \
  | grep -c "system-reminder"
# expected: ≥ 1

# turn 2's "hello again" (last user message in the request): no reminder
jq -r '.messages[-1].content' /tmp/gen-dev/main-002-request.json
# expected: "hello again" (plain text, no <system-reminder>)
```

**Expected**: queue is drained after first emission; subsequent user messages
stay clean until SessionStart/PostCompact/`/skills`/`/agents` re-enqueue.

### Scenario 3 — `/skills` toggle: live update

**Operate**:
1. `hello`, wait
2. `/skills`, ↑/↓ to a skill, Enter to cycle its state, Esc
3. `try again`, wait
4. Quit

**Inspect**:

```bash
# Before toggle: turn 1's skills directory
jq -r '.messages[0].content' /tmp/gen-dev/main-001-request.json \
  | sed -n '/<system-reminder>/,/<\/system-reminder>/p' \
  | grep -E "^- "

# After toggle: turn 2's last user message
jq -r '.messages[-1].content' /tmp/gen-dev/main-002-request.json \
  | grep -A 30 "system-reminder"
```

**Expected**: turn 2's user message carries a fresh `<system-reminder>` whose
skills list reflects the toggle.

### Scenario 4 — `/agents` toggle: tool description rebuild

**Operate**:
1. `hello`, wait
2. `/agents`, disable an agent (e.g. `code-reviewer`), Esc
3. `again`, wait
4. Quit

**Inspect**:

```bash
# Before
jq -r '.tools[] | select(.name=="Agent") | .description' /tmp/gen-dev/main-001-request.json \
  | grep "code-reviewer" && echo "✅ turn 1 has code-reviewer"

# After
jq -r '.tools[] | select(.name=="Agent") | .description' /tmp/gen-dev/main-002-request.json \
  | grep "code-reviewer" && echo "❌ turn 2 still has it" \
  || echo "✅ turn 2 removed"
```

**Expected**: turn 1 description lists `code-reviewer`; turn 2 omits it.
(Mid-stream guard: if the toggle happens while the assistant is mid-response,
the rebuild is deferred to the next user input.)

### Scenario 5 — `/compact`: reminders re-attach after compaction

**Operate**: send `hi 1`, `hi 2`, `hi 3` to build history; `/compact`; wait;
send `after compact`; quit.

**Inspect**:

```bash
LAST=$(ls /tmp/gen-dev/main-*-request.json | tail -1)
jq -r '.messages[-1].content' "$LAST" | head -50
```

**Expected**: the `after compact` user message has fresh `<system-reminder>`
blocks for skills + memory — proving the `PostCompact` handler called
`EnqueueAllProviders`.

### Scenario 6 — Idempotent `EnqueueAllProviders`: no duplicates on rapid toggles

**Operate**: `/skills`, cycle the same skill 3 times in a row (Enter Enter Enter),
Esc; then `test`; quit.

**Inspect**:

```bash
# Count how many times the skills-directory header appears in the user message
jq -r '.messages[-1].content' /tmp/gen-dev/main-001-request.json \
  | grep -c "Use the Skill tool to invoke these capabilities"
# expected: 1 (not 3 or 4)
```

**Expected**: exactly one skills reminder, regardless of how many times the
provider was re-emitted between sends. Backed by
`TestServiceEnqueueAllProvidersIsIdempotent`.

## Interactive Tests (tmux)

```bash
tmux new-session -d -s t_harness -x 220 -y 60
mkdir -p /tmp/gen-dev && rm -rf /tmp/gen-dev/*
tmux send-keys -t t_harness 'DEV_DIR=/tmp/gen-dev gen' Enter
sleep 2

# Test 1: cold-start three-channel split
tmux send-keys -t t_harness 'hello' Enter
sleep 6
test -f /tmp/gen-dev/main-001-request.json || echo "❌ no request dump"

# Channel A — system prompt is clean
jq -r '.system_prompt' /tmp/gen-dev/main-001-request.json \
  | grep -E "^<(memory|skills|agents|notice)" \
  && echo "❌ legacy tag in system prompt" \
  || echo "✅ A: system prompt clean"

# Channel B — Agent description carries directory
jq -r '.tools[] | select(.name=="Agent") | .description' /tmp/gen-dev/main-001-request.json \
  | grep -q "Available agent types for the Agent tool" \
  && echo "✅ B: agent directory inlined" \
  || echo "❌ B: agent directory missing"

# Channel C — first user message has system-reminder
jq -r '.messages[0].content' /tmp/gen-dev/main-001-request.json \
  | grep -q "system-reminder" \
  && echo "✅ C: system-reminder on first user msg" \
  || echo "❌ C: no system-reminder"

# Test 2: second turn — reminders drained
tmux send-keys -t t_harness 'hello again' Enter
sleep 6
jq -r '.messages[-1].content' /tmp/gen-dev/main-002-request.json \
  | grep -q "system-reminder" \
  && echo "❌ second turn has stale reminder" \
  || echo "✅ second turn drained cleanly"

tmux send-keys -t t_harness C-c
tmux kill-session -t t_harness
```

## Implementation Reference

| File | Role |
|------|------|
| `internal/reminder/reminder.go` | `Service`, `Provider`, `NewProvider`, `Wrap`, `WrapMemory`, `AttachToContent`, provider ID constants |
| `internal/app/agent.go` | `wireReminderProviders` (skills/memory providers); `attachPendingReminders` drains on every `sendToAgent` |
| `internal/app/hooks.go` | SessionStart + UserPromptSubmit hook context → `Reminder.Enqueue` |
| `internal/app/model.go` | PostCompact handlers → `Reminder.EnqueueAllProviders` |
| `internal/app/update.go` | `SkillCycleMsg` → `EnqueueAllProviders`; `AgentToggleMsg` → `Agent.Stop` (idle only) |
| `internal/subagent/executor.go` | `collectSubagentReminders` builds first-message reminders for subagent runs |
| `internal/tool/schema_agent.go` | `agentToolSchema(directory)` builds Agent description with directory inline |
| `internal/log/devdir.go` | `DEV_DIR` JSON dump used by the manual scenarios above |

## See Also

- [`docs/system-prompt.md`](../system-prompt.md) — full architectural design (slot table, channels, build API)
- [Feature 9: Skills System](9-skills.md) — channel C carrier for the skills directory
- [Feature 10: Agents](10-agents.md) — channel B carrier for the agents directory
- [Feature 15: Compact](15-compact.md) — PostCompact triggers reminder re-emission
- [Feature 16: Memory](16-memory.md) — CLAUDE.md / GEN.md flow into channel C
