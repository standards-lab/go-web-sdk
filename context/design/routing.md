# Routing

The design of the routing layer — `Group`, `Module`, `Router`. The code and `doc.go` are
authoritative for the API; this note records the decisions.

## Compose once, at wiring time

`NewModule` compiles a group tree into full mux patterns in one pass: every route registers under
its complete pattern with its middleware chain baked in — group middleware outermost, ordered root
to leaf, then per-route middleware. Nothing recomposes or validates per request, and nothing
rewrites a request path except `NewHandlerModule`'s stdlib `StripPrefix`, the one module kind that
exists to mount a foreign handler under a prefix.

Compile-once creates its own hazard — a route registered after compilation would be silently dead —
so compilation seals the group tree, and a later `Use`, `Handle`, or `Mount` panics instead.

## One canonical path

A group becomes servable one way (`NewModule`) and mounts one way (`Router.Mount`). Multi-segment
prefixes such as `/api/v1` are first-class: a prefix must begin with `/` and not end with one, and a
malformed prefix panics at construction.

## Probes stay outside the modules structurally

`Router.Handle` mirrors `ServeMux.Handle` on the router's native fallback mux, which makes `*Router`
satisfy `Mounter`, so `RegisterHealth` mounts the probes beyond every module's middleware: they need
no exemption from authentication or logging policy inside a module because a probe request never
enters one. Router-level middleware registered through `Router.Use` still wraps the whole dispatch,
modules and fallback alike.

Dispatch itself is longest-prefix match on segment boundaries — `/api/v10` does not match a module
mounted at `/api/v1` — with the native mux answering every path no module owns.

## Registration mistakes panic at wiring time

A malformed prefix, a duplicate pattern, a second module at a taken prefix, and a sealed-group
mutation all panic. The composition root is written once and runs at boot, so a loud failure there
beats a silent one in production — the same posture go-core's lifecycle coordinator takes toward a
late registration.
