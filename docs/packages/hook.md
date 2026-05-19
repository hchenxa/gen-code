---
package: github.com/genai-io/gen-code/internal/hook
layer: feature
---

# hook

Executes user-defined hooks at well-known application events (tool calls,
session lifecycle, permission requests, file changes, …) and merges their
outcomes back into the calling code path.

## Purpose

Hooks are how users extend Gen Code without writing Go: a shell command, an
LLM prompt, an HTTP endpoint, or an in-memory callback runs at a named
event. The engine resolves which hooks fire (matchers), runs them
synchronously or asynchronously, and reduces their structured outputs into
a single `HookOutcome` for the call site.

This package is intentionally separate from `core` agent lifecycle: hook
events are **application-layer** concerns (`PreToolUse`, `Stop`,
`SessionStart`, …), not part of the agent loop's primitives.

## Contract

One role interface plus the concrete engine — no producer-side union.

| Role | Shape | Consumers |
|---|---|---|
| **Handler** — fire hooks at an application event | `interface{ Execute; ExecuteAsync; HasHooks; StopHookActive }` | agent loop, slash-command approval flow, compaction, file watcher, worktree / plugin / mcp / subagent |
| **Engine configuration + status** (no interface) | concrete `*Engine` — `Set*` runtime knobs, `ClearSessionHooks`, `SetAuditCallback`, `CurrentStatusMessage` | app composition root (Set\* methods) and the TUI view (CurrentStatusMessage). Each is one caller; an interface would be ceremony. |

`*Engine` is the only implementation; consumers narrow by declaration:

```go
var handler hook.Handler = hook.DefaultEngine()
handler.Execute(ctx, hook.PreToolUse, input)
```

```go
package hook

// Handler is what callers depend on to fire hooks at application
// events and merge outcomes. Modeled on http.Handler — pass an event
// (the request analogue) and receive the merged HookOutcome (the
// response analogue). Most callers depend on this surface only.
type Handler interface {
    Execute(ctx context.Context, event EventType, input HookInput) HookOutcome
    ExecuteAsync(event EventType, input HookInput)
    HasHooks(event EventType) bool
    StopHookActive() *bool
}

// *Engine is the only implementation; satisfies Handler and carries
// the Set* / status methods used by the app composition root and the
// TUI view layer.
type Engine struct { /* unexported */ }

var _ Handler = (*Engine)(nil)

// Configuration methods on *Engine (used by the app composition root).
func (e *Engine) SetSettings(*setting.Settings)
func (e *Engine) SetLLMCompleter(LLMCompleter, string)
func (e *Engine) SetTranscriptPath(string)
func (e *Engine) SetCwd(string)
func (e *Engine) SetPermissionMode(string)
func (e *Engine) SetAsyncHookCallback(AsyncHookCallback)
func (e *Engine) SetAuditCallback(AuditCallback)
func (e *Engine) ClearSessionHooks()

// Status read on *Engine (used by the TUI view).
func (e *Engine) CurrentStatusMessage() string

// Package-level access.
func Initialize(opts Options)
func DefaultEngine() *Engine
func SetDefaultEngine(e *Engine)   // test-only
func ResetDefaultEngine()          // test-only
```

### Why one interface, not the previous 16-method god `Service`

The previous `hook.Service` bundled 16 methods (the largest god union
in the repo) and forced an `Engine() *Engine` escape hatch because
`SetAuditCallback` lived only on `*Engine`, not on `Service`.

Deleting `Service` and exposing `*Engine` directly (with one role
interface for the genuinely-narrow fire surface) drops:

- The escape hatch — callers that need `SetAuditCallback` just use
  `*Engine` like every other configurator.
- Six **dead methods** with zero non-test callers: `FilterToolCalls`,
  `Wait`, `AddSessionFunctionHook`, `AddRuntimeFunctionHook`,
  `SetPromptCallback`, `SetEnvProvider`. `Wait` and `FilterToolCalls`
  were deleted entirely; the other four stay on `*Engine` only as
  test infrastructure (no production caller).
- The two-flavor accessor pattern (`Default` panics, `DefaultIfInit`
  is nil-tolerant). Replaced by a single `DefaultEngine()` that
  returns an empty-but-non-nil engine until `Initialize` runs.

`CurrentStatusMessage` does not get its own interface — one method
with one caller (TUI view) is exactly the speculative abstraction
[Rule 3](TEMPLATE.md#contract-rules) warns against.

### Remaining Known Violations

None.

## Internals

- `Engine` (`engine.go`) is the only implementation. It owns:
  - `*hookStore` — settings-loaded hooks plus session/runtime function hooks
  - `*statusTracker` — currently-active hook status message for the TUI
  - mutable knobs (`settings`, `cwd`, `transcriptPath`, `permissionMode`,
    `llmCompleter`, `httpClient`, `promptCallback`, `asyncCallback`,
    `auditCallback`, `envProvider`) under one `sync.RWMutex`
  - a `sync.WaitGroup` for fire-and-forget detached goroutines
- Executors live in `executors_command.go` / `executors_http.go` /
  `executors_llm.go`. Each takes a matched hook + `HookInput` and returns a
  `HookOutcome`.
- `matcher.go` resolves which hooks fire for an event by matching against
  patterns (tool name globs, regex, exact match).
- `audit` callback is the single observation seam used by the session
  recorder to write one `hook.fired` transcript record per invocation —
  the hook package does not import `transcript`.

## Lifecycle

- Construction: `Initialize(Options{...})` runs at app startup. Engine is a
  singleton.
- Per-event execution: `Execute` is synchronous; matching hooks run in
  configuration order, outcomes are reduced left-to-right, and the first
  `ShouldContinue=false` short-circuits.
- Async hooks: `ExecuteAsync` plus the `Command.Async` flag on individual
  hooks both spawn detached goroutines tracked by an internal
  `sync.WaitGroup`. `Wait()` blocks until all detached goroutines drain
  (used on app shutdown).
- Concurrency: all `Set*` methods are mutex-guarded reads/writes; hook
  execution reads under RLock.

## Tests

```
internal/hook/hooks_test.go         — large table of execution
                                       scenarios (sync, async, matchers,
                                       outcome merging, permission paths).
internal/hook/hooks_test.go         — also covers types/registry roundtrips.
```

The 49 KB test file is the canonical reference for how outcomes merge and
how Claude-Code-compatible hook configs are interpreted.

## See Also

- Code: `internal/hook/`
- Concepts: [`concepts/permission-model.md`](../concepts/permission-model.md)
- Concepts: [`concepts/extension-model.md`](../concepts/extension-model.md)
- Related: [`packages/tool.md`](tool.md) (tool permission gate), [`packages/setting.md`](setting.md) (where hooks are loaded from)
- Layer: `feature` (see [`reference/dependency-rules.md`](../reference/dependency-rules.md))
