
---

## Part 6 — Phase 9 (CI/CD): what broke, why, and what it taught

Getting CI green took three separate, genuinely different fixes — worth remembering as a case study in reading errors precisely rather than guessing.

### Failure 1: linter binary older than the language version it's analyzing
**What happened:** `golangci-lint`'s prebuilt binary (built with Go 1.24) couldn't parse code/dependencies requiring Go 1.25/1.26 features — not a config problem, a fundamental toolchain mismatch. The first fix attempt (lowering `go.mod`'s version, then overriding the linter's target version in config) both failed because neither addressed the real cause: the *binary itself* was too old.
**The actual fix:** `install-mode: goinstall` in `golangci-lint-action` — this builds the linter from source using the CI runner's own (current) Go toolchain, instead of downloading a binary someone else built against a fixed, potentially-stale Go version.
**The rule going forward:** "package requires newer Go version" errors mean the *tool*, not your code, needs upgrading. Don't downgrade your own code/config to match an outdated tool if a way to upgrade the tool exists.

### Failure 2: a genuinely real lint finding (not a false positive)
**What happened:** `errcheck` correctly flagged `defer tx.Rollback(ctx)` for silently discarding an error return.
**The fix:** `defer func() { _ = tx.Rollback(ctx) }()` — explicitly acknowledging the error is intentionally ignored, with a comment explaining why (it's a no-op safety net after a successful commit).
**The rule going forward:** not every lint failure is a tooling problem — sometimes the linter is just right. The tell: it points at one specific line in your own code with a clear, sensible reason, rather than a wall of errors across unrelated dependencies (which is what Failure 1 looked like).

### Failure 3: registry naming rule (GHCR requires lowercase)
**What happened:** `${{ github.repository }}` expands to `Atul-Koundal/protoverse` — capital letters included — but Docker registry image names must be all lowercase. `docker buildx build` rejected the tag outright with a clear error.
**The fix:** compute a lowercased copy of the repo path once (`tr '[:upper:]' '[:lower:]'`), store it in `$GITHUB_ENV`, and reference `${{ env.IMAGE_REPO }}` in the tags instead of `${{ github.repository }}` directly.
**The rule going forward:** GitHub's built-in context variables (`github.repository`, `github.actor`, etc.) mirror real-world values like your username verbatim — they don't automatically conform to every downstream tool's constraints (registry naming, DNS-safe strings, etc.). When a workflow fails on a value that "should just work," check whether the raw context value violates some external system's format rules.

### General lesson from all three: read the *first* error precisely before reacting
Each of these three failures had a distinct, specific root cause visible directly in the error message (a Go version number, a specific file:line with a clear rule name, an explicit "must be lowercase" message). None of them needed guessing — the fix in each case was implied by reading the actual error text carefully, not by trying something plausible-sounding and seeing if it helped. Worth carrying forward as a general debugging habit beyond just this project.
