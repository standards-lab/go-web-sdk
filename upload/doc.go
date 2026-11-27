// Package upload implements resumable uploads as
// draft-ietf-httpbis-resumable-upload-16 defines them, with SHA-256 digests
// as RFC 9530 carries them. This comment lists every exported name; each
// symbol's own documentation states its contract.
//
// # Handler
//
// The handler serves the draft's upload creation, offset retrieval,
// append, and cancellation on net/http, under a prefix the consumer
// mounts it at. It appends a chunk only once a digest covering it
// matches, so an interrupted request appends nothing, and it leaves two
// appends racing at one offset to the store's conditional commit.
//
//   - [Handler] is the handler's configuration, every field required, and
//     [Handler.Routes] returns the web.Group that serves it.
//   - [File] is what the handler kept of a completed upload, passed to
//     the Complete function that writes the completing response.
//
// # Stores
//
// An upload's metadata, chunks, offset, and running SHA-256 state live in
// a store, which commits each append only at the offset it names.
//
//   - [Store] is the interface a store satisfies, in standard library
//     types only; package uploadtest is its conformance suite.
//   - [MemoryStore] is a Store within one process, and [NewMemoryStore]
//     makes one.
//
// # Digests
//
// The Repr-Digest of a whole file and the Content-Digest of one chunk each
// carry a SHA-256 as the sha-256 member of a Structured Field Dictionary,
// a Byte Sequence; everywhere else a SHA-256 is lowercase hex.
//
//   - [FormatDigest] writes a field value from a hex SHA-256.
//   - [ParseDigest] reads the hex SHA-256 back from a field value.
//
// # Structured fields
//
// The draft's fields and RFC 9530's are RFC 9651 Structured Fields. The
// package reads and writes the types they use: Integer and Boolean Items,
// and Dictionaries whose members are Integers, Booleans, or Byte
// Sequences. A parser accepts any well-formed field, ignoring parameters,
// and reports a malformed one as not parsed, which the draft has a
// receiver ignore.
//
//   - [ParseInteger] and [FormatInteger] read and write an Integer Item.
//   - [ParseBoolean] and [FormatBoolean] read and write a Boolean Item.
//   - [ParseDictionary] and [FormatDictionary] read and write a
//     [Dictionary], an ordered list of [Member] keys and values.
package upload
