package web

import (
	"encoding/json"
	"maps"
	"net/http"
	"strings"
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
// type URIs of its own, so a consumer supplies one here or through Extras.
// Extras carries extension members beyond the five standard ones, merged at
// the top level of the marshaled document: an extras key named "type",
// "title", "detail", or "instance" overrides the field of the same name,
// and a "status" key never desyncs from Status.
type Problem struct {
	Type     string
	Title    string
	Status   int
	Detail   string
	Instance string
	Extras   map[string]any
}

// problemMembers marshals Problem's five standard members under their json
// tags, without re-entering Problem's own MarshalJSON.
type problemMembers struct {
	Type     string `json:"type"`
	Title    string `json:"title,omitempty"`
	Status   int    `json:"status"`
	Detail   string `json:"detail,omitempty"`
	Instance string `json:"instance,omitempty"`
}

// MarshalJSON encodes the standard members, then merges Extras over the
// result at the top level: an extras key overrides the standard member of
// the same name, except status, which is always Status regardless of what
// Extras carries.
func (p Problem) MarshalJSON() ([]byte, error) {
	base, err := json.Marshal(problemMembers{
		Type: p.Type, Title: p.Title, Status: p.Status,
		Detail: p.Detail, Instance: p.Instance,
	})
	if err != nil || len(p.Extras) == 0 {
		return base, err
	}

	doc := make(map[string]any, len(p.Extras)+5)
	if err := json.Unmarshal(base, &doc); err != nil {
		return nil, err
	}
	maps.Copy(doc, p.Extras)
	doc["status"] = p.Status
	return json.Marshal(doc)
}

// UnmarshalJSON reads the five standard members into their fields and every
// other member into Extras, so a document written with extension members
// round-trips them.
func (p *Problem) UnmarshalJSON(data []byte) error {
	var m problemMembers
	if err := json.Unmarshal(data, &m); err != nil {
		return err
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		return err
	}
	// encoding/json matches problemMembers' fields case-insensitively, so
	// the strip has to as well - otherwise a non-conforming document (a
	// capitalized "Status") both populates the typed field above and
	// leaks the same member into Extras.
	for k := range doc {
		for _, standard := range [...]string{"type", "title", "status", "detail", "instance"} {
			if strings.EqualFold(k, standard) {
				delete(doc, k)
				break
			}
		}
	}

	*p = Problem{
		Type: m.Type, Title: m.Title, Status: m.Status,
		Detail: m.Detail, Instance: m.Instance,
	}
	if len(doc) > 0 {
		p.Extras = doc
	}
	return nil
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

// Write sends the problem, applying defaults: a zero Status becomes 500, an
// empty Type becomes about:blank, and an empty Title takes the status
// phrase.
func (p Problem) Write(w http.ResponseWriter) error {
	p.applyDefaults()

	w.Header().Set("Content-Type", ProblemMediaType)
	w.WriteHeader(p.Status)
	return json.NewEncoder(w).Encode(p)
}

// WriteFor is [Problem.Write], with Instance set to the request path first
// when p.Instance is empty, and the request's correlation id surfaced as the
// "request_id" extension member when the request's context carries a
// non-empty one (see [WithRequestID]). The id is the SDK's own assertion of
// which request the document answers, so it overrides a "request_id" key the
// caller put in Extras. Extras is copied before the id is merged in; the
// caller's map is never written.
func (p Problem) WriteFor(w http.ResponseWriter, r *http.Request) error {
	if p.Instance == "" {
		p.Instance = r.URL.Path
	}
	if id, ok := RequestIDFrom(r.Context()); ok && id != "" {
		extras := make(map[string]any, len(p.Extras)+1)
		maps.Copy(extras, p.Extras)
		extras[requestIDMember] = id
		p.Extras = extras
	}
	return p.Write(w)
}

// WriteProblem sends a problem for the request, with Instance set to the
// request path and [Problem.Write]'s defaults applied.
func WriteProblem(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	title, detail string,
) error {
	return Problem{Title: title, Status: status, Detail: detail}.WriteFor(w, r)
}
