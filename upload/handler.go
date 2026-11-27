package upload

import (
	"context"
	"crypto/sha256"
	"encoding"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"
	"uuid"

	"github.com/standards-lab/go-web-sdk"
)

// See https://datatracker.ietf.org/doc/html/draft-ietf-httpbis-resumable-upload-16

const (
	interopVersion = 11

	statusUploadResumptionSupported = 104

	partialUpload = "application/partial-upload"

	problemMismatchingOffset  = "https://iana.org/assignments/http-problem-types#mismatching-upload-offset"
	problemInconsistentLength = "https://iana.org/assignments/http-problem-types#inconsistent-upload-length"

	fieldComplete        = "Upload-Complete"
	fieldOffset          = "Upload-Offset"
	fieldLength          = "Upload-Length"
	fieldLimit           = "Upload-Limit"
	fieldInterop         = "Upload-Draft-Interop-Version"
	fieldReprDigest      = "Repr-Digest"
	fieldContentDigest   = "Content-Digest"
	fieldContentEncoding = "Content-Encoding"
)

// Handler serves resumable uploads over a [Store]: the creating POST, and
// the HEAD, PATCH, and DELETE of each upload resource. Every field is
// required; [Handler.Routes] panics, naming the field, when one is missing.
//
// The handler holds no lock of its own. Two requests appending at one
// offset, whichever replicas serve them, are settled by the Store's
// conditional [Store.Append]: one commits, and the other is answered 409
// with the offset the winner left.
type Handler struct {
	// Store keeps the uploads. Replicas that serve one upload share it.
	Store Store

	// Transfer sets the connection deadlines of each request's content.
	// Its limit is the max-append-size the handler announces and enforces:
	// the most content one creation or append carries, a whole file sent
	// with Upload-Complete: ?1 included.
	Transfer web.Transfer

	// Lifetime is how long an upload lives from its creation. An upload
	// past it answers as missing, 404.
	Lifetime time.Duration

	// Complete writes the final response of the request that completes an
	// upload, once its content is appended. The handler has already set
	// Upload-Complete: ?1 on w. An error it returns is written as a
	// problem, as an error from any of the handler's routes is.
	Complete func(w http.ResponseWriter, r *http.Request, f File) error

	// Logger receives the cause of every 5xx the handler sends.
	Logger *slog.Logger
}

// File is what the handler kept of an upload that completed, for
// [Handler.Complete]: its identifier, under which the Store holds its
// content; the Content-Type, Repr-Digest SHA-256 in lowercase hex, and
// Content-Disposition file name its creation carried; and its length.
type File struct {
	ID        string
	MediaType string
	Digest    string
	Name      string
	Length    int64
}

// Routes returns the group that serves the uploads under prefix, for the
// consumer to mount: a POST to the prefix creates an upload, and the
// upload resource is prefix/{id}, its UUIDv4. Location carries the
// creating request's own path with the id appended, so it resolves
// wherever the group is mounted. Routes panics on a missing field and on a
// Transfer whose limit is not a positive structured field Integer.
func (h Handler) Routes(prefix string) *web.Group {
	switch {
	case h.Store == nil:
		panic("upload: Handler requires a Store")
	case h.Transfer == web.Transfer{}:
		panic("upload: Handler requires a Transfer")
	case h.Lifetime <= 0:
		panic("upload: Handler requires a positive Lifetime")
	case h.Complete == nil:
		panic("upload: Handler requires a Complete function")
	case h.Logger == nil:
		panic("upload: Handler requires a Logger")
	}
	limit := h.Transfer.Limit()
	if _, err := FormatInteger(limit); err != nil || limit <= 0 {
		panic(fmt.Sprintf("upload: Handler requires a Transfer whose limit is a positive structured field Integer, not %d", limit))
	}
	s := &server{h: h, limit: limit}
	g := web.NewGroup(prefix)
	g.SetErrorWriter(web.NewErrorWriter(h.Logger, missing))
	g.HandleErr(http.MethodPost, "", s.create)
	g.HandleErr(http.MethodHead, "/{id}", s.head)
	g.HandleErr(http.MethodPatch, "/{id}", s.append)
	g.HandleErr(http.MethodDelete, "/{id}", s.cancel)
	return g
}

