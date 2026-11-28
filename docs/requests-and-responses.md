# Requests and responses

This page holds the reasoning behind the request helpers in [`request.go`](../request.go) and
[`query.go`](../query.go), the object response in [`response.go`](../response.go), the
deadlines in [`transfer.go`](../transfer.go), and the event stream in
[`eventstream.go`](../eventstream.go).

## Requests

- `ParseQuery` bounds the page so that the data layer's offset, (page-1) times the size, fits an
  `int` at the largest size a request may ask for.
- `IfMatch` takes base-10 digits with an optional leading `-`: `strconv.ParseInt`'s syntax
  without the `+` it also accepts.
- After `DecodeJSON` reads its value, a further token, or bytes that are not one, is trailing
  data; any other error is the read failing, an overflow included, and is reported as itself.
  An overflow names the limit the read hit, the tighter of `DecodeJSON`'s and an outer
  `middleware.BodyLimit`'s.

## The object response

- When the open fails, `WriteObject` answers a problem that must not be stored in the object's
  place: a 404 is heuristically cacheable. The headers that describe the object's bytes or how
  long to keep them, set by `WriteObject` or by its caller for the object, are removed first,
  since they describe bytes the problem is not: a browser would otherwise save a JSON 404 under
  the object's name and cache it for the object's lifetime.
- Cache preconditions are evaluated in RFC 9110 §13.2.2's order: `If-None-Match` when present,
  and `If-Modified-Since` only in its absence. A date that does not parse is ignored, as the RFC
  requires, and so is either header on a method other than GET or HEAD.
- `If-None-Match` matches by weak comparison: `*` matches any current object, and otherwise one
  listed tag must equal the ETag once a `W/` prefix is set aside on either side. The list is
  scanned tag by tag, since a quoted tag may itself contain a comma; a malformed remainder ends
  the scan.
- A `Transfer` deadline too long for a `time.Duration` saturates, and a saturated duration sets
  no deadline at all.

## The event stream

- A stream's context ends when the request's context or the run context `Server.Start` carries
  into the request ends, whichever is first. The request's context alone would hold the
  connection open through a drain, since `http.Server.Shutdown` waits for every active
  connection. The run context alone would miss a client that went away. The stream's context is
  a child of the request's, and `context.AfterFunc` cancels it, with the run context's cause, when
  the run context ends. A request with no carried run context, as under a plain `httptest`
  server, ends with the request alone. [The server page](server.md) says why the run context is a
  value rather than the request context's parent.
- Before each write, the headers included, the writer sets the write deadline to the Config's
  `WriteTimeout` from now, rather than once for the whole stream. A stream has no size to derive
  a transfer deadline from, and a deadline on each write keeps `WriteTimeout` meaning what it
  means for any response: a stalled client fails within one `WriteTimeout`, while a stream that
  keeps writing lasts as long as it needs.
- The sender keeps one `sse.Encoder` behind a mutex, since an Encoder isn't safe for concurrent
  use. The Encoder writes through a wrapper that records any write error, so the sender can tell
  two failures apart. When the Encoder rejects an event, it has written nothing, and the stream
  stays usable. When a write fails, part of an event may have reached the client, so the sender
  closes.
- The writer owns keepalives rather than leaving them to the handler, for two reasons. A
  keepalive writes to the same `sse.Encoder` as the handler's events, and only the sender,
  holding its mutex, can write one without interleaving it with an event. A keepalive is also how
  a stream notices a client that has gone away: a stream with nothing to send writes nothing, so
  without keepalives a dead connection would stay open until the stream's context ended.
- One goroutine per stream runs a timer for the `KeepAlive` interval. When the timer fires, the
  goroutine takes the mutex and writes an empty comment if nothing has been written for a whole
  interval; otherwise it resets the timer for the rest of the interval. Each successful write
  records its time, so an event or a retry restarts the interval and a busy stream carries no
  keepalives. A keepalive goes through the same deadline and flush as an event, and a failed one
  closes the sender like any failed write. The goroutine stops when the sender closes or the
  stream's context ends, and `Serve` waits for it before returning.
- The sender closes when the stream's context ends, when a deadline, write, or flush fails, or
  when `Serve` returns, so a producer still holding it can't write to a finished response.
- Nothing is written at the end of a stream: no closing event, comment, or retry. The client sees
  the response end and reconnects, or not, by its own policy.
