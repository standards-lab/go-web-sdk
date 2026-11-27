package uploadtest_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/standards-lab/go-web-sdk/upload"
	"github.com/standards-lab/go-web-sdk/upload/uploadtest"
)

const brokenEnv = "UPLOADTEST_BROKEN"

type unconditional struct{ *upload.MemoryStore }

func (s unconditional) Append(ctx context.Context, id string, _ int64, chunk, state []byte) (int64, bool, error) {
	offset, _, _, _, err := s.Progress(ctx, id)
	if err != nil {
		return 0, false, err
	}
	return s.MemoryStore.Append(ctx, id, offset, chunk, state)
}

type stateless struct{ *upload.MemoryStore }

func (s stateless) Append(ctx context.Context, id string, offset int64, chunk, _ []byte) (int64, bool, error) {
	return s.MemoryStore.Append(ctx, id, offset, chunk, nil)
}

type immortal struct{ *upload.MemoryStore }

func (s immortal) Create(ctx context.Context, id, mediaType, digest, name string, length int64, expires time.Time) error {
	if expires.Before(time.Now()) {
		expires = time.Now().Add(time.Hour)
	}
	return s.MemoryStore.Create(ctx, id, mediaType, digest, name, length, expires)
}

type undeletable struct{ *upload.MemoryStore }

func (undeletable) Delete(context.Context, string) error { return nil }

type opaque struct{ *upload.MemoryStore }

func (s opaque) Meta(ctx context.Context, id string) (string, string, string, error) {
	mt, d, n, err := s.MemoryStore.Meta(ctx, id)
	if err != nil {
		err = errors.New(err.Error())
	}
	return mt, d, n, err
}

var broken = map[string]struct {
	store func() upload.Store
	fails string
}{
	"unconditional": {func() upload.Store { return unconditional{upload.NewMemoryStore()} }, "ConditionalCommit"},
	"stateless":     {func() upload.Store { return stateless{upload.NewMemoryStore()} }, "HashState"},
	"immortal":      {func() upload.Store { return immortal{upload.NewMemoryStore()} }, "Expired"},
	"undeletable":   {func() upload.Store { return undeletable{upload.NewMemoryStore()} }, "Delete"},
	"opaque":        {func() upload.Store { return opaque{upload.NewMemoryStore()} }, "Missing"},
}

func TestBrokenStore(t *testing.T) {
	b, ok := broken[os.Getenv(brokenEnv)]
	if !ok {
		t.Skip("runs only as the subprocess TestRunDetectsBrokenStores starts")
	}
	uploadtest.Run(t, b.store)
}

func TestRunDetectsBrokenStores(t *testing.T) {
	for name, b := range broken {
		t.Run(name, func(t *testing.T) {
			cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestBrokenStore$", "-test.v")
			cmd.Env = append(os.Environ(), brokenEnv+"="+name)
			out, err := cmd.CombinedOutput()
			if _, ok := errors.AsType[*exec.ExitError](err); !ok {
				t.Fatalf("the suite against %s exited with %v; want a failure\n%s", name, err, out)
			}
			if want := "--- FAIL: TestBrokenStore/" + b.fails + " "; !strings.Contains(string(out), want) {
				t.Errorf("the suite against %s did not fail %s\n%s", name, b.fails, out)
			}
		})
	}
}
