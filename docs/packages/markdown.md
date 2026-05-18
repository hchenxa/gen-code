---
package: github.com/genai-io/gen-code/internal/markdown
layer: infrastructure
---

# markdown

YAML frontmatter parser for markdown files. The smallest infrastructure
package in the repo: one function, one file.

## Purpose

Skill, subagent, identity, and slash-command files all share a common
shape: a `---` YAML frontmatter block followed by a markdown body. This
package extracts the two halves so each consumer can parse the YAML
with its own schema.

## Contract

```go
package markdown

// ParseFrontmatterFile reads a markdown file and returns
// (frontmatter, body). frontmatter is the raw YAML text between the
// opening and closing --- delimiters; body is everything after.
// If no frontmatter is found, frontmatter is "" and body is the
// entire file contents.
func ParseFrontmatterFile(path string) (frontmatter, body string, err error)
```

One function, no state, no interface. The body of the function is also
small (~30 lines). Consumers `gopkg.in/yaml.v3`-decode the frontmatter
into their own struct.

## Lifecycle

Stateless. Safe for concurrent use (each call opens its own file).

## Tests

No package-level tests; the function is exercised end-to-end every
time `gen` starts (every skill / agent / identity / command file is
parsed through it).

## See Also

- Code: `internal/markdown/`
- Consumers: [`packages/skill.md`](skill.md), [`packages/subagent.md`](subagent.md), [`packages/identity.md`](identity.md), [`packages/command.md`](command.md).
- Layer: `infrastructure`.
