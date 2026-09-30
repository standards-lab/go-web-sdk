package web

import (
	"encoding/json"
	"fmt"
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

// Problem is an RFC 9457 problem document. Extras carries extension members,
// merged at the top level of the marshaled document, where a key may
// override any standard member except status, which always matches Status.
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

// Error renders the problem as a status line, "404 Not Found: detail", so a
// Problem serves directly as an error.
func (p Problem) Error() string {
	title := p.Title
	if title == "" {
		title = http.StatusText(p.Status)
	}
	line := fmt.Sprintf("%d %s", p.Status, title)
	if p.Detail != "" {
		line += ": " + p.Detail
	}
	return line
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

// WriteFor is [Problem.Write] with Instance defaulted to the request path
// and the request's correlation id ([WithRequestID]) as the "request_id"
// member, overriding one in Extras without writing the caller's map.
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
