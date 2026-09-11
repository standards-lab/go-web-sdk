package web_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/standards-lab/go-web-sdk"
)

func TestWrapWriter_IsIdempotent(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	if web.WrapWriter(rec) != rec {
		t.Error("WrapWriter(rec) returned a new wrapper instead of rec")
	}
}

func TestRecorder_WriteHeaderCommits(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusAccepted)

	if !rec.Committed() {
		t.Error("Committed() = false after WriteHeader")
	}
	if rec.Status() != http.StatusAccepted {
		t.Errorf("Status() = %d, want %d", rec.Status(), http.StatusAccepted)
	}
}

func TestRecorder_WriteHeaderFirstCallWins(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusAccepted)
	rec.WriteHeader(http.StatusInternalServerError)

	if rec.Status() != http.StatusAccepted {
		t.Errorf("Status() = %d, want the first status %d", rec.Status(), http.StatusAccepted)
	}
}

func TestRecorder_WriteCommitsAnImplicitOK(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	if _, err := rec.Write([]byte("body")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if !rec.Committed() {
		t.Error("Committed() = false after Write with no prior WriteHeader")
	}
	if rec.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", rec.Status(), http.StatusOK)
	}
}

func TestRecorder_WriteAfterWriteHeaderKeepsTheExplicitStatus(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusAccepted)
	if _, err := rec.Write([]byte("body")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	if rec.Status() != http.StatusAccepted {
		t.Errorf("Status() = %d, want the explicit %d", rec.Status(), http.StatusAccepted)
	}
}

func TestRecorder_ReadFromCommitsAnImplicitOK(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	if _, err := rec.ReadFrom(strings.NewReader("streamed")); err != nil {
		t.Fatalf("ReadFrom: %v", err)
	}

	if !rec.Committed() {
		t.Error("Committed() = false after ReadFrom with no prior WriteHeader")
	}
	if rec.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", rec.Status(), http.StatusOK)
	}
}

func TestRecorder_ImplementsReaderFrom(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	if _, ok := any(rec).(io.ReaderFrom); !ok {
		t.Error("Recorder does not implement io.ReaderFrom")
	}
}

func TestRecorder_UnwrapReachesTheUnderlyingWriter(t *testing.T) {
	under := httptest.NewRecorder()
	rec := web.WrapWriter(under)

	if got := rec.Unwrap(); got != http.ResponseWriter(under) {
		t.Errorf("Unwrap() = %v, want the underlying writer", got)
	}
}

func TestRecorder_StatusIsZeroBeforeAnyWrite(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())

	if rec.Committed() {
		t.Error("Committed() = true before any write")
	}
	if rec.Status() != 0 {
		t.Errorf("Status() = %d, want 0", rec.Status())
	}
}
