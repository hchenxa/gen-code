# System Prompt

How Gen Code builds, mutates, and renders the system prompt for the main
agent and subagents — and how it splits responsibility between three
**harness channels** so the system prompt itself stays effectively immutable.

## Harness channels

Gen Code, like Claude Code, delivers context to the LLM through three
distinct channels. Each has different cache, mutability, and persistence
characteristics:

```
┌──────────────────────────────────────────────────────────────────────┐
│  Channel A — System Prompt        (API system field, session-stable) │
│  ──────────────────────────────                                      │
│  Identity, provider, policy, guidelines, environment.                │
│  Effectively immutable: every LLM call sends the same body until     │
│  the day-of-week date rolls over.                                    │
│                                                                      │
│  Channel B — Tool Schemas         (API tools field, session-stable)  │
│  ──────────────────────────────                                      │
│  Tool descriptions and parameters.                                   │
│  The Agent tool's description embeds the available-agents directory  │
│  inline; other tools are static. /agents toggles stop the running    │
│  agent so ensureAgentSession rebuilds with the new directory next    │
│  turn.                                                               │
│                                                                      │
│  Channel C — System-Reminder      (XML inside user messages)         │
│  ──────────────────────────────                                      │
│  Skills directory, memory (CLAUDE.md / GEN.md), hook-injected        │
│  context (SessionStart, UserPromptSubmit additionalContext).         │
│  Re-injected on SessionStart and PostCompact via the harness         │
│  reminder service (internal/reminder). Changes here do NOT touch     │
│  the system prompt — only the next user message.                     │
└──────────────────────────────────────────────────────────────────────┘
```

The decision rule: **agent personality (true for every Gen Code session)
goes in the system prompt; capabilities tied to invocation go in the tool
schema; everything project- or session-level rides on system-reminder.**
This matches Claude Code's split.

## System prompt mental model

The system prompt is a layered structure of named **Sections**, each owning
a **Slot** (render order). Sections are cached individually; mutating one
re-renders only that section.

```
core.System
├── identity        (slot 0) — who you are
├── provider        (slot 1) — provider quirks
├── policy          (slot 2) — safety contract
├── guidelines      (slot 3) — tool usage, git, tasks, questions
└── environment     (slot 4) — cwd, git, date (only changes at day rollover)
```

Slots that used to live here but have moved to other channels:

| Former slot | Now lives in | Why |
|---|---|---|
| `memory` | system-reminder | per-project, per-machine; would invalidate cache on every reload |
| `skills` directory | system-reminder | active/enable state changes during a session |
| `agents` directory | Agent tool description | tool capability — directory and invocation belong together |
| `invocation` (skill body) | inline in user message | one-shot per skill activation; lives in conversation history |
| `notice` (hook context) | system-reminder | event-triggered; should not invalidate the system prompt cache |

## Slot reference

| Slot | Section | Source | Volatility |
|------|---------|--------|------------|
| 0 | `identity` | `prompts/identity.txt` (default), `WithIdentity(text)`, `WithSubagentIdentity(brief)` | session-stable |
| 1 | `provider` | `prompts/providers/<name>.txt` | session-stable |
| 2 | `policy` | `prompts/policy.txt` | always |
| 3 | `guidelines-tools` | `prompts/guidelines/tools.txt` | always |
| 3 | `guidelines-git` | `prompts/guidelines/git.txt` (only if git repo) | session-stable |
| 3 | `guidelines-tasks` | `prompts/guidelines/tasks.txt` (main only) | always |
| 3 | `guidelines-questions` | `prompts/guidelines/questions.txt` (main only) | always |
| 4 | `environment` | cwd, git, platform, model, today's date | per-day, per-cwd |

**Order rationale:** stable content sits in low slots so the prompt-cache
prefix survives changes in `environment` (which only flips at the day
rollover). Within a slot, sections render in insertion order so callers
control fine-grained order.

## Build API

