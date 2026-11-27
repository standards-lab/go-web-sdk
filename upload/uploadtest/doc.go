// Package uploadtest is the conformance suite for an [upload.Store]. A
// module that writes a store over its own storage runs the suite against
// it from a test, so the resumable-upload handler can rely on what the
// suite checks: the conditional commit of an append and the race it
// settles, the hash state kept with each offset, the length recorded
// once, the file assembled in order, deletion, expiry, and fs.ErrNotExist
// for a missing upload. This comment lists every exported name; each
// symbol's own documentation states its contract.
//
//   - [Run] runs the suite against the stores a constructor returns.
package uploadtest
