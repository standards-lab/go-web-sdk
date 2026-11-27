package upload

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/http/httptrace"
	"net/textproto"
	"net/url"
	"strconv"
	"sync"
	"time"
)

// Client uploads files to a draft-16 upload server, such as a [Handler],
// over HTTP/1.1 or later. HTTP and Attempts are required; [Client.Create]
// panics, naming the field, when one is missing.
type Client struct {
	// HTTP sends every request. Its Timeout, if any, bounds each request,
	// an append of one chunk among them.
	HTTP *http.Client

	// Attempts is how many requests in a row may fail to advance an upload
	// before the client gives up: a request that gets no response, a 5xx
	// or 429, or a 409 counts, and an append the server commits starts the
	// count again. It bounds a creation that learns no upload resource the
	// same way. It is at least 1.
	Attempts int

	// Delay is how long the client waits before each retry. Zero retries at
	// once.
	Delay time.Duration

	// ChunkSize, when positive, is the most content one append carries,
	// below the max-append-size the server announces. Zero leaves the
	// chunk at max-append-size, or at the rest of the file when the server
	// announces none. The client holds one chunk in memory at a time.
	ChunkSize int64
}

// Resource is one upload resource a [Client] created: the file it sends,
// where the server keeps it, and how much of the file the server holds. A
// Resource sends one request at a time and is not safe for concurrent use.
type Resource struct {
	c        Client
	location string
	content  io.ReadSeeker
	length   int64
	offset   int64
	known    bool
	limit    int64
}

