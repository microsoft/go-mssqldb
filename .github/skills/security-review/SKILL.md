---
name: security-review
description: Security review for microsoft/go-mssqldb changes touching authentication, TLS, credentials, connection strings, or data read from the network. Use when reviewing changes under azuread/, integratedauth/, aecmk/, msdsn/, or any diff involving passwords, tokens, certificates, encryption, or parsing untrusted wire data. Also use when the user asks for a security review or asks whether a change is safe.
---

# Security Review

This is a database driver. It handles credentials on every connection and parses
attacker-influenced bytes off the wire on every query. Those are the two areas where a
defect is a vulnerability rather than a bug.

Report only findings you can demonstrate. A speculative "consider validating this" with
no reachable path is noise, and it trains the author to ignore you.

## Credential and secret handling

- Passwords, access tokens, and connection strings must never reach logs, error
  messages, or wrapped errors. Check every `fmt.Errorf`, `log.*`, and `%v`/`%+v` on a
  struct that might carry credentials — a struct-level format verb is the usual way a
  secret leaks.
- No secrets committed in code, tests, examples, or fixtures. Tests take credentials
  from environment variables.
- Credentials should not be retained in memory longer than needed, and should not be
  copied into structs that outlive the connection attempt.

## TLS and certificate validation

- `TrustServerCertificate` must never default to true. A change that flips it, or that
  makes it easier to reach a trusting state by accident, is **Blocking**.
- Certificate validation must not be skipped or downgraded. `InsecureSkipVerify` is
  Blocking outside a clearly documented, CI-only workaround.
- Minimum TLS version must not be lowered. Cipher suite lists must not gain weak
  entries.
- Encryption negotiation must not silently fall back to plaintext when encryption was
  requested. Check that a failed negotiation is an error, not a downgrade.
- The repo has one legitimate CI-only exception — `GODEBUG=x509negativeserial=1` for the
  SQL Server 2017 Docker image in `pr-validation.yml`. Anything resembling this outside
  CI is a finding.

## Authentication

- Auth failures must fail closed. Check that an error path cannot fall through to a
  connected-and-trusted state.
- Token acquisition, refresh, and caching in `azuread/` must not widen the audience,
  scope, or lifetime of a token beyond what the connection needs.
- Kerberos and integrated auth changes in `integratedauth/` are Windows-affecting and are
  not covered by the Linux CI matrix — read them more carefully for that reason.
- Always Encrypted key handling in `aecmk/`: column encryption keys and master key
  material must not be logged, cached beyond their intended scope, or written to disk.

## Untrusted input from the wire

Everything the server sends is untrusted input. The most common vulnerability class in
this codebase is a length or offset read from the wire and then used without checking it
against the actual buffer.

- Every length prefix, offset, and count read from a TDS token must be bounds-checked
  against the remaining buffer before it is used to slice, index, or allocate.
- An attacker-controlled length must not drive an unbounded allocation.
- Integer conversions on wire values must not overflow or truncate into a smaller type
  before a bounds check.
- Loops driven by a wire-supplied count must terminate on malformed input.

For depth on the TDS layer specifically, use the `protocol-review` skill alongside this
one.

## Connection string parsing

- Parsing changes in `msdsn/` must not let a crafted connection string alter security
  posture unexpectedly — silently disabling encryption, changing the target host, or
  injecting parameters through unescaped separators.
- Errors from parsing must not echo the full connection string back, since it may
  contain a password.

## Applying the gates

The three gates from `code-review` apply here too, with one adjustment: for a genuine
security finding, weight **reachability** hardest. A theoretical issue with no path from
untrusted input is not a vulnerability, and reporting it as one devalues the findings
that are real.

Conversely, when input genuinely reaches the code path, the bar for consequence is lower
than elsewhere — a panic/DoS (for example an out-of-bounds slice) or credential exposure
is severe even when the triggering conditions are narrow.

## Output

Follow the `code-review` output format, with severity meaning:

- **Blocking** — exploitable, or weakens an existing security guarantee. Credential
  exposure, TLS downgrade, missing bounds check on wire data, auth fail-open.
- **Suggestion** — defense in depth; not currently reachable but narrows future risk.
- **Nit** — reserve for genuinely minor items; most security findings are not nits.

State the attack path explicitly: who controls the input, how it reaches the code, and
what they gain. If you cannot write that sentence, the finding is not ready to post.

## Escalation

Never propose a change to credentials handling, TLS defaults, or `TrustServerCertificate`
as an automated fix. Flag it and hand it to a human — these need a deliberate decision,
not an agent's judgment call.
