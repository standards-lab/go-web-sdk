package uploadtest

import (
	"bytes"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"sync"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk/upload"
)

const (
	mediaType = "application/pdf"
	name      = "report.pdf"
)

var digest = hex.EncodeToString(make([]byte, sha256.Size))

// Run runs the conformance suite against the stores newStore returns, one
// subtest per behavior and a fresh store for each. A store passes when
// every subtest does. The expiry subtests create uploads that expire in
// the past and a quarter second ahead, so a store compares expiry with
// the time it serves a call at.
func Run(t *testing.T, newStore func() upload.Store) {
	t.Helper()
	checks := []struct {
		name string
		run  func(*testing.T, upload.Store)
	}{
		{"Create", testCreate},
		{"CreateExisting", testCreateExisting},
		{"ConditionalCommit", testConditionalCommit},
		{"RacingAppends", testRacingAppends},
		{"HashState", testHashState},
		{"Assembled", testAssembled},
		{"LengthOnce", testLengthOnce},
		{"Delete", testDelete},
		{"Missing", testMissing},
		{"Expired", testExpired},
		{"Expiring", testExpiring},
	}
	for _, c := range checks {
		t.Run(c.name, func(t *testing.T) { c.run(t, newStore()) })
	}
}

func hour() time.Time { return time.Now().Add(time.Hour).Truncate(time.Second) }

func create(t *testing.T, s upload.Store, id string, length int64) {
	t.Helper()
	if err := s.Create(t.Context(), id, mediaType, digest, name, length, hour()); err != nil {
		t.Fatalf("Create(%q) = %v", id, err)
	}
}

func appendChunk(t *testing.T, s upload.Store, id string, offset int64, chunk, state []byte) (int64, bool) {
	t.Helper()
	cur, ok, err := s.Append(t.Context(), id, offset, chunk, state)
	if err != nil {
		t.Fatalf("Append(%q, %d) = %v", id, offset, err)
	}
	return cur, ok
}

func progress(t *testing.T, s upload.Store, id string) (int64, int64, []byte) {
	t.Helper()
	offset, length, state, _, err := s.Progress(t.Context(), id)
	if err != nil {
		t.Fatalf("Progress(%q) = %v", id, err)
	}
	return offset, length, state
}

func contents(t *testing.T, s upload.Store, id string) []byte {
	t.Helper()
	r, err := s.Open(t.Context(), id)
	if err != nil {
		t.Fatalf("Open(%q) = %v", id, err)
	}
	defer func() { _ = r.Close() }()
	b, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("reading Open(%q) = %v", id, err)
	}
	return b
}

func testCreate(t *testing.T, s upload.Store) {
	expires := hour()
	if err := s.Create(t.Context(), "a", mediaType, digest, name, 42, expires); err != nil {
		t.Fatalf("Create = %v", err)
	}
	mt, d, n, err := s.Meta(t.Context(), "a")
	if err != nil || mt != mediaType || d != digest || n != name {
		t.Errorf("Meta = %q, %q, %q, %v; want %q, %q, %q", mt, d, n, err, mediaType, digest, name)
	}
	offset, length, state, exp, err := s.Progress(t.Context(), "a")
	if err != nil || offset != 0 || length != 42 || len(state) != 0 || !exp.Equal(expires) {
		t.Errorf("Progress = %d, %d, %x, %v, %v; want 0, 42, empty, %v", offset, length, state, exp, err, expires)
	}
	if b := contents(t, s, "a"); len(b) != 0 {
		t.Errorf("Open before any append read %q; want nothing", b)
	}
}

func testCreateExisting(t *testing.T, s upload.Store) {
	create(t, s, "a", -1)
	if err := s.Create(t.Context(), "a", mediaType, digest, name, -1, hour()); !errors.Is(err, fs.ErrExist) {
		t.Errorf("second Create = %v; want fs.ErrExist", err)
	}
}