type server struct {
	h     Handler
	limit int64
}

func (s *server) create(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set(fieldComplete, FormatBoolean(false))
	complete, ok := booleanField(r, fieldComplete)
	if !ok {
		return badRequest("the creation requires a valid Upload-Complete")
	}
	if r.Header.Get(fieldContentEncoding) != "" {
		return unencoded()
	}
	mediaType, err := fileType(r)
	if err != nil {
		return err
	}
	digest, ok := ParseDigest(field(r, fieldReprDigest))
	if !ok {
		return badRequest("the creation requires a Repr-Digest with a sha-256 member")
	}
	name, err := fileName(r)
	if err != nil {
		return err
	}
	length, ok := integerField(r, fieldLength)
	if !ok {
		length = -1
	}
	if err := s.declared(r); err != nil {
		return err
	}
	if r.ContentLength >= 0 && length >= 0 {
		if (complete && r.ContentLength != length) || r.ContentLength > length {
			return inconsistent(fmt.Sprintf("the content's %d bytes do not fit Upload-Length %d", r.ContentLength, length))
		}
	}
	if complete && r.ContentLength >= 0 {
		length = r.ContentLength
	}

	id := uuid.NewV4().String()
	expires := time.Now().Add(s.h.Lifetime)
	if err := s.h.Store.Create(r.Context(), id, mediaType, digest, name, length, expires); err != nil {
		return err
	}
	w.Header().Set("Location", r.URL.EscapedPath()+"/"+id)
	w.Header().Set(fieldLimit, s.limits(expires))
	if version, ok := integerField(r, fieldInterop); ok && version == interopVersion {
		w.Header().Set(fieldInterop, strconv.Itoa(interopVersion))
		w.WriteHeader(statusUploadResumptionSupported)
		w.Header().Del(fieldInterop)
	}

	body, err := s.read(w, r)
	if err != nil {
		return err
	}
	end, err := s.bound(r.Context(), id, 0, length, int64(len(body)), complete)
	if err != nil {
		return err
	}
	checked, err := checkDigest(r, body)
	if err != nil {
		return err
	}
	if !checked && !complete {
		end = 0
	} else if err := s.commit(r.Context(), w, id, 0, nil, body); err != nil {
		return err
	}
	if complete {
		return s.complete(w, r, id, length, end)
	}
	w.Header().Set(fieldOffset, strconv.FormatInt(end, 10))
	w.WriteHeader(http.StatusCreated)
	return nil
}

func (s *server) head(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	offset, length, _, expires, err := s.h.Store.Progress(r.Context(), id)
	if err != nil {
		return err
	}
	w.Header().Set(fieldOffset, strconv.FormatInt(offset, 10))
	w.Header().Set(fieldLimit, s.limits(expires))
	if length >= 0 {
		w.Header().Set(fieldLength, strconv.FormatInt(length, 10))
	}
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) append(w http.ResponseWriter, r *http.Request) error {
	w.Header().Set(fieldComplete, FormatBoolean(false))
	if r.Header.Get(fieldContentEncoding) != "" {
		return unencoded()
	}
	if mt, _, err := mime.ParseMediaType(r.Header.Get("Content-Type")); err != nil || mt != partialUpload {
		return &web.UploadError{Header: "Content-Type", Reason: "an append's Content-Type is " + partialUpload}
	}
	provided, ok := integerField(r, fieldOffset)
	if !ok {
		return badRequest("an append requires a valid Upload-Offset")
	}
	complete, ok := booleanField(r, fieldComplete)
	if !ok {
		return badRequest("an append requires a valid Upload-Complete")
	}
	if err := s.declared(r); err != nil {
		return err
	}
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	offset, length, state, _, err := s.h.Store.Progress(r.Context(), id)
	if err != nil {
		return err
	}
	if provided != offset {
		return conflict(w, offset, provided)
	}
	if named, ok := integerField(r, fieldLength); ok {
		if named < offset {
			return inconsistent(fmt.Sprintf("Upload-Length %d is short of the offset, %d", named, offset))
		}
		if length < 0 {
			if length, err = s.h.Store.SetLength(r.Context(), id, named); err != nil {
				return err
			}
		}
		if named != length {
			return inconsistent(fmt.Sprintf("Upload-Length %d differs from the upload's length, %d", named, length))
		}
	}
	if r.ContentLength >= 0 {
		if _, err := s.bound(r.Context(), id, offset, length, r.ContentLength, complete); err != nil {
			return err
		}
	}

	body, err := s.read(w, r)
	if err != nil {
		return err
	}
	end, err := s.bound(r.Context(), id, offset, length, int64(len(body)), complete)
	if err != nil {
		return err
	}
	checked, err := checkDigest(r, body)
	if err != nil {
		return err
	}
	if !checked && len(body) > 0 {
		return badRequest("an append requires a Content-Digest with a sha-256 member")
	}
	if err := s.commit(r.Context(), w, id, offset, state, body); err != nil {
		return err
	}
	if complete {
		return s.complete(w, r, id, length, end)
	}
	w.Header().Set(fieldOffset, strconv.FormatInt(end, 10))
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) cancel(w http.ResponseWriter, r *http.Request) error {
	id, err := web.PathUUID(r, "id")
	if err != nil {
		return err
	}
	if err := s.h.Store.Delete(r.Context(), id); err != nil {
		return err
	}
	w.WriteHeader(http.StatusNoContent)
	return nil
}