// Create creates the upload resource for content at target, the URL the
// server's creating POST is served at, and returns it with nothing of the
// file sent yet. mediaType is the file's Content-Type, and name, when not
// empty, its Content-Disposition file name.
//
// Create reads content from its start to its end for the SHA-256 the
// creation's Repr-Digest carries and the length its Upload-Length carries;
// [Resource.Send] then seeks content to each offset it sends from, so
// content holds the same bytes until the upload completes.
//
// The creation carries no content and an Upload-Draft-Interop-Version of
// [InteropVersion], so the server names the resource in a 104 (Upload
// Resumption Supported) as well as in its 201. When the 201 is lost after
// the 104, the Resource reads its offset before it sends. When neither
// arrives, Create repeats the creation, within c.Attempts. A final
// response other than a 2xx, 429, or 5xx is returned as an error.
func (c Client) Create(ctx context.Context, target, mediaType, name string, content io.ReadSeeker) (*Resource, error) {
	switch {
	case c.HTTP == nil:
		panic("upload: Client.Create requires an HTTP client")
	case c.Attempts < 1:
		panic("upload: Client.Create requires a positive Attempts")
	}
	digest, length, err := whole(content)
	if err != nil {
		return nil, err
	}
	reprDigest, err := FormatDigest(digest)
	if err != nil {
		return nil, err
	}
	base, err := url.Parse(target)
	if err != nil {
		return nil, fmt.Errorf("upload: target %q: %w", target, err)
	}
	header := http.Header{}
	header.Set(fieldComplete, FormatBoolean(false))
	header.Set(fieldLength, strconv.FormatInt(length, 10))
	header.Set(fieldReprDigest, reprDigest)
	if mediaType != "" {
		header.Set("Content-Type", mediaType)
	}
	if name != "" {
		header.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	r := &Resource{c: c, content: content, length: length}
	for failures := 0; ; {
		o, err := r.create(ctx, base, header)
		switch o {
		case located:
			return r, nil
		case failed:
			if failures++; failures >= c.Attempts {
				return nil, fmt.Errorf("upload: the creation gave up after %d attempts: %w", failures, err)
			}
			if err := r.pause(ctx); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
	}
}

func (r *Resource) create(ctx context.Context, base *url.URL, header http.Header) (outcome, error) {
	var mu sync.Mutex
	var interim *url.URL
	var interimErr error
	trace := &httptrace.ClientTrace{
		Got1xxResponse: func(code int, h textproto.MIMEHeader) error {
			if code != statusUploadResumptionSupported || h.Get("Location") == "" {
				return nil
			}
			mu.Lock()
			defer mu.Unlock()
			interim, interimErr = base.Parse(h.Get("Location"))
			r.limits(http.Header(h))
			return nil
		},
	}
	req, err := http.NewRequestWithContext(httptrace.WithClientTrace(ctx, trace), http.MethodPost, base.String(), http.NoBody)
	if err != nil {
		return fatal, fmt.Errorf("upload: %w", err)
	}
	req.Header = header.Clone()
	interop(req)
	res, err := r.c.HTTP.Do(req)
	mu.Lock()
	defer mu.Unlock()
	if interimErr != nil {
		return fatal, fmt.Errorf("upload: the 104's Location: %w", interimErr)
	}
	if interim != nil {
		r.location = interim.String()
	}
	if err != nil {
		if ctx.Err() != nil {
			return fatal, ctx.Err()
		}
		if interim != nil {
			return located, nil
		}
		return failed, fmt.Errorf("upload: the creation at %s: %w", base, err)
	}
	_ = res.Body.Close()
	switch {
	case success(res.StatusCode):
		final, err := res.Request.URL.Parse(res.Header.Get("Location"))
		if err != nil || res.Header.Get("Location") == "" {
			return fatal, fmt.Errorf("upload: the creation's %s named no valid Location", res.Status)
		}
		if interim != nil && final.String() != interim.String() {
			return fatal, fmt.Errorf("upload: the creation's 104 named %s and its %s named %s", interim, res.Status, final)
		}
		r.location = final.String()
		r.limits(res.Header)
		r.offset, r.known = offsetField(res.Header)
		return located, nil
	case retryable(res.StatusCode) && interim != nil:
		return located, nil
	case retryable(res.StatusCode):
		return failed, statusError("the creation", base.String(), res)
	default:
		return fatal, statusError("the creation", base.String(), res)
	}
}

// Location returns the upload resource's absolute URL.
func (r *Resource) Location() string {
	return r.location
}

// Send appends the rest of the file to the upload resource and returns the
// final response of the request that completes it, which the server's
// target resource writes: a [Handler]'s Complete. That response is
// returned whatever its status, for a 4xx or 5xx that marks the upload
// complete reports the file failed in processing, which resuming would not
// change.
//
// Send appends from the server's offset in chunks no larger than the
// max-append-size the server last announced and the client's ChunkSize,
// each with its Content-Digest, the last with Upload-Complete: ?1. Send
// reads each of the draft's fields from all of its field lines, combined
// as RFC 9651 requires, and ignores an Upload-Limit whole when a member
// the draft defines is invalid, as the draft requires. After
// a request that gets no response, a 5xx, or a 429, Send reads the
// server's offset with HEAD, never while one of its own requests to the
// upload is in flight, and resumes from it; a 409 carries the offset to
// resume from. When the Client's Attempts run out it returns an error and
// leaves the upload in place, for a later Send to resume or for
// [Resource.Cancel] to delete. An upload the server no longer has returns
// an error that matches [io/fs.ErrNotExist] under errors.Is.
func (r *Resource) Send(ctx context.Context) (*http.Response, error) {
	failures := 0
	for {
		var res *http.Response
		var o outcome
		var err error
		if r.known {
			res, o, err = r.append(ctx)
		} else {
			o, err = r.head(ctx)
		}
		switch o {
		case finished:
			return res, nil
		case advanced:
			failures = 0
		case located:
		case failed:
			if failures++; failures >= r.c.Attempts {
				return nil, fmt.Errorf("upload: %s gave up after %d attempts: %w", r.location, failures, err)
			}
			if err := r.pause(ctx); err != nil {
				return nil, err
			}
		default:
			return nil, err
		}
	}
}

// Cancel deletes the upload resource with a DELETE, so the server releases
// what it holds of the upload. An upload the server no longer has returns
// an error that matches [io/fs.ErrNotExist] under errors.Is.
func (r *Resource) Cancel(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.location, http.NoBody)
	if err != nil {
		return fmt.Errorf("upload: %w", err)
	}
	interop(req)
	res, err := r.c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("upload: cancel %s: %w", r.location, err)
	}
	_ = res.Body.Close()
	if !success(res.StatusCode) {
		return statusError("the cancellation", r.location, res)
	}
	return nil
}

type outcome int

const (
	fatal outcome = iota
	finished
	advanced
	located
	failed
)

