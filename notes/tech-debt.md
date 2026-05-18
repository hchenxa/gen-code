# Technical Debt

This file tracks structural follow-ups that are not tied to a single feature.

## Current Candidates

- Continue reducing root `internal/app` file size by lifecycle responsibility.
- Document extension reload behavior across skills, commands, MCP, plugins, and
  settings.

### Code refactors flagged by `docs/packages/*/Known Violations`

- **God `Service` interfaces.** Split per the per-package suggestions:
  `hook` (16 methods), `plugin` (21), `setting` (14), `skill` (12),
  `session` (11), `agent` (11), `cron` (10), `mcp` (10), `subagent` (9),
  `command` (7), `llm` (8), `task` (8), `tool` (6). Define narrow
  consumer-defined interfaces alongside the concrete `*service` / `*Engine`
  / `*Registry`; let each call site narrow to what it needs.
- **Escape-hatch methods on Service interfaces.** Drop `.Engine()` /
  `.Registry()` / `.GetStore()` from the `Service` contracts; have
  consumers depend on either a narrower interface or the concrete type
  directly. Three known call sites today: `internal/app/model.go`
  (`Hook.Engine()`), `internal/app/agent.go` (`MCP.Registry()`),
  `internal/app/update.go` (`Session.GetStore()`).
- **Singleton via `Default()` / `DefaultIfInit()`.** Move construction
  into `cmd/gen` and pass the concrete service into
  `internal/app.newServices()` instead of pulling from each package's
  package-level singleton. Eliminates the two-flavor accessor pattern
  (`Default` panics; `DefaultIfInit` is nil-tolerant).
- **`skill.AddPluginSkills` uses anonymous struct slice.** Replace with
  a named `PluginSkillSource` type so the exported API is
  go-doc-readable.

### Adapter cleanups

- **Extract `core.Image` adapter out of `internal/image`.** `ToProviderData`
  and `ReadImageToProviderData` are the only reasons `image` cannot be
  `infrastructure`. Move them to a small adapter (e.g. consumer code in
  `internal/app/input` or a new `internal/image/adapter` subpackage that
  is itself `feature`) so `internal/image` can be reclassified back to
  `infrastructure`.

### Tests

- Unit test missing for `internal/agent.BuildParams` → `core.Config`
  translation (flagged in `docs/packages/agent.md`).
- `internal/agent/` has no package-local test file; coverage is
  end-to-end only.

### Documentation gaps (resolved 2026-05-18 in the docs/restructure branch)

- ~~`docs/guides/` only contains `explore-mode.md`.~~ Added
  `getting-started.md`, `writing-a-skill.md`, `writing-a-subagent.md`,
  `writing-a-plugin.md`.
- ~~`docs/design/decisions/` is empty.~~ Added
  `0001-layered-package-architecture.md`.
- ~~Infrastructure packages (`log`, `secret`, `filecache`, `markdown`)
  have no `docs/packages/*.md` page.~~ Added all four.

### Remaining documentation work

- `docs/packages/ui.md` Contract section is complete but the
  per-subpackage tour (`input/`, `conv/`, `hub/`, `trigger/`, `kit/`)
  is still squeezed into one page. Could split into
  `packages/ui/<sub>.md` once any single subpackage grows enough to
  warrant it.
