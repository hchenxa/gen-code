---
package: github.com/genai-io/gen-code/internal/secret
layer: infrastructure
---

# secret

Filesystem-backed key/value store under `~/.gen/secrets.json` for
sensitive values (API keys, tokens) that should not live in
`settings.json` or `providers.json`.

## Purpose

Provider credentials, marketplace tokens, and other secrets need a
home that is:

- not committed (the file is gitignored at the user level)
- separated from settings so that sharing `settings.json` across
  machines doesn't leak credentials
- accessible without prompting the OS keychain

`internal/secret` is that home. Provider packages call `secret.Default()`
to fetch and store API keys; the user-facing slash commands
(`/provider`) edit the file through this package.

## Contract

```go
package secret

// Store is a thread-safe map persisted to ~/.gen/secrets.json.
type Store struct { /* unexported */ }

func Default() *Store
func (s *Store) Get(key string) (string, bool)
func (s *Store) Set(key, value string) error
func (s *Store) Delete(key string) error
func (s *Store) Keys() []string
```

Returns `*Store` directly — no `Service` interface needed. The
package-level `Default()` lazily initializes a singleton tied to
`~/.gen/secrets.json`.

### Notes

- File permissions are 0600 on create.
- The file is plain JSON, not encrypted. The threat model is local
  multi-user isolation, not at-rest secrecy. Users who need
  encryption-at-rest should integrate an OS keychain externally.

## Lifecycle

- First call to `Default()` creates `~/.gen/secrets.json` if missing.
- Reads and writes are mutex-protected; each `Set` re-serializes the
  whole file (atomic write).

## Tests

No package-level tests today; logic is small and exercised through
provider integration paths.

## See Also

- Code: `internal/secret/`
- Consumer: [`packages/llm.md`](llm.md) (provider API keys).
- Layer: `infrastructure`.
