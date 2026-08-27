---
name: protocol-review
description: Deep review of TDS wire-protocol and buffer handling changes in microsoft/go-mssqldb — tds.go, buf.go, token stream readers and writers, packet framing, PLP chunks, collation, UCS-2 conversion, and bulk copy encoding. Use when a diff touches wire encoding or decoding, buffer indexing, packet lengths, or type serialization, and when reviewing buffer-overflow or malformed-response fixes.
---

# TDS Protocol Review

This is the layer where a bug is a denial-of-service or integrity issue rather than a wrong
result. The server's bytes are untrusted input; the driver must survive a malformed or
hostile response without panicking, over-reading, or over-allocating.

Read the surrounding code before judging anything here. Protocol code is dense with
invariants established several functions away from the line in the diff.

## Bounds checking — the primary concern

The single most common security bug in this codebase is a length or offset taken from
the wire and used without validating it against the actual buffer.

For every value read from the wire and then used to slice, index, allocate, or loop:

- Is it checked against the **remaining** buffer length, not the total?
- Is the check done **before** the slice expression, not after?
- Does the check account for the size of the element, not just the count? A count of
  `n` items of 4 bytes each needs `4*n` bytes available, and that multiplication can
  overflow.
- Is the comparison done in a type wide enough to hold the product?

Verify the actual types before flagging a missing check. A `uint16` cannot be negative,
so a `>= 0` check on one is dead code, and suggesting it is a false positive. Read the
declaration.

## Packet framing

- Packet length fields must match the bytes actually written. An off-by-one in a header
  length desynchronizes the stream and produces failures far from the cause.
- Status flags — EOM in particular — must be set correctly on the final packet of a
  message.
- A message spanning multiple packets must handle a token split across a packet
  boundary. This is a classic source of bugs that only appear under large payloads.
- Reads must handle a short read: fewer bytes arriving than requested is normal on a
  socket, not an error.

## Token stream

- Token type bytes must be validated before dispatch; an unknown token must produce an
  error rather than a mis-parse.
- Token lengths must be consumed exactly. Leaving bytes unread, or reading past the
  end, corrupts every subsequent token.
- Error and info tokens carry server-controlled strings — check they are length-limited
  and not used in a format string.
- `DONE` token handling must correctly propagate row counts and error state.

## PLP and variable-length data

- PLP chunk lengths are wire-controlled. Each chunk length needs its own bounds check;
  checking only the total is insufficient.
- The unknown-length sentinel must be handled distinctly from a concrete length.
- A terminator must be required — a stream that ends without one is malformed input, not
  end of data.
- Accumulating chunks must not grow unbounded from a hostile server.

## Character and type encoding

- UCS-2 conversion: an odd byte count is malformed input. Check that `str2ucs2` and
  `ucs22str` paths handle it rather than slicing past the end. Surrogate pairs must not
  be split across a chunk boundary.
- Collation bytes must be validated before use as an index into a codepage table.
- `decimal`, `money`, `datetime2`, `datetimeoffset`: check scale and precision handling
  for truncation and sign errors. Midnight and timezone boundaries are a recurring
  source of off-by-one bugs here.
- Nullable types: the null indicator must be checked before the value is read.

## Bulk copy

- Column metadata must match the data actually sent; a mismatch produces a server-side
  error that is hard to trace back.
- Row batching and the final flush must not drop buffered rows.
- Type conversion in the bulk path must apply the same precision rules as the normal
  parameter path.

## Testing expectations

- Malformed-input handling should be covered by a unit test that feeds crafted bytes
  directly to the parser — no SQL Server required. For a buffer-overflow fix, the test
  must reproduce the specific malformed input from the issue and fail without the fix.
- Round-trip tests for encoding changes: encode, decode, compare.
- Flag a protocol fix that ships without a malformed-input test. That is the test that
  would have caught the original bug.

## Output

Follow the `code-review` output format. Severity here:

- **Blocking** — missing bounds check on wire data, a panic reachable from a malformed
  response, stream desynchronization, silent data corruption.
- **Suggestion** — correct but fragile; an invariant that holds today only by accident
  of the calling code.
- **Nit** — genuinely minor.

For every finding, state the malformed input that triggers it. If you cannot describe
the bytes that cause the failure, you have not verified the finding — do not post it.
