---
name: code-review
description: Review a pull request, diff, or set of proposed changes in microsoft/go-mssqldb — from a PR link, a PR number, or local staged/unstaged changes. Use whenever the user asks to review a PR, asks for feedback on a diff, or asks whether changes are ready to merge. Covers correctness, tests and coverage, API/breaking changes, resource handling, and go-mssqldb repo conventions. For wire-protocol or buffer changes also use protocol-review; for auth, TLS, or credential handling also use security-review.
---

# Pull Request Review

You are reviewing proposed changes. Review only what the diff changes plus directly
affected code — do not critique pre-existing code outside the PR's scope.

Your review may be published alongside GitHub Copilot's built-in review. Repeating what
it already said costs the author a round trip and adds nothing. Read the existing
feedback first.

## Process

1. Read the PR title and description to understand intent. Flag a description that is
   missing, or that does not match the diff.
2. Read the full diff against the base branch before commenting.

   ```bash
   gh pr view <number> --json title,body,author,state,baseRefName,files,additions,deletions
   gh pr diff <number>
   ```

3. Read **all existing feedback** before forming findings. Three separate sources, all
   of which must be checked:

   ```bash
   gh api repos/microsoft/go-mssqldb/pulls/<n>/comments   # inline review comments
   gh api repos/microsoft/go-mssqldb/pulls/<n>/reviews    # review summary bodies
   gh api repos/microsoft/go-mssqldb/issues/<n>/comments  # top-level comments, incl. Codecov
   ```

   Copilot's review summary and bot analysis usually live in the latter two, not inline.

4. Check CI: `gh pr checks <n>`. If checks are failing, say which one and what it
   reports, and stop. A line-by-line review of a PR whose tests do not compile wastes
   everyone's time.

5. **Verify claims against the actual code — do not assume.** Read the full function,
   the type declarations, and the call sites, not just the changed lines. Most false
   positives in this repository come from reading a hunk in isolation.

## The three gates

Every candidate finding must pass all three before you report it. This is the main
defence against plausible-but-wrong comments.

**Reachability** — can you name a specific input and call path that triggers this? A
`uint16` cannot be negative. A parameter every caller allocates cannot be nil inside the
callee. If the type system or the call sites already exclude the condition, drop it.

**Novelty** — is this already covered by a Copilot comment, an existing thread, or a CI
failure message? If yes, drop it.

**Consequence** — what concretely breaks: panic, wrong data returned to the caller,
credential exposure, hung connection? "Cleaner", "safer", and "more idiomatic" are not
consequences. Drop it.

Three checklist items are exempt from the consequence gate, because their harm is to the
review and release process rather than to running code: a **weakened, skipped, or deleted
test**; a **PR description that does not match the diff**; and **unrelated changes** that
inflate the diff. Report these when you find them. Everything else must clear all three
gates.

## What to check

Evaluate each area. Skip areas that don't apply rather than padding the review.

- **Correctness**: logic bugs, off-by-one, boundary and empty cases, error handling,
  incorrect assumptions, ignored returned errors on paths that matter.
- **Tests**: new or changed behavior has a test that would fail without the change.
  Tests assert real behavior, not tautologies. For a bug fix, the test encodes the
  specific failing input from the issue rather than a generic happy path. Flag an
  existing test that was weakened, skipped, or deleted to make CI pass — that is never
  an acceptable fix.
- **Coverage**: read the Codecov comment on the PR and look at *which* lines are
  uncovered, not the percentage. Uncovered error paths in protocol or connection code
  are worth raising; a missed line in a trivial getter is not. Advise on specific gaps;
  do not issue a numeric verdict — CI already reports the number authoritatively.
- **Resource handling**: connections, `*sql.Rows`, and file handles closed on every
  error path, not just the happy path.
- **Concurrency**: data races on connection state, deadlock in the buffer or writer
  path, goroutine leaks.
