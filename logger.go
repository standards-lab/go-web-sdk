package web

import (
	"io"
	"log/slog"
	"net/http"
	"time"
)

// RequestLogger emits one record per request — method, path, status,
// duration, remote address — at info level. A successful request to
// [HealthPath] or [ReadyPath] logs at debug, keeping orchestrator heartbeat
// out of production logs while a failing probe stays visible; a panicking
// handler logs at error with the panic value attached before the panic
// continues to net/http's recovery. The wrapped ResponseWriter records the
// first status written, implements Unwrap so http.ResponseController reaches
// through it, and delegates io.ReaderFrom.
func RequestLogger(logger *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			recorder := &statusRecorder{
				ResponseWriter: w,
				status:         http.StatusOK,
			}

			defer func() {
				attrs := []slog.Attr{
					slog.String("method", r.Method),
					slog.String("path", r.URL.Path),
					slog.Int("status", recorder.status),
					slog.Duration("duration", time.Since(start)),
					slog.String("remote_addr", r.RemoteAddr),
				}
				if rec := recover(); rec != nil {
					attrs = append(attrs, slog.Any("panic", rec))
					logger.LogAttrs(
						r.Context(),
						slog.LevelError,
						"request",
						attrs...,
					)
					panic(rec)
				}
				level := slog.LevelInfo
				probe := r.URL.Path == HealthPath ||
					r.URL.Path == ReadyPath
				if probe && recorder.status >= 200 && recorder.status < 300 {
					level = slog.LevelDebug
				}
				logger.LogAttrs(
					r.Context(),
					level,
					"request",
					attrs...,
				)
			}()

			next.ServeHTTP(recorder, r)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	wrote  bool
}

func (s *statusRecorder) WriteHeader(code int) {
	if !s.wrote {
		s.status = code
		s.wrote = true
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) ReadFrom(src io.Reader) (int64, error) {
	if rf, ok := s.ResponseWriter.(io.ReaderFrom); ok {
		return rf.ReadFrom(src)
	}
	return io.Copy(s.ResponseWriter, src)
}

func (s *statusRecorder) Unwrap() http.ResponseWriter {
	return s.ResponseWriter
}