func testConditionalCommit(t *testing.T, s upload.Store) {
	create(t, s, "a", -1)
	if cur, ok := appendChunk(t, s, "a", 0, []byte("abc"), []byte("s1")); !ok || cur != 3 {
		t.Fatalf("Append at 0 = %d, %v; want 3, true", cur, ok)
	}
	for _, at := range []int64{0, 2, 5} {
		if cur, ok := appendChunk(t, s, "a", at, []byte("xyz"), []byte("bad")); ok || cur != 3 {
			t.Errorf("Append at %d with the offset at 3 = %d, %v; want 3, false", at, cur, ok)
		}
	}
	if offset, _, state := progress(t, s, "a"); offset != 3 || string(state) != "s1" {
		t.Errorf("Progress after refusals = %d, %q; want 3, %q", offset, state, "s1")
	}
	if cur, ok := appendChunk(t, s, "a", 3, []byte("de"), []byte("s2")); !ok || cur != 5 {
		t.Errorf("Append at 3 = %d, %v; want 5, true", cur, ok)
	}
	if b := contents(t, s, "a"); string(b) != "abcde" {
		t.Errorf("Open read %q; want %q", b, "abcde")
	}
}

func testRacingAppends(t *testing.T, s upload.Store) {
	chunks := [2][]byte{[]byte("aaa"), []byte("bbbbb")}
	for n := range 20 {
		id := fmt.Sprint("race-", n)
		create(t, s, id, -1)
		type result struct {
			cur int64
			ok  bool
			err error
		}
		var results [2]result
		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := range 2 {
			wg.Go(func() {
				<-start
				cur, ok, err := s.Append(t.Context(), id, 0, chunks[i], chunks[i])
				results[i] = result{cur, ok, err}
			})
		}
		close(start)
		wg.Wait()
		for i, r := range results {
			if r.err != nil {
				t.Fatalf("racing Append %d = %v", i, r.err)
			}
		}
		if results[0].ok == results[1].ok {
			t.Fatalf("racing Appends committed %v and %v; want exactly one", results[0].ok, results[1].ok)
		}
		w, l := 0, 1
		if results[1].ok {
			w, l = 1, 0
		}
		want := int64(len(chunks[w]))
		if results[w].cur != want || results[l].cur != want {
			t.Fatalf("racing Appends returned %d (winner) and %d (loser); want %d for both", results[w].cur, results[l].cur, want)
		}
		if offset, _, state := progress(t, s, id); offset != want || !bytes.Equal(state, chunks[w]) {
			t.Fatalf("Progress after the race = %d, %q; want %d, %q", offset, state, want, chunks[w])
		}
		if b := contents(t, s, id); !bytes.Equal(b, chunks[w]) {
			t.Fatalf("Open after the race read %q; want %q", b, chunks[w])
		}
	}
}

func testHashState(t *testing.T, s upload.Store) {
	create(t, s, "a", -1)
	parts := [][]byte{[]byte("hello, "), []byte("resumable "), []byte("world")}
	var offset int64
	for _, p := range parts {
		h := sha256.New()
		if _, _, state := progress(t, s, "a"); len(state) > 0 {
			if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
				t.Fatalf("restoring the stored state at %d: %v", offset, err)
			}
		}
		h.Write(p)
		state, err := h.(encoding.BinaryMarshaler).MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		sent := bytes.Clone(state)
		cur, ok := appendChunk(t, s, "a", offset, p, state)
		if !ok {
			t.Fatalf("Append at %d refused at %d", offset, cur)
		}
		clear(state)
		offset = cur
		if got, _, stored := progress(t, s, "a"); got != offset || !bytes.Equal(stored, sent) {
			t.Fatalf("Progress = %d, %x; want %d and the state Append stored, %x", got, stored, offset, sent)
		}
	}
	h := sha256.New()
	_, _, state := progress(t, s, "a")
	if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
		t.Fatal(err)
	}
	want := sha256.Sum256(bytes.Join(parts, nil))
	if got := h.Sum(nil); !bytes.Equal(got, want[:]) {
		t.Errorf("the stored state sums to %x; want %x", got, want)
	}
}