func (s *server) declared(r *http.Request) error {
	if r.ContentLength > s.limit {
		return &web.UploadError{
			TooLarge: true,
			Reason:   fmt.Sprintf("the declared %d bytes exceed max-append-size, %d", r.ContentLength, s.limit),
		}
	}
	return nil
}

func (s *server) read(w http.ResponseWriter, r *http.Request) ([]byte, error) {
	if err := s.h.Transfer.WidenUpload(w, r); err != nil {
		return nil, err
	}
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, s.limit))
	switch {
	case err == nil:
		return body, nil
	case isMaxBytes(err):
		return nil, &web.UploadError{TooLarge: true, Reason: fmt.Sprintf("the content exceeds max-append-size, %d", s.limit)}
	case timedOut(err):
		return nil, web.Problem{Status: http.StatusRequestTimeout, Detail: "the content arrived slower than the transfer rate allows; nothing was appended"}
	default:
		return nil, badRequest("the content broke off before its end; nothing was appended")
	}
}

func (s *server) bound(ctx context.Context, id string, offset, length, n int64, complete bool) (int64, error) {
	end := offset + n
	if length >= 0 && end > length {
		if err := s.h.Store.Delete(ctx, id); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return 0, err
		}
		return 0, inconsistent(fmt.Sprintf("the content runs to %d, past the upload's length, %d; the upload is deactivated", end, length))
	}
	if complete && length >= 0 && end != length {
		return 0, inconsistent(fmt.Sprintf("the completing content ends at %d, not at the upload's length, %d", end, length))
	}
	return end, nil
}

func (s *server) commit(ctx context.Context, w http.ResponseWriter, id string, offset int64, state, chunk []byte) error {
	if len(chunk) == 0 {
		return nil
	}
	next, err := advance(state, chunk)
	if err != nil {
		return err
	}
	current, committed, err := s.h.Store.Append(ctx, id, offset, chunk, next)
	if err != nil {
		return err
	}
	if !committed {
		return conflict(w, current, offset)
	}
	return nil
}

func (s *server) complete(w http.ResponseWriter, r *http.Request, id string, length, end int64) error {
	if length < 0 {
		recorded, err := s.h.Store.SetLength(r.Context(), id, end)
		if err != nil {
			return err
		}
		if recorded != end {
			return inconsistent(fmt.Sprintf("the completing content ends at %d, not at the upload's length, %d", end, recorded))
		}
	}
	mediaType, digest, name, err := s.h.Store.Meta(r.Context(), id)
	if err != nil {
		return err
	}
	h := w.Header()
	h.Del("Location")
	h.Del(fieldLimit)
	h.Del(fieldOffset)
	h.Set(fieldComplete, FormatBoolean(true))
	return s.h.Complete(w, r, File{ID: id, MediaType: mediaType, Digest: digest, Name: name, Length: end})
}

