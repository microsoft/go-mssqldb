---
name: agent-merge
description: Respond to pull request review feedback on microsoft/go-mssqldb — addressing review comments, fixing CI failures, and resolving merge conflicts, including unattended "agent merge" runs. Use when working through review comments on an open PR, when the user asks to address or resolve feedback, or when deciding whether a review comment is actually correct.
---

# Responding to Review Feedback

Review comments in this repository come from two automated reviewers (GitHub Copilot and
a scheduled review agent) as well as humans. **Automated reviewers are sometimes wrong.**

The author will revisit the resolved threads afterwards to confirm the fixes are correct.
Your job is to make that review fast and trustworthy.

## Prime directive: verify before you comply

Your default must not be compliance. For every comment:

1. Read the code the comment refers to — the full function, the type declarations, and
   the call sites, not just the quoted line.
2. Decide whether the claim is actually true for this code.
3. Only then act.

An agent that always complies, paired with a reviewer that occasionally hallucinates,
silently lands plausible-but-wrong changes. That failure mode is worse than an
unaddressed comment, because nobody sees it.

Also consider whether the comment identifies a real problem but proposes the wrong
remedy. Fixing the underlying issue is better than applying a suggested fix you know to
be wrong — say so in the reply when you do this.

## Replying

Every reply begins with one of two prefixes. These are parsed for weekly metrics, so the
format matters.

### `Fixed:` — the comment was valid

```
Fixed: <what changed and why it addresses the concern>
```

Then: make the change, add or update a test covering it, run the targeted tests, and
resolve the thread.

### `Refuted:` — the comment was wrong

```
Refuted: <concrete reason, citing the type, call site, or invariant that makes the
concern impossible or already handled>
```

Then: resolve the thread **without changing any code**.

Refuting is a first-class outcome, not a failure. Common valid refutations here:

- The suggested bounds check is impossible — the value is a `uint16` and cannot be
  negative
- The nil check is unreachable — every call site allocates the argument
- The condition is already handled earlier in the function (cite the line)
- The suggestion would break `database/sql` driver semantics
- The behavior is intentional and covered by an existing test (cite it)

If you are genuinely unsure whether a comment is valid, do not guess in either
direction. Reply asking for clarification and leave the thread open for the author.

## Hard rules

- **Never weaken, skip, or delete a test** to make a comment or a failing check go away.
- **Never reduce coverage.** If a fix adds a branch, it adds a test.
- **Never force-push** over the author's commits. Add new commits.
- **Never expand scope.** Fix what the comment identifies, nothing more. A review
  comment is not an invitation to refactor.
- **Never resolve a thread you did not actually address.** Resolving without a `Fixed:`
  or `Refuted:` reply hides the comment from the author.
- **Never change exported API** in response to a comment without flagging it in the PR
  description as a breaking change.
- **Never touch credentials, TLS defaults, or `TrustServerCertificate`** in response to
  a comment. Escalate to the author instead.

## After each fix

Run the narrowest test command covering the change, plus the fast unit suite:

```bash
go build ./... && go vet ./...
go test ./msdsn ./internal/... ./integratedauth ./azuread
```

Run the full suite when the change touches protocol, buffer, or connection code.
Confirm CI is green before considering the PR ready again.

## CI failures

- Read the actual failure output before changing anything. Do not guess from the check
  name.
- A flaky integration test is not a reason to change production code. If a failure does
  not reproduce and is unrelated to the diff, say so and re-run the check.
- A benchmark regression flagged by `benchstat` (>15%, p<0.01) is real. Investigate it;
  do not suppress it.
- A coverage failure means tests are missing, not that the threshold is wrong. Look at
  which lines are uncovered and whether they are paths that matter.

## Merge conflicts

- Rebase or merge `main` and resolve conflicts preserving **both** intents — the PR's
  change and the incoming change from `main`.
- If a conflict is not mechanically resolvable, or resolving it requires a judgment call
  about intended behavior, stop and hand it to the author. Do not guess.
- Re-run the full validation after any conflict resolution.

## What stays human

- **Merging.** Always. Every PR requires human approval regardless of how clean the
  automated loop was.
- Any change to security posture, TLS handling, or authentication behavior.
- Any breaking change to exported API.
- Any decision to close a PR or abandon an approach.
