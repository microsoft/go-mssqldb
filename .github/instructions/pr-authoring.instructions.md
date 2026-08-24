# PR Authoring Instructions

These instructions apply when an agent is **authoring or modifying code** in
`microsoft/go-mssqldb` — typically after taking over a Copilot-generated PR in a local
session. They supplement `.github/copilot-instructions.md`, which remains the source of
truth for build, test, and Go-version details.

## Scope discipline

- Change only what the linked issue requires. Do not opportunistically refactor, rename,
  or reformat unrelated code — it inflates the diff and buries the real change from
  reviewers.
- If you find a genuine bug adjacent to your change, fix it only when it is caused by, or
  tightly coupled to, the change you are making. Otherwise file an issue.
- One logical change per PR. If the work splits cleanly into independent pieces, say so
  rather than bundling.

## Tests are mandatory

Every behavioral change ships with a test that fails before the fix and passes after.

- Prefer **unit tests that need no SQL Server** — they run in ~1s and gate every PR
  reliably. Reach for an integration test only when the behavior genuinely requires a
  live server (protocol negotiation, bulk copy, auth).
- Integration tests must skip gracefully when no connection string is configured, matching
  the existing pattern in the repo.
- For a bug fix, the test must encode the *specific* failing input from the issue, not a
  generic happy path.
- Never weaken, skip, or delete an existing test to make CI pass. If an existing test now
  fails legitimately, that is a behavior change and it must be called out explicitly in
  the PR description.

## Coverage

- **Patch (diff) coverage must be at least 90%.** Project coverage must stay at or above
  80%. Both are enforced by Codecov and will block the PR.
- Verify locally before pushing:

  ```bash
  go test -coverprofile=coverage.out ./...
  go tool cover -func=coverage.out | tail -1
  ```

- Uncovered error paths are the usual cause of a coverage miss. Either test them or
  justify the gap in the PR description.

## Validation before marking ready for review

Run all of these and confirm they pass:

```bash
go fmt ./...
go build ./...
go vet ./...
go test ./msdsn ./internal/... ./integratedauth ./azuread   # fast unit tests
go test ./...                                                # full suite, needs SQL Server
```

The full suite takes 15+ minutes against SQL Server. Do not cancel it. If no local SQL
Server is available, say so in the PR description so a human knows integration coverage
was left to CI.

## Protocol and driver conventions

- Driver name is `sqlserver`. Parameters use `@name` or `@p1, @p2`.
- `LastInsertId()` is unsupported — use an `OUTPUT` clause or `SCOPE_IDENTITY()`.
- Never trust a length or offset read from the wire without bounds-checking it against the
  actual buffer. This is the single most common source of security bugs in this codebase.
- Do not change exported API signatures without flagging it as a breaking change.
- Windows-only code paths (named pipes, shared memory) are validated by AppVeyor, not the
  Linux CI matrix. Changes there need extra care because Linux CI will pass regardless.

## Security

- Never log, wrap, or embed credentials, tokens, or connection strings in error messages.
- Never commit secrets, even in tests or examples. Use environment variables.
- Do not default `TrustServerCertificate` to true or weaken TLS defaults anywhere outside
  a clearly documented CI-only workaround.

## Commits and PR description

Use Conventional Commits (`feat:`, `fix:`, `docs:`, `chore:`, `ci:`, `test:`, `refactor:`,
`perf:`, `feat!:`) — Release Please depends on this for versioning. See
`.github/copilot-instructions.md` for the full table.

The PR description must state:

1. What the issue was, and the root cause — not just what changed
2. How the fix works
3. Which tests were added, and what they would have caught
4. Any validation that was skipped and why (e.g. no local SQL Server)
5. Whether the change is Windows-affecting

## Definition of done

- [ ] Tests added that fail without the fix
- [ ] Patch coverage ≥ 90%
- [ ] `go build`, `go vet`, `go fmt` clean
- [ ] Full test suite green, or the gap explicitly declared
- [ ] Conventional commit title
- [ ] PR description covers root cause, fix, and tests
- [ ] Diff contains no unrelated changes
