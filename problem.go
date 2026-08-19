package web

import (
	"encoding/json"
	"maps"
	"net/http"
)

const (
	// ProblemMediaType is the RFC 9457 problem document media type.
	ProblemMediaType = "application/problem+json"
	// ProblemTypeBlank is the type of a problem with no semantics beyond its
	// status code.
	ProblemTypeBlank = "about:blank"
)

// Problem is an RFC 9457 problem document. Type identifies the problem's
// semantics and is the member a client branches on; this package mints no
// type URIs of its own, so a consumer supplies one here or through the extras
// of [WriteProblemWith].
type Problem struct {
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// Write sends the problem, applying defaults: a zero Status becomes 500, an
// empty Type becomes about:blank, and an empty Title takes the status phrase.
func (p Problem) Write(w http.ResponseWriter) error {
	p.applyDefaults()

	w.Header().Set("Content-Type", ProblemMediaType)
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(p)
}

func (p *Problem) applyDefaults() {
	if p.Status == 0 {
		p.Status = http.StatusInternalServerError
	}
	if p.Type == "" {
		p.Type = ProblemTypeBlank
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
}

// WriteProblem sends a problem for the request, with Instance set to the
// request path and [Problem.Write]'s defaults applied.
func WriteProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	title, detail string,
) error {
	return Problem{
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
	}.Write(w)
}

// WriteProblemWith is [WriteProblem] with extra members copied over the
// document. Extras may add or override any member except status, which always
// matches the status line.
func WriteProblemWith(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	title, detail string,
	extras map[string]any,
) error {
	p := Problem{
		Title:    title,
		Status:   status,
		Detail:   detail,
		Instance: r.URL.Path,
	}
	p.applyDefaults()

	body := map[string]any{
		"type":   p.Type,
		"status": p.Status,
	}
	if p.Title != "" {
		body["title"] = p.Title
	}
	if p.Detail != "" {
		body["detail"] = p.Detail
	}
	if p.Instance != "" {
		body["instance"] = p.Instance
	}
	maps.Copy(body, extras)
	body["status"] = p.Status

	w.Header().Set("Content-Type", ProblemMediaType)
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(body)
}
