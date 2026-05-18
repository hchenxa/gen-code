# Writing a Skill

A skill is a markdown file the model can be made aware of, or that the
user can invoke via a slash command. Skills are the simplest extension
surface in Gen Code — one file, no install step.

For the system-level design see [`packages/skill.md`](../packages/skill.md)
and [`concepts/extension-model.md`](../concepts/extension-model.md).

## Where to Put It

| Scope | Directory | When to use |
|---|---|---|
| Project | `<project>/.gen/skills/<name>/SKILL.md` | Skill tied to this project (lives in the repo). |
| User | `~/.gen/skills/<name>/SKILL.md` | Personal skill shared across projects. |
| Claude-compat | `<project>/.claude/skills/<name>/SKILL.md` or `~/.claude/skills/<name>/SKILL.md` | If you also use Claude Code. |

Project scope wins over user; user wins over Claude-compat. The directory
name is the skill name.

## Minimal Example

`./.gen/skills/release-notes/SKILL.md`:

```markdown
---
name: release-notes
description: Draft release notes from recent commits
allowed-tools: [Bash, Read, Write]
argument-hint: <version-tag>
---

You are drafting release notes for the version supplied as the argument.

1. Run `git log --oneline <prev-tag>..<arg>` to list commits.
2. Group them into Added / Changed / Fixed sections.
3. Write the result to `CHANGELOG.md` under a `## [<arg>]` heading.
```

## Frontmatter Fields

| Field | Required | Purpose |
|---|---|---|
| `name` | yes | Slash command name (lowercase, kebab-case). |
| `description` | yes | One-liner shown in selectors and the system prompt. |
| `namespace` | no | Group prefix (e.g. `git`, `jira`); invoked as `/git:branch-cleanup`. |
| `allowed-tools` | no | Restrict the skill's tool subset. Default = all tools. |
| `argument-hint` | no | Hint shown after `/skill-name ` in the autocompleter. |

## Bundled Resources (Agent Skills spec)

A skill directory may also contain:

```
./.gen/skills/release-notes/
├── SKILL.md
├── scripts/             # Optional helper scripts the skill may invoke
├── references/          # Optional reference files inlined when active
└── assets/              # Optional binary assets (images, fonts)
```

Resources are lazy-loaded — Gen Code does not read them until the skill
is actually invoked.

## The Three Skill States

Each skill is in one of three states; cycle with `/skill`:

| State | Effect |
|---|---|
| `disable` | Hidden. Not in the slash-command list. Model unaware. |
| `enable` | Visible as a slash command. Model unaware (user-invoked only). |
| `active` | Visible as a slash command *and* listed in the model's
  system prompt so it can invoke the skill on its own. |

Default for newly discovered skills is `enable`. State is persisted in
`~/.gen/skills.json` (user level) or `<project>/.gen/skills.json`
(project level).

## Trying It

1. Save the SKILL.md as above.
2. Run `gen` (or restart your session — skills are discovered at startup).
3. Type `/skill` to see the new entry and cycle its state to `enable`
   or `active`.
4. Invoke it directly: `/release-notes v1.18.0`.

## Sharing It

- Commit `./.gen/skills/<name>/` into the project repo — collaborators
  get it on next `gen` run.
- For cross-project skills, put them under `~/.gen/skills/` and back
  them up (the directory is just markdown).
- To distribute as part of a bundle, see [Writing a plugin](writing-a-plugin.md).

## Common Pitfalls

- **Description too long.** It is rendered into the system prompt for
  active skills; long descriptions waste context. Aim for one line.
- **`allowed-tools` filters silently.** If the skill assumes `Write` but
  `allowed-tools: [Read]` is set, the tool call will be denied — the
  skill won't say why.
- **Project state shadows user state.** Toggling a skill at user level
  has no effect if a `<project>/.gen/skills.json` entry exists for the
  same name.

## See Also

- [`packages/skill.md`](../packages/skill.md) — skill loader internals.
- [`concepts/extension-model.md`](../concepts/extension-model.md) — how
  skills relate to subagents, slash commands, plugins.
- [`writing-a-plugin.md`](writing-a-plugin.md) — bundle multiple skills.