- **Data conversion**: precision loss in `decimal`, `money`, `datetime2`; timezone and
  midnight boundary handling; nullable type round-trips.
- **API & breaking changes**: exported signatures, `database/sql` driver semantics,
  connection string parameter behavior. Flag a breaking change and whether it is
  documented as one.
- **Repo conventions**: matches existing patterns in the codebase; respects
  `.github/copilot-instructions.md`.

## go-mssqldb specifics

- **Driver conventions**: driver name is `sqlserver`; parameters are `@name` or
  `@p1, @p2`. `LastInsertId()` is unsupported — an `OUTPUT` clause or `SCOPE_IDENTITY()`
  is required. Using it is a real finding.
- **Commit format**: Conventional Commits, because Release Please depends on it for
  versioning. See `.github/copilot-instructions.md` for the type table.
- **Windows-only paths**: named pipes and shared memory are validated by AppVeyor, not
  the Linux CI matrix. Changes there deserve closer reading precisely because Linux CI
  will pass regardless.
- **Benchmarks**: `pr-validation.yml` detects regressions via `benchstat` at >15%,
  p<0.01. Do not speculate about performance without benchmark evidence.
- **Scope**: unrelated refactoring, renaming, or reformatting inflates the diff and
  buries the real change. Flag it.
- **No AI slop**: comments that restate what the code does, filler phrases, redundant
  validation, duplicated logic.

## What never to report

These are handled by tooling or by other people, and reporting them is pure noise:

- Style, naming, formatting, comment wording, import ordering — `gofmt` and
  `golangci-lint` own these and run on every PR.
- Anything `go vet`, `revive`, or `reviewdog` already reports.
- Test structure or table-test formatting preferences.
- "Consider adding a nil check", "consider validating this input", "consider handling
  this error" — unless you can name a reachable path where it matters.
- Defensive checks for conditions the type system already excludes.
- Refactoring suggestions, extracting helpers, reducing nesting.
- Anything already stated in a Copilot review comment or a CI failure message.
- Praise, summaries, or restatements of what the PR does.

This list outranks the severity tiers. A finding that lands here is dropped even if you
believe it is correct.

## Output format

1. **Summary** — one to three sentences: what the PR does and your overall assessment.
2. **Findings grouped by severity:**
   - **Blocking** — must fix before merge: bugs, security issues, breaking changes
     without handling, a weakened test.
   - **Suggestion** — should consider; improves quality but does not block merge.
   - **Nit** — a correctness or clarity issue too small to block. Never style, naming,
     formatting, comment wording, or import ordering.
3. Each finding gives the exact `file:line`, the concrete failure path, the consequence,
   and a suggested fix — or an explicit "I am not certain of the right fix here".

Cap findings at **five**. More than that and you are almost certainly over-reporting;
re-apply the gates and keep the real ones.

## Principles

- If a change is correct, do not invent problems. An empty severity group means "none
  found" — say so briefly and stop.
- Distinguish facts you verified in the code from concerns worth checking. Never state
  a guess as a defect. Prefix anything you would not defend without more context with
  `Possible:`.
- Prefer the smallest correct fix over a large refactor.
- No preamble, no praise, no restatement of what the PR does beyond the summary line.
- Reviewing is not merging. The author owns the merge — never merge someone else's PR.

## Unattended runs

When invoked by the scheduled review sweep rather than interactively, publish one review
per run and end the body with an idempotency marker so later sweeps skip this commit:

```
<!-- agent-review: <full head SHA> -->
```

Before reviewing, check the PR's comments for a marker matching the current head SHA and
skip the PR entirely if one exists. With no findings, post only a brief "No findings"
line plus the marker — that is a normal, successful outcome, not a failure.

Never raise a finding on a line that was modified in direct response to an earlier review
comment — whether yours, GitHub Copilot's, or a human's — unless the change introduced a
new defect you can demonstrate. Second-guessing a fix someone already asked for is a
review loop and it does not converge.
