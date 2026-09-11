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

func TestRecorder_1xxStatusDoesNotCommit(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusEarlyHints)

	if rec.Committed() {
		t.Error("Committed() = true after a 1xx status")
	}
	if rec.Status() != 0 {
		t.Errorf("Status() = %d, want 0", rec.Status())
	}
}

func TestRecorder_SwitchingProtocolsCommits(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusSwitchingProtocols)

	if !rec.Committed() {
		t.Error("Committed() = false after 101 Switching Protocols")
	}
	if rec.Status() != http.StatusSwitchingProtocols {
		t.Errorf("Status() = %d, want %d", rec.Status(), http.StatusSwitchingProtocols)
	}
}

func TestRecorder_1xxThenTheFinalStatusCommitsTheFinal(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusEarlyHints)
	rec.WriteHeader(http.StatusCreated)

	if !rec.Committed() {
		t.Error("Committed() = false after the final status")
	}
	if rec.Status() != http.StatusCreated {
		t.Errorf("Status() = %d, want the final %d", rec.Status(), http.StatusCreated)
	}
}

func TestRecorder_FlushCommitsAnImplicitOK(t *testing.T) {
	under := httptest.NewRecorder()
	rec := web.WrapWriter(under)

	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if !rec.Committed() {
		t.Error("Committed() = false after a Flush with no prior WriteHeader")
	}
	if rec.Status() != http.StatusOK {
		t.Errorf("Status() = %d, want %d", rec.Status(), http.StatusOK)
	}
	if !under.Flushed {
		t.Error("the underlying writer was not flushed")
	}
}

func TestRecorder_FlushAfterWriteHeaderKeepsTheExplicitStatus(t *testing.T) {
	rec := web.WrapWriter(httptest.NewRecorder())
	rec.WriteHeader(http.StatusAccepted)

	if err := http.NewResponseController(rec).Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if rec.Status() != http.StatusAccepted {
		t.Errorf("Status() = %d, want the explicit %d", rec.Status(), http.StatusAccepted)
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
