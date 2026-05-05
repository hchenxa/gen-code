# Manual Test Plan — v1.17.0

Cumulative manual regression for the v1.16.0 → v1.17.0 window plus uncommitted
fixes on `main`. Each section is self-contained: every test starts with a
`reset` (rebuild + clear last round's artifacts) and ends with where to
inspect the captured files.

> All shell commands assume the working directory is `/Users/myan/Workspace/ideas/gencode`.
> Sandbox lives at `/tmp/gen-test`; LLM request/response artifacts land in `/tmp/gen-test/devdir/`.

---

## 0. One-time environment setup

```bash
mkdir -p /tmp/gen-test/devdir
mkdir -p /tmp/gen-test/proj && (cd /tmp/gen-test/proj && git init -q)

# Inject three helpers into the current shell:
# - reset    : rebuild + clear last round's artifacts + drop sentinel files
# - gen-test : launch gen in the sandbox; DEV_DIR auto-captures every LLM request/response
# - show     : list the JSON artifacts captured this round
cat > /tmp/gen-test/env.sh <<'EOF'
GEN_REPO=/Users/myan/Workspace/ideas/gencode
SANDBOX=/tmp/gen-test
DEVDIR=$SANDBOX/devdir
PROJ=$SANDBOX/proj

# Earlier rounds of this doc set `alias gen-test=...`. zsh expands aliases
# during function definitions, so leaving one in place yields:
#   parse error near `()'
# Drop any stale aliases before (re)defining the helpers.
unalias reset gen-test show 2>/dev/null

reset() {
  ( cd "$GEN_REPO" && make build ) || return 1
  rm -rf "$DEVDIR"/* "$PROJ/gen_debug.log" 2>/dev/null
  rm -f  "$PROJ/GEN.md" "$PROJ/CLAUDE.md" "$PROJ/.gen/settings.json" 2>/dev/null
  rm -rf "$PROJ/.gen/commands" "$PROJ/.gen/identities" 2>/dev/null
  echo "✓ rebuilt + sandbox cleared ($DEVDIR)"
}

gen-test() {
  ( cd "$PROJ" && DEV_DIR="$DEVDIR" GEN_DEBUG=1 "$GEN_REPO/bin/gen" "$@" )
}

show() {
  echo "Artifacts in $DEVDIR:"
  ls -lt "$DEVDIR"/*.json 2>/dev/null | head -20
  echo
  echo "Quick-look helpers:"
  echo "  jq '.system_prompt' \"\$(ls -t $DEVDIR/*-request.json | head -1)\""
  echo "  jq -r '.messages[] | select(.role==\"user\") | .content' \"\$(ls -t $DEVDIR/*-request.json | head -1)\""
}
EOF

# Load helpers into the current shell.
source /tmp/gen-test/env.sh
```

For new terminals later: `source /tmp/gen-test/env.sh`.

---

## 1. Identity system (v1.16.0)

### 1.1 Selector

```bash
reset
gen-test
```
```
type:    /identity
expect:  selector pops up, default is listed
keys:    ↑/↓ to move, Esc to dismiss
```
**Inspect**: UI only — no file artifacts.

### 1.2 Create (command form)

```bash
reset
gen-test
```
```
type:  /identity create test-reviewer Strict Go reviewer focused on concurrency safety
wait for the workflow to finish, then exit
```
**Inspect**:
```bash
cat ~/.gen/identities/test-reviewer.md   # frontmatter has name: test-reviewer
show                                     # DEV_DIR shows the workflow inlined into the user message
```

### 1.3 Create (Shift+N)

```bash
reset
gen-test
```
```
type:  /identity
keys:  Shift+N
follow the prompts
```
**Inspect**: `ls ~/.gen/identities/` — the new file should appear.

### 1.4 Edit (both paths + error prompts)

```bash
reset
gen-test
```
```
path A: /identity edit test-reviewer
path B: /identity → select test-reviewer → Shift+E
no name:        /identity edit       # expects "Usage: /identity edit <name>"
unknown sub:    /identity foo        # expects "Usage: /identity [create | edit <name>]"
```
**Inspect**: `diff` `~/.gen/identities/test-reviewer.md` before vs. after.

### 1.5 Project-scoped identity

```bash
reset
mkdir -p $PROJ/.gen/identities
cat > $PROJ/.gen/identities/proj-only.md <<'EOF'
---
name: proj-only
description: project scoped
---

You are project-scoped only.
EOF
gen-test
```
```
type:    /identity
expect:  the list shows both user-level and proj-only (labelled "project")
select:  proj-only → Enter, then exit
```
**Inspect**:
```bash
jq .identity $PROJ/.gen/settings.json    # expects "proj-only"
```

### 1.6 Identity takes effect immediately (key regression)

Two bugs converged here: before v1.17.0 `Settings.Clone()` dropped the
`Identity` field, and `setActiveIdentity` did not hot-patch the running
main agent's system prompt. Both paths must pass.

**Path A — identity already in settings at startup (verifies the Clone fix)**:
```bash
reset
# Assumes ~/.gen/identities/go-reviewer.md exists.
tmp=$(mktemp) && jq '.identity = "go-reviewer"' ~/.gen/settings.json > $tmp && mv $tmp ~/.gen/settings.json
gen-test
```
```
type:  who are you
exit
```

**Path B — switch identity mid-session (verifies hot-swap)**:
```bash
reset
gen-test
```
```
type:    /identity → select go-reviewer → Enter
type:    who are you
exit
```

**Inspect** (same check for both paths):
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.system_prompt' "$LATEST" | head -3
# Expect first line: "You are a strict Go code reviewer..." (from go-reviewer.md)
# If you still see "You are Gen Code, an interactive AI assistant..." → identity did not apply
```

### 1.7 Subagent identity replacement

```bash
reset
gen-test
```
```
type:  use the Agent tool to launch a subagent with subagent_type=general-purpose,
       prompt="echo hello and stop"
exit after the subagent returns
```
**Inspect**:
```bash
for f in $DEVDIR/*-request.json; do
  echo "=== $f ==="
  jq -r '.system_prompt' "$f" | grep -E "<identity>|<agents>|name=\"tasks\"|name=\"questions\"" | head
done
# Subagent system_prompt must NOT contain <agents>, name="tasks", or name="questions".
```

---

## 2. Reminder system (v1.17.0 core)

### 2.1 Memory injection

```bash
reset
cat > $PROJ/GEN.md <<'EOF'
# Test Memory Sentinel
PROJECT_MEMORY_TOKEN_ABC123
EOF
gen-test
```
```
type:  hi
wait for the response, then exit
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" | grep -A2 "PROJECT_MEMORY_TOKEN_ABC123"
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" | grep -c "<system-reminder>"
# Expect: sentinel hits; reminder count >= 1
```

### 2.2 PostCompact re-injection

```bash
reset
cat > $PROJ/GEN.md <<'EOF'
PROJECT_MEMORY_TOKEN_POSTCOMPACT
EOF
gen-test
```
```
type:  hi
type:  /compact
wait for compaction to finish
type:  continue
exit
```
**Inspect**:
```bash
TARGET=$(ls -t $DEVDIR/*-request.json | sed -n '2p')   # first request after compact
jq -r '.messages[] | select(.role=="user") | .content' "$TARGET" | grep -E "<system-reminder>|PROJECT_MEMORY_TOKEN_POSTCOMPACT"
# Expect: reminder block and sentinel both reappear
```

### 2.3 SessionStart hook flows through the reminder channel

```bash
reset
mkdir -p $PROJ/.gen
cat > $PROJ/.gen/settings.json <<'EOF'
{
  "hooks": {
    "SessionStart": [{
      "hooks": [{
        "type": "command",
        "command": "echo '{\"hookSpecificOutput\":{\"additionalContext\":\"HOOK_TOKEN_XYZ789\"}}'"
      }]
    }]
  }
}
EOF
gen-test
```
```
type:  hi
exit
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
echo "system_prompt hits (expect 0): $(jq -r '.system_prompt' "$LATEST" | grep -c HOOK_TOKEN_XYZ789)"
echo "user message hits (expect >=1): $(jq -r '.messages[] | select(.role==\"user\") | .content' "$LATEST" | grep -c HOOK_TOKEN_XYZ789)"
```

### 2.4 Empty providers do not emit

```bash
reset      # reset already removed GEN.md / CLAUDE.md
gen-test
```
```
type:  hi
exit
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" \
  | grep -E "<system-reminder>\s*</system-reminder>"
# Expect: no output (empty reminder must not be emitted)
```

---

## 3. System prompt slot stability

```bash
reset
gen-test
```
```
type:  hi
type:  hello again
exit
```
**Inspect**:
```bash
FILES=($(ls -tr $DEVDIR/*-request.json | head -2))
diff <(jq -r '.system_prompt' "${FILES[0]}") <(jq -r '.system_prompt' "${FILES[1]}")
# Expect: identical (no diff output) — the cache prefix must not churn between turns
```

---

## 4. Skill / slash idempotency (uncommitted fix)

### 4.1 Direct skill invocation

```bash
reset
gen-test
```
```
type:  /git:my-prs   (or any enabled skill)
wait for the response, then exit
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" | head -3
# Expect first line: <command-name>git:my-prs</command-name>

LATEST_RESP=$(ls -t $DEVDIR/*-response.json | head -1)
echo "Skill tool calls (expect 0): $(jq -r '.. | .name? // empty' "$LATEST_RESP" | grep -c '^Skill$')"
```

### 4.2 Short-circuit verification (induced)

```bash
reset
gen-test
```
```
type:  /git:my-prs
after the response: please call the Skill tool again to load git:my-prs and confirm reload behavior
exit
```
**Inspect**:
```bash
grep -l "skill-already-loaded" $DEVDIR/*-request.json
# Expect: at least one tool result contains skill-already-loaded
```

### 4.3 Custom command

```bash
reset
mkdir -p $PROJ/.gen/commands
cat > $PROJ/.gen/commands/echo-args.md <<'EOF'
---
description: Echo arguments
---

Print exactly: ARGS=$ARGUMENTS
EOF
gen-test
```
```
type:  /echo-args hello world
exit
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" \
  | grep -E "<command-name>echo-args</command-name>|<custom-command name=\"echo-args\">|ARGS=hello world"
# Expect: all three lines hit
```

### 4.4 `/identity create` (name-with-space edge case)

```bash
reset
gen-test
```
```
type:  /identity create temp-test simple test
exit immediately (do not wait for the model)
```
**Inspect**:
```bash
LATEST=$(ls -t $DEVDIR/*-request.json | head -1)
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" \
  | grep "<command-name>identity create</command-name>"
# Expect: hit (verifies the PendingFullName fix)
```

---

## 5. Plugin async UX (uncommitted fix)

### 5.1 Spinner animation

```bash
reset
gen-test
```
```
type:    /plugin
keys:    switch to Installed tab → pick an installed plugin → trigger disable
expect:  bottom bar shows ◐ ◓ ◑ ◒ spinner cycling at ~80ms/frame
result:  "Disabled <name>"
re-enable, confirm "Enabled <name>"
```
**Inspect**: pure UI verification.

### 5.2 No tick compounding

```bash
reset
gen-test
```
```
type:    /plugin
action:  press disable/enable rapidly twice with no gap
expect:  spinner stays at the same frame rate (no 2× speed-up)
exit
```
**Inspect**: pure UI verification.

### 5.3 Install

```bash
reset
gen-test
```
```
type:    /plugin
action:  switch to Marketplaces / Discover → pick an uninstalled plugin → install
expect:  spinner stays up until done; "Installed <name>" appears
```
**Inspect**: `ls ~/.gen/plugins/` — the new plugin directory should appear.

---

## 6. Agent preload dedup (uncommitted fix)

### 6.1 Plain user message appears once

```bash
reset
gen-test
```
```
type:  HELLO_WORLD_SENTINEL_42
wait for the response, then exit
```
**Inspect**:
```bash
FIRST=$(ls -tr $DEVDIR/*-request.json | head -1)
echo "sentinel occurrences (expect 1, was 2 before fix): $(grep -c HELLO_WORLD_SENTINEL_42 "$FIRST")"
```

### 6.2 Skill path does not duplicate

```bash
reset
gen-test
```
```
type:  /git:my-prs
exit
```
**Inspect**:
```bash
FIRST=$(ls -tr $DEVDIR/*-request.json | head -1)
echo "skill-invocation count (expect 1): $(jq -r '.messages[] | select(.role=="user") | .content' "$FIRST" | grep -c '<skill-invocation')"
```

---

## 7. Smoke test

```bash
reset
gen-test
```
```
type each of these and confirm nothing crashes:
  /help
  /clear
  /model
  /plugin   (Esc to close)
  /skill    (Esc to close)
  /identity (Esc to close)
  please run echo hi via Bash
  please launch a subagent to run echo bg
  TaskList    (the LLM should retrieve the task list)
  /compact
exit
```
**Inspect**:
```bash
show       # browse all request/response JSON from this round
```

---

## One-shot summary check

Run after any round to glance at reminder / command-name / slot health:

```bash
cat > /tmp/gen-test/check.sh <<'EOF'
#!/usr/bin/env bash
DIR=${1:-/tmp/gen-test/devdir}
echo "Total requests: $(ls $DIR/*-request.json 2>/dev/null | wc -l)"
LATEST=$(ls -t $DIR/*-request.json 2>/dev/null | head -1)
[ -z "$LATEST" ] && { echo "no requests captured"; exit 1; }
echo
echo "=== latest: $LATEST ==="
echo "<system-reminder> blocks in user msgs: $(jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" | grep -c '<system-reminder>')"
echo
echo "=== command-name tags ==="
jq -r '.messages[] | select(.role=="user") | .content' "$LATEST" | grep -oE "<command-name>[^<]+</command-name>" | sort -u
echo
echo "=== system_prompt slots present ==="
jq -r '.system_prompt' "$LATEST" | grep -oE "<(identity|policy|guidelines name=\"[^\"]+\"|environment|agents)>" | sort -u
echo
echo "=== identity slot first line ==="
jq -r '.system_prompt' "$LATEST" | head -1
EOF
chmod +x /tmp/gen-test/check.sh

# Usage:
/tmp/gen-test/check.sh
```
