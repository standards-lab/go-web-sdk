# Requests and responses

Reasoning behind the request helpers in [`request.go`](../request.go) and
[`query.go`](../query.go), the object response in [`response.go`](../response.go), and the deadlines in
[`transfer.go`](../transfer.go).

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
