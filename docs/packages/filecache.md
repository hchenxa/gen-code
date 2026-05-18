---
package: github.com/genai-io/gen-code/internal/filecache
layer: infrastructure
---

# filecache

LRU index of file paths the agent has read recently, plus the "file
restore" feature that re-injects truncated/displaced file content back
into context when the model loses track of it.

## Purpose

Two things, both small:

1. **Touch tracking.** Every time a tool reads or writes a file, the
   tool calls `cache.Touch(path)`. The cache keeps the 20 most-recent
   timestamps in an LRU map.
2. **Context restore.** When the agent's context drops below a
   threshold (compaction or fresh resume), the cache's most-recent
   entries are eligible for `restore.Build()` which re-reads the top-N
   files and produces a synthetic injection block. Cap is 5 files /
   5,000 lines per file / 50,000 total lines.

## Contract

```go
package filecache

type Cache struct { /* unexported */ }

func New() *Cache
func (c *Cache) Touch(filePath string)
func (c *Cache) RecentEntries() []Entry  // newest first, max 20

// Restore is a separate function that consumes a Cache and produces
// the inject block. Constants restoreMaxFiles, restoreMaxPerFile,
// restoreMaxTotal define the limits.
func Build(c *Cache) string  // see restore.go
```

No `Service` interface; concrete `*Cache` is the contract. Callers
typically hold one cache per session.

## Lifecycle

- A `*Cache` is created at session start and lives for the session.
- `Touch` is goroutine-safe (mutex).
- `Build` is called by the compactor and by session-restore code.

## Tests

No package-level tests; behavior is exercised end-to-end via the
compaction and restore paths in `internal/app/conv/`.

## See Also

- Code: `internal/filecache/`
- Consumers: `internal/tool/fs/` (Touch on Read/Write/Edit),
  `internal/app/conv/compact.go` (Build on compaction).
- Concepts: [`concepts/harness-channels.md`](../concepts/harness-channels.md)
  covers the compaction flow.
- Layer: `infrastructure`.
