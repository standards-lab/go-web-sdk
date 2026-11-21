# Routing misses

How a [`Router`](../router.go) and a [`Module`](../module.go) answer a request no route matches,
and why.

## Discriminating a miss

`http.ServeMux` has no not-found hook, and a catch-all pattern would defeat its 405. So a request
whose `ServeMux.Handler` returns an empty pattern, a miss, runs ServeMux's own handler into a
recorder that keeps its status and header and discards its body, and the recorded status picks
the answer: a redirect as is, a 405 with its `Allow`, else a 404. The miss path mirrors the guard
`ServeMux.ServeHTTP` applies before matching, which `Handler` skips.

A match is served through `ServeMux.ServeHTTP`, the only way to set `http.Request.Pattern` and
the path values, so the hit path matches twice.

The default miss handlers answer with the status's undecorated problem. Their encoder's error is
dropped: nothing in the miss path logs. `middleware.ContentType`'s 415 drops its encoder's error
the same way.

## Miss handlers belong to the module

A module's miss handlers are read from its root group alone. One set on a nested group would be
dead wiring, so `NewModule` panics on it like every other registration mistake.