func (r *Resource) append(ctx context.Context) (*http.Response, outcome, error) {
	if r.offset > r.length {
		return nil, fatal, fmt.Errorf("upload: %s holds %d bytes of a %d-byte file", r.location, r.offset, r.length)
	}
	n := r.length - r.offset
	if r.limit > 0 {
		n = min(n, r.limit)
	}
	if r.c.ChunkSize > 0 {
		n = min(n, r.c.ChunkSize)
	}
	chunk := make([]byte, n)
	if _, err := r.content.Seek(r.offset, io.SeekStart); err != nil {
		return nil, fatal, fmt.Errorf("upload: seek the file to %d: %w", r.offset, err)
	}
	if _, err := io.ReadFull(r.content, chunk); err != nil {
		return nil, fatal, fmt.Errorf("upload: read the file at %d: %w", r.offset, err)
	}
	sum := sha256.Sum256(chunk)
	contentDigest, err := FormatDigest(hex.EncodeToString(sum[:]))
	if err != nil {
		return nil, fatal, err
	}
	complete := r.offset+n == r.length
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch, r.location, bytes.NewReader(chunk))
	if err != nil {
		return nil, fatal, fmt.Errorf("upload: %w", err)
	}
	req.Header.Set("Content-Type", partialUpload)
	req.Header.Set(fieldOffset, strconv.FormatInt(r.offset, 10))
	req.Header.Set(fieldComplete, FormatBoolean(complete))
	req.Header.Set(fieldContentDigest, contentDigest)
	interop(req)
	res, err := r.c.HTTP.Do(req)
	if err != nil {
		o, err := r.lost(ctx, "the append", err)
		return nil, o, err
	}
	if completes(res, complete) {
		return res, finished, nil
	}
	_ = res.Body.Close()
	r.limits(res.Header)
	err = statusError("the append", r.location, res)
	switch {
	case success(res.StatusCode):
		if offset, ok := offsetField(res.Header); ok {
			r.offset = offset
		} else {
			r.offset += n
		}
		return nil, advanced, nil
	case res.StatusCode == http.StatusConflict:
		r.offset, r.known = offsetField(res.Header)
		return nil, failed, err
	case res.StatusCode == http.StatusRequestEntityTooLarge && r.limit > 0 && r.limit < n:
		return nil, failed, err
	case retryable(res.StatusCode):
		r.known = false
		return nil, failed, err
	default:
		return nil, fatal, err
	}
}

func (r *Resource) head(ctx context.Context) (outcome, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, r.location, http.NoBody)
	if err != nil {
		return fatal, fmt.Errorf("upload: %w", err)
	}
	interop(req)
	res, err := r.c.HTTP.Do(req)
	if err != nil {
		return r.lost(ctx, "the offset retrieval", err)
	}
	_ = res.Body.Close()
	err = statusError("the offset retrieval", r.location, res)
	offset, ok := offsetField(res.Header)
	switch {
	case success(res.StatusCode) && ok:
		r.limits(res.Header)
		r.offset, r.known = offset, true
		return located, nil
	case errors.Is(err, fs.ErrNotExist):
		return fatal, err
	case success(res.StatusCode) || res.StatusCode >= 400:
		return failed, err
	default:
		return fatal, err
	}
}

func (r *Resource) lost(ctx context.Context, request string, err error) (outcome, error) {
	if ctx.Err() != nil {
		return fatal, ctx.Err()
	}
	r.known = false
	return failed, fmt.Errorf("upload: %s to %s: %w", request, r.location, err)
}

func (r *Resource) pause(ctx context.Context) error {
	if r.c.Delay <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(r.c.Delay)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func interop(req *http.Request) {
	req.Header.Set(fieldInterop, strconv.Itoa(InteropVersion))
}

func (r *Resource) limits(h http.Header) {
	d, ok := ParseDictionary(field(h, fieldLimit))
	if !ok {
		return
	}
	values := map[string]int64{}
	for _, m := range d {
		floor, known := limitFloors[m.Key]
		if !known {
			continue
		}
		n, ok := m.Value.(int64)
		if !ok || n < floor {
			return
		}
		values[m.Key] = n
	}
	if !ordered(values, "min-size", "max-size") ||
		!ordered(values, "min-append-size", "max-append-size") ||
		!ordered(values, "min-append-size", "max-size") {
		return
	}
	if n, ok := values["max-append-size"]; ok {
		r.limit = n
	}
}

var limitFloors = map[string]int64{
	"max-size":        1,
	"min-size":        0,
	"max-append-size": 1,
	"min-append-size": 0,
	"max-age":         0,
}

func ordered(values map[string]int64, low, high string) bool {
	l, lok := values[low]
	h, hok := values[high]
	return !lok || !hok || l <= h
}

func whole(content io.ReadSeeker) (string, int64, error) {
	if _, err := content.Seek(0, io.SeekStart); err != nil {
		return "", 0, fmt.Errorf("upload: seek the file to its start: %w", err)
	}
	h := sha256.New()
	length, err := io.Copy(h, content)
	if err != nil {
		return "", 0, fmt.Errorf("upload: read the file: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), length, nil
}

func completes(res *http.Response, sent bool) bool {
	raw := field(res.Header, fieldComplete)
	if raw == "" {
		return sent && success(res.StatusCode)
	}
	complete, ok := ParseBoolean(raw)
	return ok && complete
}

func offsetField(h http.Header) (int64, bool) {
	n, ok := ParseInteger(field(h, fieldOffset))
	return n, ok && n >= 0
}

func success(status int) bool {
	return status >= 200 && status < 300
}

func retryable(status int) bool {
	return status == http.StatusTooManyRequests || status >= 500
}

func statusError(request, location string, res *http.Response) error {
	err := fmt.Errorf("upload: %s to %s answered %s", request, location, res.Status)
	if res.StatusCode == http.StatusNotFound || res.StatusCode == http.StatusGone {
		return fmt.Errorf("%w: %w", err, fs.ErrNotExist)
	}
	return err
}
