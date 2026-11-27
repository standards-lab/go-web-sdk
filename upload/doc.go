// Package upload implements resumable uploads as
// draft-ietf-httpbis-resumable-upload-16 defines them, with SHA-256 digests
// as RFC 9530 carries them. This comment lists every exported name; each
// symbol's own documentation states its contract.
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