func (s *server) limits(expires time.Time) string {
	age := max(int64(time.Until(expires)/time.Second), 0)
	return fmt.Sprintf("max-append-size=%d, max-age=%d", s.limit, age)
}

func advance(state, chunk []byte) ([]byte, error) {
	h := sha256.New()
	if len(state) > 0 {
		if err := h.(encoding.BinaryUnmarshaler).UnmarshalBinary(state); err != nil {
			return nil, fmt.Errorf("upload: restore the hash state: %w", err)
		}
	}
	h.Write(chunk)
	return h.(encoding.BinaryMarshaler).MarshalBinary()
}

func checkDigest(r *http.Request, chunk []byte) (bool, error) {
	raw := field(r, fieldContentDigest)
	if raw == "" {
		return false, nil
	}
	want, ok := ParseDigest(raw)
	if !ok {
		return false, badRequest("Content-Digest carries no valid sha-256 member")
	}
	sum := sha256.Sum256(chunk)
	if hex.EncodeToString(sum[:]) != want {
		return false, badRequest("the content does not match its Content-Digest; nothing was appended")
	}
	return true, nil
}

func fileType(r *http.Request) (string, error) {
	ct := r.Header.Get("Content-Type")
	if ct == "" {
		return "", &web.UploadError{Header: "Content-Type", Reason: "the creation requires the file's Content-Type"}
	}
	mt, _, err := mime.ParseMediaType(ct)
	if err != nil || !strings.Contains(mt, "/") {
		return "", &web.UploadError{Header: "Content-Type", Reason: fmt.Sprintf("Content-Type %q is not a type/subtype media type", ct)}
	}
	return ct, nil
}

func fileName(r *http.Request) (string, error) {
	cd := r.Header.Get("Content-Disposition")
	if cd == "" {
		return "", nil
	}
	_, params, err := mime.ParseMediaType(cd)
	if err != nil {
		return "", badRequest(fmt.Sprintf("Content-Disposition %q: %v", cd, err))
	}
	return params["filename"], nil
}

func field(r *http.Request, name string) string {
	return strings.Join(r.Header.Values(name), ", ")
}

func integerField(r *http.Request, name string) (int64, bool) {
	raw := field(r, name)
	if raw == "" {
		return 0, false
	}
	n, ok := ParseInteger(raw)
	return n, ok && n >= 0
}

func booleanField(r *http.Request, name string) (bool, bool) {
	raw := field(r, name)
	if raw == "" {
		return false, false
	}
	return ParseBoolean(raw)
}

func isMaxBytes(err error) bool {
	_, ok := errors.AsType[*http.MaxBytesError](err)
	return ok
}

func timedOut(err error) bool {
	if errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	ne, ok := errors.AsType[net.Error](err)
	return ok && ne.Timeout()
}

func missing(err error) (web.Problem, bool) {
	if errors.Is(err, fs.ErrNotExist) {
		return web.Problem{Status: http.StatusNotFound}, true
	}
	return web.Problem{}, false
}

func unencoded() web.Problem {
	return web.Problem{Status: http.StatusUnsupportedMediaType, Detail: "an upload's content carries no Content-Encoding"}
}

func badRequest(detail string) web.Problem {
	return web.Problem{Status: http.StatusBadRequest, Detail: detail}
}

func inconsistent(detail string) web.Problem {
	return web.Problem{
		Type:   problemInconsistentLength,
		Title:  "inconsistent length values for upload",
		Status: http.StatusBadRequest,
		Detail: detail,
	}
}

func conflict(w http.ResponseWriter, expected, provided int64) web.Problem {
	w.Header().Set(fieldOffset, strconv.FormatInt(expected, 10))
	return web.Problem{
		Type:   problemMismatchingOffset,
		Title:  "offset from request does not match offset of resource",
		Status: http.StatusConflict,
		Detail: fmt.Sprintf("the upload's offset is %d, not %d", expected, provided),
		Extras: map[string]any{"expected-offset": expected, "provided-offset": provided},
	}
}
