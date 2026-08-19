# Tests and documentation

How the module is tested and how its API documentation is owned.

## Tests: co-located, black-box, hermetic

Tests are `{file}_test.go` files co-located with the source they cover, in an external test package
(`package <pkg>_test`). They exercise only the public API; private infrastructure is covered
transitively through the public entry points that use it.

They are hermetic. Handlers are exercised through `httptest` recorders with no listener; the server
tests that must listen bind port 0 and read the assignment back through `Server.Addr`, never a fixed
port. CI needs no network fixture.

A helper stays in its test package until more than one test package needs it; then it is hoisted
into `internal/webtest`, the module-private test-infrastructure package. `internal/` keeps it
invisible to consumers — nothing in it is API — and the stdlib's `<domain>test` naming
(`httptest`, `fstest`, `iotest`) is the model. Promoting it to a public `webtest` package would be
an ordinary additive change, made only when a consumer demonstrates the need.

## doc.go and godoc

Production source is written without doc comments; the agent writes godoc. Each package has exactly
one `doc.go` holding only the package comment, and that comment is authoritative for the package's
API.
