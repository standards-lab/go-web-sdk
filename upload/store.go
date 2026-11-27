package upload

import (
	"context"
	"io"
	"time"
)

// Store keeps resumable uploads: each upload's metadata, its chunks in
// order, its offset, and the running SHA-256 state the chunks so far leave.
// Its methods use only standard library types, so a store written in
// another module satisfies it without importing this package.
//
// Every method returns an error that matches [io/fs.ErrNotExist] under
// errors.Is when the upload is missing, which an upload deleted or past
// its expiry is. A store is safe for concurrent use, across every process
// that shares it: package uploadtest's suite holds a store to this
// contract.
type Store interface {
	// Create records a new upload under id, which the caller mints. It
	// keeps the file's media type, its SHA-256 in lowercase hex as the
	// creation's Repr-Digest carries it, its file name, its length in
	// bytes or -1 when the length is not yet known, and the time the
	// upload expires. The upload starts at offset 0 with an empty hash
	// state. An id already in use returns an error that matches
	// [io/fs.ErrExist].
	Create(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error

	// Meta returns what Create kept of the upload that does not change:
	// its media type, digest, and file name.
	Meta(ctx context.Context, id string) (mediaType, digest, name string, err error)

	// Progress returns the upload's offset, its length or -1 when the
	// length is not yet known, the hash state the last committed Append
	// stored (empty at offset 0), and the time it expires.
	Progress(ctx context.Context, id string) (offset, length int64, state []byte, expires time.Time, err error)

	// SetLength records the upload's length once it becomes known and
	// returns the length recorded. A length is recorded once: when one is
	// already recorded, SetLength leaves it and returns it, and the caller
	// compares it with the length it named.
	SetLength(ctx context.Context, id string, length int64) (int64, error)

	// Append commits chunk at offset only when the upload's offset is
	// offset, and stores offset+len(chunk) and state, the running SHA-256
	// state after chunk, with it in one atomic step. It returns the
	// upload's offset after the call and whether it committed. A refusal
	// is not an error: two appends racing at one offset commit exactly
	// one, and the other returns false with the winner's offset. Append
	// retains neither slice after it returns.
	Append(ctx context.Context, id string, offset int64, chunk, state []byte) (current int64, committed bool, err error)

	// Open returns the chunks committed so far, in offset order, as one
	// reader.
	Open(ctx context.Context, id string) (io.ReadCloser, error)

	// Delete removes the upload and its chunks.
	Delete(ctx context.Context, id string) error
}