func testAssembled(t *testing.T, s upload.Store) {
	create(t, s, "a", 9)
	create(t, s, "b", -1)
	buf := []byte("abc")
	var offset int64
	for _, p := range []string{"abc", "def", "ghi"} {
		copy(buf, p)
		offset, _ = appendChunk(t, s, "a", offset, buf, nil)
	}
	appendChunk(t, s, "b", 0, []byte("other"), nil)
	if b := contents(t, s, "a"); string(b) != "abcdefghi" {
		t.Errorf("Open read %q; want %q", b, "abcdefghi")
	}
	if b := contents(t, s, "b"); string(b) != "other" {
		t.Errorf("Open of a second upload read %q; want %q", b, "other")
	}
}

func testLengthOnce(t *testing.T, s upload.Store) {
	create(t, s, "a", -1)
	if _, length, _ := progress(t, s, "a"); length != -1 {
		t.Errorf("Progress length before it is known = %d; want -1", length)
	}
	for _, c := range []struct{ set, want int64 }{{10, 10}, {12, 10}, {10, 10}} {
		if got, err := s.SetLength(t.Context(), "a", c.set); err != nil || got != c.want {
			t.Errorf("SetLength(%d) = %d, %v; want %d", c.set, got, err, c.want)
		}
	}
	if _, length, _ := progress(t, s, "a"); length != 10 {
		t.Errorf("Progress length = %d; want 10", length)
	}
	create(t, s, "b", 7)
	if got, err := s.SetLength(t.Context(), "b", 9); err != nil || got != 7 {
		t.Errorf("SetLength(9) on a length known at creation = %d, %v; want 7", got, err)
	}
}

func testDelete(t *testing.T, s upload.Store) {
	create(t, s, "a", -1)
	create(t, s, "b", -1)
	appendChunk(t, s, "a", 0, []byte("abc"), nil)
	if err := s.Delete(t.Context(), "a"); err != nil {
		t.Fatalf("Delete = %v", err)
	}
	missing(t, s, "a")
	if offset, _, _ := progress(t, s, "b"); offset != 0 {
		t.Errorf("Progress of another upload = %d; want 0", offset)
	}
}

func testMissing(t *testing.T, s upload.Store) {
	missing(t, s, "none")
}

func testExpired(t *testing.T, s upload.Store) {
	if err := s.Create(t.Context(), "a", mediaType, digest, name, 3, time.Now().Add(-time.Minute)); err != nil {
		t.Fatalf("Create = %v", err)
	}
	missing(t, s, "a")
}

func testExpiring(t *testing.T, s upload.Store) {
	if err := s.Create(t.Context(), "a", mediaType, digest, name, 3, time.Now().Add(250*time.Millisecond)); err != nil {
		t.Fatalf("Create = %v", err)
	}
	deadline := time.NewTimer(10 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(25 * time.Millisecond)
	defer tick.Stop()
	for {
		_, _, _, _, err := s.Progress(t.Context(), "a")
		if errors.Is(err, fs.ErrNotExist) {
			break
		}
		if err != nil {
			t.Fatalf("Progress before expiry = %v", err)
		}
		select {
		case <-deadline.C:
			t.Fatal("the upload still answers 10s after its expiry")
		case <-tick.C:
		}
	}
	missing(t, s, "a")
}

func missing(t *testing.T, s upload.Store, id string) {
	t.Helper()
	ctx := t.Context()
	check := func(op string, err error) {
		t.Helper()
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s(%q) = %v; want fs.ErrNotExist", op, id, err)
		}
	}
	_, _, _, err := s.Meta(ctx, id)
	check("Meta", err)
	_, _, _, _, err = s.Progress(ctx, id)
	check("Progress", err)
	_, err = s.SetLength(ctx, id, 3)
	check("SetLength", err)
	_, _, err = s.Append(ctx, id, 0, []byte("abc"), nil)
	check("Append", err)
	r, err := s.Open(ctx, id)
	if r != nil {
		_ = r.Close()
	}
	check("Open", err)
	check("Delete", s.Delete(ctx, id))
}
