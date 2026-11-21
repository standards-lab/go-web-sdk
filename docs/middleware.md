# Middleware

Reasoning behind the middleware in [`middleware/`](../middleware) and
[`middleware/rate-limit`](../middleware/rate-limit).

## RequestLogger

The logged status is the one the client got:

- A committed status stands, whether the handler returned or panicked afterwards.
- A handler that returns without writing gets net/http's implicit 200.
- Otherwise a panic is unwinding with nothing on the wire. A `Recoverer` outside the logger
  writes a 500 problem next; with none wired, net/http drops the connection, and 500 is the
  honest label for what the client saw.

The logger tells a panic from a normal return by a flag set after the handler returns, not by
calling `recover`, so it never becomes a second recovery point.

`http.route` is the ServeMux pattern with its method token removed. ServeMux's pattern grammar
is `[METHOD ][HOST]/[PATH]`, the method ending at the first space or tab with any further blanks
skipped, and the logger mirrors that split. `request_id` is logged only when the request's
context carries a non-empty id, so a chain with no `RequestID` logs no spurious empty id.

## RequestID

An inbound id is accepted only as 1 to 128 visible ASCII characters, a shape safe to echo into a
log record and a response header. A generated id is 32 lowercase hex characters from 16 random
bytes; `crypto/rand.Read` never returns an error, since it crashes the program if the operating
system's source fails.

## Headers

Each header name is canonicalized once, at construction, as `http.Header.Set` canonicalizes it.

## Rate limit

- A window under one second is rejected: httprate reports the window to the client in whole
  seconds (`Retry-After`, `X-RateLimit-Reset`), so a shorter one cannot be expressed there.
- A request is keyed by the host part of its remote address, canonicalized through
  `httprate.CanonicalizeIP`. A `RemoteAddr` with no port, as some test setups leave it, is used
  as it is. The key function's error return is httprate's signature; it never fails.
- httprate's error path, which neither the key function nor the in-memory counter reaches,
  answers a fixed 500 problem.
