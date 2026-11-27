package upload

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"slices"
	"sync"
	"time"
)

// MemoryStore is a [Store] that holds every upload in the memory of one
// process. It is unfit for a service with more than one replica, since an
// append that reaches another replica finds no upload there; such a
// service writes a Store over storage its replicas share. Its zero value
// is not ready for use: [NewMemoryStore] makes one.
type MemoryStore struct {
	mu      sync.Mutex
	uploads map[string]*memoryUpload
}

type memoryUpload struct {
	mediaType, digest, name string
	length                  int64
	expires                 time.Time
	offset                  int64
	state                   []byte
	chunks                  [][]byte
}

// NewMemoryStore returns an empty [MemoryStore].
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{uploads: map[string]*memoryUpload{}}
}

// Create implements [Store].
func (m *MemoryStore) Create(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	for k, u := range m.uploads {
		if !now.Before(u.expires) {
			delete(m.uploads, k)
		}
	}
	if _, ok := m.uploads[id]; ok {
		return &fs.PathError{Op: "create", Path: id, Err: fs.ErrExist}
	}
	m.uploads[id] = &memoryUpload{
		mediaType: mediaType, digest: digest, name: name,
		length: length, expires: expires,
	}
	return nil
}

// Meta implements [Store].
func (m *MemoryStore) Meta(ctx context.Context, id string) (mediaType, digest, name string, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.find(ctx, "meta", id)
	if err != nil {
		return "", "", "", err
	}
	return u.mediaType, u.digest, u.name, nil
}

// Progress implements [Store].
func (m *MemoryStore) Progress(ctx context.Context, id string) (offset, length int64, state []byte, expires time.Time, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.find(ctx, "progress", id)
	if err != nil {
		return 0, 0, nil, time.Time{}, err
	}
	return u.offset, u.length, slices.Clone(u.state), u.expires, nil
}

// SetLength implements [Store].
func (m *MemoryStore) SetLength(ctx context.Context, id string, length int64) (int64, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.find(ctx, "setlength", id)
	if err != nil {
		return 0, err
	}
	if u.length < 0 {
		u.length = length
	}
	return u.length, nil
}

// Append implements [Store].
func (m *MemoryStore) Append(ctx context.Context, id string, offset int64, chunk, state []byte) (current int64, committed bool, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.find(ctx, "append", id)
	if err != nil {
		return 0, false, err
	}
	if u.offset != offset {
		return u.offset, false, nil
	}
	u.chunks = append(u.chunks, slices.Clone(chunk))
	u.offset += int64(len(chunk))
	u.state = slices.Clone(state)
	return u.offset, true, nil
}

// Open implements [Store].
func (m *MemoryStore) Open(ctx context.Context, id string) (io.ReadCloser, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	u, err := m.find(ctx, "open", id)
	if err != nil {
		return nil, err
	}
	readers := make([]io.Reader, len(u.chunks))
	for i, c := range u.chunks {
		readers[i] = bytes.NewReader(c)
	}
	return io.NopCloser(io.MultiReader(readers...)), nil
}

// Delete implements [Store].
func (m *MemoryStore) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, err := m.find(ctx, "delete", id); err != nil {
		return err
	}
	delete(m.uploads, id)
	return nil
}

func (m *MemoryStore) find(ctx context.Context, op, id string) (*memoryUpload, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	u, ok := m.uploads[id]
	if ok && !time.Now().Before(u.expires) {
		delete(m.uploads, id)
		ok = false
	}
	if !ok {
		return nil, &fs.PathError{Op: op, Path: id, Err: fs.ErrNotExist}
	}
	return u, nil
}
