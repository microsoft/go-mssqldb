---
description: "Use when authoring, updating, or preparing a pull request in microsoft/go-mssqldb. Covers scope discipline, test requirements, coverage expectations, local validation, commit format, and PR description content."
name: "go-mssqldb PR Workflow"
applyTo: "**"
---

# go-mssqldb PR Workflow

Follow this workflow for pull requests in `microsoft/go-mssqldb`. This applies whenever
code is authored here, including when an agent takes over a Copilot-generated PR in a
local session.

For build, test, and Go-version details, `.github/copilot-instructions.md` remains the
source of truth.

## Scope discipline

- Change only what the linked issue requires. Do not opportunistically refactor, rename,
  or reformat unrelated code — it inflates the diff and buries the real change from
  reviewers.
- Fix an adjacent bug only when it is caused by, or tightly coupled to, the change you
  are making. Otherwise file an issue.
- One logical change per PR. If the work splits cleanly into independent pieces, say so
  rather than bundling.

## Tests

Every behavioral change ships with a test that fails before the fix and passes after.

- Prefer **unit tests that need no SQL Server** — they run in about a second and gate
  every PR reliably. Reach for an integration test only when the behavior genuinely
  requires a live server: protocol negotiation, bulk copy, auth.
- Integration tests must skip gracefully when no connection string is configured,
  matching the existing pattern in the repo.
- For a bug fix, the test must encode the *specific* failing input from the issue, not a
  generic happy path. For a malformed-input or buffer fix, feed the crafted bytes
  directly to the parser — that test needs no server and is the one that would have
  caught the original bug.
- Never weaken, skip, or delete an existing test to make CI pass. If an existing test
  now fails legitimately, that is a behavior change and must be called out explicitly in
  the PR description.

## Coverage

Aim to cover all new code. Every branch you add, including the error paths, should have
a test that exercises it — untested error paths are where this driver's bugs live, and
they are the usual reason a change misses coverage.

Codecov reports the numbers on the PR and enforces the project floor; treat its comment
as feedback on *which* lines are untested rather than a percentage to satisfy. Writing
tests that chase a threshold rather than assert real behavior is worse than a visible
gap. If a gap is deliberate, explain it in the PR description.

Check locally before pushing:

```bash
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out | tail -1
```

## Validation before marking ready for review

```bash
go fmt ./...
go build ./...
go vet ./...
go test ./msdsn ./internal/... ./integratedauth ./azuread   # fast unit tests
go test ./...                                                # full suite, needs SQL Server
```

Note `go build ./...` rather than bare `go build` — the latter compiles only the root
package and its dependencies, so it never builds `azuread`, the `aecmk` providers, or
`examples/`.

The full suite takes 15+ minutes against SQL Server. Do not cancel it. If no local SQL
Server is available, say so in the PR description so a human knows integration coverage
was left to CI.

## Driver conventions

- Driver name is `sqlserver`. Parameters use `@name` or `@p1, @p2`.
- `LastInsertId()` is unsupported — use an `OUTPUT` clause or `SCOPE_IDENTITY()`.
- Never trust a length or offset read from the wire without bounds-checking it against
  the actual buffer. This is the most common source of security bugs in this codebase.
- Do not change exported API signatures without flagging it as a breaking change.
- Windows-only code paths (named pipes, shared memory) are validated by AppVeyor, not
  the Linux CI matrix. Changes there need extra care because Linux CI will pass
  regardless.

## Security

- Never log, wrap, or embed credentials, tokens, or connection strings in error
  messages. Watch struct-level format verbs — `%v` on a struct carrying a password is
  the usual way a secret leaks.
- Never commit secrets, even in tests or examples. Use environment variables.
- Do not default `TrustServerCertificate` to true or weaken TLS defaults anywhere
  outside a clearly documented CI-only workaround.

## Commits and PR description

Use Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`,
`refactor:`, `perf:`, `feat!:`) — Release Please depends on this for versioning. See
`.github/copilot-instructions.md` for the full table.

The PR description must state:

1. What the issue was, and the root cause — not just what changed
2. How the fix works
3. Which tests were added, and what they would have caught
4. Any validation that was skipped and why
5. Whether the change is Windows-affecting

## Review and merge

- Address Copilot review feedback before requesting human review. Verify each comment
  against the actual code first — automated reviewers are sometimes wrong, and the
  `agent-merge` skill covers how to refute one properly.
- Watch PR validation and fix failures on the latest commit.
- The PR author owns the merge. Do not merge someone else's PR.

## Definition of done

- [ ] Tests added that fail without the fix
- [ ] New code covered, including error paths; deliberate gaps explained
- [ ] `go build ./...`, `go vet ./...`, `go fmt ./...` clean
- [ ] Full test suite green, or the gap explicitly declared
- [ ] Conventional commit title
- [ ] PR description covers root cause, fix, and tests
- [ ] Diff contains no unrelated changes
