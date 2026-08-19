# The server

The design of the `Server` bootstrap. The code and `doc.go` are authoritative for the API; this
note records the reasoning.

## The bootstrap belongs in the SDK

A web service that receives only health handlers hand-writes the same wrapper around `http.Server`
— the same fifty lines in every service, with the same defect: `ListenAndServe` binds inside the
goroutine it serves from, so a taken port or a bad address is logged by a goroutine nobody watches
while startup continues and readiness reports healthy with nothing listening. The SDK ships the
wrapper once, with the defect designed out.

## Bind on the calling goroutine, serve in the background

`Server.Start` splits the two halves. The bind runs on the calling goroutine, bounded by the
context `Start` receives, and a bind failure is a returned error; serving begins in the background
only after the listener exists. Because a composition root registers `Start` as a startup hook and
a startup hook's error fails the coordinator's startup, a bind failure stops startup before
readiness ever flips. Binding first also makes the bound address knowable: a configured port 0
binds an ephemeral port, and `Server.Addr` reads back the assignment — which is what lets tests
bind without racing a fixed port.

## A serve failure is a value, not a log line

A failure after startup arrives on the buffered channel `Server.Err` returns. The composition root
registers the channel as a monitored source, so the first failure ends the coordinator's run and
surfaces in `Run`'s returned error. A server that logged the failure instead would be choosing a
policy the composition root owns. `http.ErrServerClosed` is the expected end of a shutdown and is
not reported; the channel closes when serving stops.

## Lifecycle wiring

The package registers no lifecycle hooks of its own and owns no shutdown timeout. `Start` and
`Shutdown` match the hook signature of go-core's lifecycle coordinator, so the composition root
registers them as bare method values. The coordinator cancels the run context and then invokes each
shutdown hook with a fresh timeout-bounded drain context, which `Shutdown(ctx)` passes directly to
`http.Server.Shutdown` — a private timeout and a cancellation guard have no counterpart here.
`Shutdown` before a successful `Start` is a no-op that leaves the server startable, so the drain
that follows a failed startup passes through cleanly; once it has served, a `Server` is single-use.