```go
// internal/core/system/builder.go
sys := system.Build(core.ScopeMain,
    system.WithProvider(client.Name()),
    system.WithIdentity(persona),       // optional override
    system.WithGitGuidelines(isGit),
    system.WithEnvironment(env),
)
```

The build options are intentionally lean. Memory, skills, and agents directories
are NOT here — they belong to other channels (system-reminder for memory and
skills, Agent tool description for agents).

Stock sections (identity, policy, guidelines.tools, plus tasks/questions for
main scope) auto-apply when `Build` is called. Options register additional
sections or override stock ones by name.

### Scope-based defaults

`core.Scope` distinguishes who the prompt is for:

- **`ScopeMain`** — top-level interactive agent. All four guidelines, full
  capabilities, default identity.
- **`ScopeSubagent`** — spawned by Agent tool. Identity replaced via
  `WithSubagentIdentity(brief)`; `tasks` and `questions` guidelines omitted
  (main-only behaviors); `WithAgents` not called by default (subagents are
  leaves — no recursion).
- **`ScopeCompact`** — not built via `Build`; `system.CompactPrompt()`
  returns a one-shot string.

### Mutating at runtime

```go
sys.Refresh("environment")                // cwd changed, re-render env
```

In practice the only runtime mutation left is `Refresh("environment")` for
cwd / day rollover. Hook-injected notices, skill state changes, memory
edits — all of those go through the system-reminder channel
(`reminder.Service.Enqueue`) instead of mutating the system prompt.

Per-section render output is cached; `Refresh(name)` invalidates one
section. The full prompt is also cached and rebuilt only when something
changes (`dirty` flag in `core.system_impl`).

## XML envelope

All non-identity sections wrap their body in a uniform tag, applied by
`system/catalog.go:wrap()`:

```xml
<policy>...</policy>
<guidelines name="tools">...</guidelines>
<environment>...</environment>
```

The identity section is rendered raw (no envelope) so it appears as the
familiar "You are X" preamble. For subagents, identity uses `<identity>`
attributes to surface mode info: `<identity mode="explore">...</identity>`.

