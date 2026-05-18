---
package: github.com/genai-io/gen-code/internal/log
layer: infrastructure
---

# log

Process-wide structured logger built on `go.uber.org/zap`, plus a
development-mode sidecar that dumps per-turn request/response artifacts
to a configurable directory.

## Purpose

Two writers, one package:

1. **Structured log** — `log.Logger()` returns a `*zap.Logger`. Used
   across the codebase for warnings, debug traces, and audit events.
   Output is suppressed by default; setting `GEN_DEBUG=1` enables it,
   with `lumberjack` rotation behind it.
2. **Dev directory** — when `DEV_DIR` is set, per-turn LLM request /
   response / chunk payloads are written under that path for offline
   inspection. Used by tests and by the inspector's replay flow.

## Contract

```go
package log

func Init() error              // initialize from env; idempotent
func Logger() *zap.Logger      // process-wide logger; never nil
func TurnCount() int           // monotonic turn counter (TUI sets it)
func IncrementTurn()           // called once per turn boundary

// Dev directory helpers
func DevEnabled() bool
func WriteRequest(payload any) error
func WriteResponse(payload any) error
func WriteChunk(payload any) error

// Queue logging is a small named ring (see queuelog.go) used by
// trigger-side debugging.
type Loggable interface { ... } // see loggable.go
```

No types are exported beyond these helpers; concrete state lives in
package-level vars guarded by a mutex.

## Lifecycle

- `log.Init()` runs once at app startup (from `internal/app/init.go`).
- After `Init`, `Logger()` is safe to call from any goroutine.
- `DEV_DIR` is read once at `Init` time. Changing it after startup has
  no effect.

## Tests

No package-level tests; behavior is exercised through callers and
manual `GEN_DEBUG=1` runs.

## See Also

- Code: `internal/log/`
- Consumers: every package that needs to emit a warning or debug trace.
- Layer: `infrastructure` — no `internal/*` imports.