Memory and skills use `<memory>` and skill-listing tags too, but those bodies
ride on user messages inside `<system-reminder>` blocks — see
[`reminder` package](#harness-reminder-channel-internalreminder) below.

## Harness reminder channel (`internal/reminder`)

The reminder service supplies session/project context to the LLM through
`<system-reminder>` XML tags inside user messages. This keeps the system
prompt immutable while still surfacing dynamic state.

### Lifecycle

```
SessionStart  →  Service.EnqueueAllProviders()
                 (drained on first user message of session)

User submits  →  sendToAgent
                 ├─ Service.Drain() returns wrapped reminders
                 └─ AttachToContent appends them to the user content
                    before it goes into the agent inbox.

PostCompact   →  Service.EnqueueAllProviders()
                 (drained on next user message — recovers from compaction)

State change  →  Service.Enqueue(adhoc body)
                 (e.g. skill state toggled, file watcher fired)
```

### Provider registry

Long-lived sources register via `Service.Register(Provider)`. Each provider
has a stable ID (replace-by-ID semantics) and a `Render() string` that gets
called on every emission — so providers always reflect the latest state.

Registered in `model.wireReminderProviders()`:

| ID | Source | Wrapper |
|----|--------|---------|
| `skills-directory` | `skill.Registry.PromptSection()` | none — body is plain text with header |
| `memory-user` | `env.CachedUserInstructions` | `<memory scope="user">…</memory>` |
| `memory-project` | `env.CachedProjectInstructions` | `<memory scope="project">…</memory>` |

### Ad-hoc enqueues

Beyond providers, callers can `Service.Enqueue(body)` an ad-hoc reminder
that ships on the next user message:

| Event | Source | Triggered by |
|-------|--------|--------------|
| SessionStart hook | `outcome.AdditionalContext` | `app.fireStartupHooks` |
| UserPromptSubmit hook | `outcome.AdditionalContext` | `app.checkPromptHook` |
| `/skills` toggle | re-emit all providers | `update.go: SkillCycleMsg` handler |

Subagents do not use the registry; their first user message is augmented by
`subagent.collectSubagentReminders()` which builds the same shapes inline
from skills + memory bodies and attaches them via `reminder.AttachToContent`.

### Why this exists

| Benefit | How |
|---------|-----|
| System prompt cache stays valid all session | dynamic content never touches the system prompt |
| Memory edits don't blow up cache | a new reminder rides on the next user message; system prompt unchanged |
| Skill state toggles are cheap | enqueue a new reminder; no system prompt rebuild |
| PostCompact can recover state | re-emit all providers; skills/memory reappear on next user turn |

## Progressive loading

Skills, agents, and identities all use the same disclosure pattern:

| Level | When | Content |
|-------|------|---------|
| 1 | Always (skills via system-reminder; agents in Agent tool description) | Name + one-line description |
| 2 | On invocation / spawn | Full body loaded from `.md` file |
| 3 | On demand inside the body | Resource files (scripts, references, AGENT.md) |

This keeps the always-on context small. The full body of a skill or agent
only enters the LLM context when the user (or LLM) explicitly invokes it.

## Identity (custom personas)

Identity is the only slot that lets the user fully replace its default
content. Identities are markdown files:

```
~/.gen/identities/<name>.md           # user-level
.gen/identities/<name>.md             # project-level (overrides user)
```

Each file has frontmatter + body:

```markdown
---
name: ml-engineer
description: ML engineering specialist (PyTorch, JAX)
---

You are an ML engineer assistant ...

# Tone
...
```

The body lands directly in slot 0, replacing `prompts/identity.txt`.

**What belongs in an identity body:** persona / role definition, tone,
domain-specific behavior, code style preferences.

**What does NOT belong:** policy / security rules, git safety, tool usage
guidelines, task management. Those live in their own slots and always
apply, regardless of which identity is active.

### Activation flow

```
settings.identity ("ml-engineer")
        │
        ▼
identity.Registry.Active("ml-engineer")
        │  resolves user/project files; project wins
        ▼
Identity.Body  (markdown, no frontmatter)
        │
        ▼
BuildParams.IdentityText  →  system.WithIdentity(body)
        │
        ▼
core.System slot 0 replaced
```

Empty / missing / unknown name → `Active()` returns `""` → catalog uses the
built-in default. No errors; the user always gets a working prompt.

### `/identity` command

The `/identity` slash command unifies three actions:

| Form | Action |
|------|--------|
| `/identity` | Open read-only selector overlay |
| `/identity create [name-hint]` | Inject create workflow as PendingInstructions |
| `/identity edit <name>` | Inject edit workflow with target file |

The selector exposes `Shift+N` (create) and `Shift+E` (edit) hotkeys that
dispatch through the same handler. Workflow templates live in
`internal/command/builtin/identity-{create,edit}.md` (embedded), are loaded
by `command.BuiltinWorkflow(name)`, and instruct the agent to use
`AskUserQuestion` when intent is unclear, then write/edit the file using
its normal Read / Write / Edit tools.

There is no in-UI form or external editor invocation — file authoring is
the agent's responsibility, with the user supplying intent.

The user-level directory (`~/.gen/identities/`) and its `README.md` are
auto-created on startup (`identity.EnsureUserDir`) so the create workflow
has a format spec to read.

## Subagent identity replacement

For subagents, the identity slot is replaced entirely by a charter built
from the agent's config:

```go
// internal/subagent/executor_prompt.go
brief := system.SubagentBrief{
    AgentName:       config.Name,
    Description:     config.Description,
    Mode:            string(permMode),
    ToolConstraints: config.AllowTools.ConstrainedDisplayNames(),
    CustomPrompt:    config.GetSystemPrompt(),  // AGENT.md body
}
sys := system.Build(core.ScopeSubagent,
    system.WithSubagentIdentity(brief),
    ...
)
```

The brief renders as:

```xml
<identity mode="explore">
You are a code-reviewer subagent operating inside Gen Code.
Role: Reviews code changes for bugs, security, performance.

Operational scope: read-only research; do not modify files or run shell commands.
Tool constraints: Bash limited to git diff*, git log*, ...

{AGENT.md body}
</identity>
```

Subagents inherit policy and guidelines from the same templates as the main
agent. Identity is replaced via `WithSubagentIdentity`; main-only guidelines
(`tasks`, `questions`) are dropped; memory and skills ride on the subagent's
first user message as `<system-reminder>` blocks (built by
`subagent.collectSubagentReminders`).

## Skill / Agent injection

### Skills (system-reminder channel)

`skill.Registry.PromptSection()` returns a body listing **active** skills
only (state machine: Active / Enable / Disable, controlled by user via
`/skills`). The reminder service registers a `skills-directory` provider
that emits this body inside `<system-reminder>` on the first user message
and again after every PostCompact. Inactive skills are absent; full skill
bodies are loaded only when the LLM invokes the `Skill` tool or the user
activates via `/<skill-name>` slash command (which prepends the body to the
user message — see "Skill invocation" below).

### Agents (Agent tool description channel)

`subagent.Registry.GetAgentsSection()` returns a body listing all enabled
agent types with name + description + tool list. The body is embedded
directly into the `Agent` tool's description by `agentToolSchema(directory)`
when tool schemas are built. Subagents call the same machinery with an
empty directory (no recursive spawning), so their Agent tool — if
allow-listed — sees no available types.

### Skill invocation (inline in user message)

When the user types `/<skill-name>`, the slash-command handler stashes the
full skill body in `Skill.PendingInstructions`. On submit, `ConsumeInvocation`
prepends that body to the user message; the LLM receives the body inline as
part of the user turn. The body then lives in conversation history — cached
on subsequent turns and persisted across session resume — so the system
prompt stays untouched.

## Memory injection (system-reminder channel)

Memory comes from files (GEN.md / CLAUDE.md) loaded by `system.LoadMemoryFiles`
into `env.CachedUserInstructions` and `env.CachedProjectInstructions`:

```
~/.gen/GEN.md              ─┐
~/.claude/CLAUDE.md         ├── memory-user
~/.gen/rules/*.md          ─┘

.gen/GEN.md                ─┐
GEN.md (project root)       │
.claude/CLAUDE.md           ├── memory-project
CLAUDE.md (project root)    │
.gen/rules/*.md             │
.gen/GEN.local.md          ─┘
```

User and project memory are deduplicated (first source wins per level) and
joined with blank lines. The reminder service has two providers
(`memory-user`, `memory-project`) that wrap the bodies as
`<memory scope="user">…</memory>` and `<memory scope="project">…</memory>`,
respectively. Both ride inside `<system-reminder>` on the first user
message and after every PostCompact — the system prompt itself never sees
them, so memory edits don't invalidate the prompt cache.

## File map

| File | Role |
|------|------|
| `internal/core/section.go` | `Section`, `Slot`, `Scope`, `Source` types |
| `internal/core/system.go` | `System` interface (Use / Drop / Refresh / Sections / Prompt) |
| `internal/core/system_impl.go` | Default implementation; per-section + whole-prompt caching |
| `internal/core/system/builder.go` | `Build(scope, opts...)` entry point |
| `internal/core/system/catalog.go` | All section factories + `wrap()` envelope helper |
| `internal/core/system/memory.go` | `LoadMemoryFiles`, `LoadInstructions` |
| `internal/core/system/prompts/` | Embedded `.txt` templates (identity, policy, guidelines, compact) |
| `internal/identity/` | Identity registry, file parser, template generator |
| `internal/skill/registry.go` | `PromptSection`, `GetSkillInvocationPrompt` |
| `internal/subagent/registry.go` | `GetAgentsSection` (consumed by Agent tool description) |
| `internal/subagent/executor.go` | `collectSubagentReminders` builds memory + skills system-reminders for subagent first-user-message |
| `internal/subagent/executor_prompt.go` | `buildBrief` for `WithSubagentIdentity` |
| `internal/agent/build.go` | `BuildParams.IdentityText`, `AgentDirectory`; no memory/skills knobs anymore |
| `internal/reminder/reminder.go` | `Service`, `Provider`, `Wrap`, `AttachToContent` |
| `internal/tool/schema_agent.go` | `agentToolSchema(directory)` builds Agent tool description with directory |
| `internal/app/agent.go` | `wireReminderProviders` registers the harness reminder providers |
| `internal/command/builtin/identity-{create,edit}.md` | Embedded workflow templates |

## Sample API call shape

After the refactor, the picture for a single API call looks like three
distinct payloads. The system prompt is short and stable (5 sections, only
`environment` is volatile); reminders ride in user messages; agents live in
the tools array.

### `system` field (Channel A)

```text
You are Gen Code, an interactive AI assistant ...

# Tone / Output / Behavior / Scope / Code conventions
...

<policy>
Defensive security only ...
</policy>

<guidelines name="tools">...</guidelines>
<guidelines name="tasks">...</guidelines>
<guidelines name="questions">...</guidelines>
<guidelines name="git">...</guidelines>

<environment>
date: 2026-05-04
cwd: /Users/myan/Workspace/ideas/gencode
git: yes
platform: darwin/arm64
model: claude-sonnet-4-20250514
</environment>
```

### `tools` field (Channel B) — Agent tool description excerpt

```text
Launch a subagent for complex work that benefits from separate context or parallel execution.

Available agent types for the Agent tool:

- general-purpose: General multi-step agent
  Tools: *
- code-reviewer: Reviews code changes without mutating the workspace
  Tools: Read, Glob, Grep

When using the Agent tool, specify a subagent_type parameter to select which
agent type to use. If omitted, the general-purpose agent is used.
...
```

### First user message (Channel C — system-reminder bodies)

```text
{user's literal input here, e.g. "implement feature X"}

<system-reminder>
Use the Skill tool to invoke these capabilities:

- git: Git workflow automation
- review: Review a pull request

Invoke with: Skill(skill="name", args="optional args")
</system-reminder>

<system-reminder>
<memory scope="user">
Always use tabs for indentation.
</memory>
</system-reminder>

<system-reminder>
<memory scope="project">
This is a Go project using Bubble Tea.
</memory>
</system-reminder>
```

### Subagent (`code-reviewer`, explore mode)

```text
# system field
<identity mode="explore">
You are a code-reviewer subagent operating inside Gen Code.
Role: Reviews code changes for bugs, security, performance, and style.

Operational scope: read-only research; do not modify files or run shell commands.
Tool constraints: Bash limited to git diff*, git log*, git show*, git status*

{AGENT.md body}
</identity>

<policy>... (same as main) ...</policy>
<guidelines name="tools">...</guidelines>
<guidelines name="git">...</guidelines>
<!-- no <guidelines name="tasks"> or <guidelines name="questions"> -->

<environment>...</environment>

# tools field — Agent tool (if allow-listed) shows no directory:
# "When using the Agent tool, specify subagent_type ..."
# (no "Available agent types" block — subagents do not recursively spawn)

# first user message attached by collectSubagentReminders:
{parent's brief prompt}

<system-reminder>
Use the Skill tool to invoke these capabilities:
- review: Review a pull request
...
</system-reminder>

<system-reminder>
<memory scope="user">...</memory>
</system-reminder>

<system-reminder>
<memory scope="project">...</memory>
</system-reminder>
```

## See also

- [`subagent.md`](subagent.md) — subagent execution flow
- [`skill-system.md`](skill-system.md) — skill registry and invocation tool
- [`features/10-agents.md`](features/10-agents.md) — agent definition and lifecycle
- [`features/16-memory.md`](features/16-memory.md) — memory file conventions
