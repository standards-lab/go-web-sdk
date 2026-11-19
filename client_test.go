package web_test

import (
	"encoding/json"
	"errors"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"regexp"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/standards-lab/go-web-sdk"
	"github.com/standards-lab/go-web-sdk/internal/handlertest"
)

// viteBuild is a Vite build as NewClient reads it: a manifest whose entry
// imports a shared chunk statically and a route dynamically, the entry and
// the shared chunk each pulling in CSS, and a file copied from a public
// directory, which Vite does not hash. Two more entries stand for modules
// a shell loads render-blocking: one with CSS of its own, importing the
// entry's shared chunk and a chunk of their own, and one importing that
// chunk alone.
func viteBuild() fstest.MapFS {
	return fstest.MapFS{
		web.ManifestPath: {Data: []byte(`{
			"src/main.ts": {
				"file": "assets/main-abc.js", "src": "src/main.ts", "isEntry": true,
				"css": ["assets/main-abc.css"],
				"imports": ["_shared-def.js"],
				"dynamicImports": ["src/about.ts"]
			},
			"_shared-def.js": {"file": "assets/shared-def.js", "css": ["assets/shared-def.css"]},
			"src/about.ts": {"file": "assets/about-ghi.js", "isDynamicEntry": true, "css": ["assets/about-ghi.css"]},
			"src/register.ts": {
				"file": "assets/register-jkl.js", "src": "src/register.ts", "isEntry": true,
				"css": ["assets/register-jkl.css"],
				"imports": ["_shared-def.js", "_registry-mno.js"]
			},
			"src/preferences.ts": {
				"file": "assets/preferences-pqr.js", "src": "src/preferences.ts", "isEntry": true,
				"imports": ["_registry-mno.js"]
			},
			"_registry-mno.js": {"file": "assets/registry-mno.js"}
		}`)},
		"assets/main-abc.js":   {Data: []byte("console.log('main')")},
		"assets/main-abc.css":  {Data: []byte("body{}")},
		"assets/shared-def.js": {Data: []byte("export {}")},
		"assets/about-ghi.js":  {Data: []byte("export {}")},
		"favicon.svg":          {Data: []byte("<svg/>")},
		".gitkeep":             {Data: nil},
	}
}

// clientEntry is the build's entry key.
const clientEntry = "src/main.ts"

// clientConfig stands in for the configuration an application writes into
// the shell.
type clientConfig struct {
	APIBase string `json:"apiBase"`
	Token   string `json:"token"`
}

func clientOptions(base string) web.ClientOptions {
	return web.ClientOptions{
		Base:   base,
		Title:  "Pipeline <emulator>",
		Config: clientConfig{APIBase: "/api", Token: "t"},
	}
}

// newClient builds the handler over viteBuild() with opts, failing the test
// on an error.
func newClient(t *testing.T, opts web.ClientOptions) http.Handler {
	t.Helper()
	h, err := web.NewClient(viteBuild(), clientEntry, opts)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return h
}

// shellOf is the body the handler answers at its root.
func shellOf(t *testing.T, opts web.ClientOptions) string {
	t.Helper()
	return handlertest.Get(newClient(t, opts), "/").Body.String()
}

// unreadableFS is a build one of whose files cannot be opened, as a read
// failure on a real file system would leave it. It implements Open alone,
// so every read of the build reaches it.
type unreadableFS struct {
	build fstest.MapFS
	name  string
}

func (u unreadableFS) Open(name string) (fs.File, error) {
	if name == u.name {
		return nil, &fs.PathError{Op: "open", Path: name, Err: errors.New("input/output error")}
	}
	return u.build.Open(name)
}

func TestNewClient_FailsNamingTheProblem(t *testing.T) {
	noManifest := viteBuild()
	delete(noManifest, web.ManifestPath)
	badManifest := viteBuild()
	badManifest[web.ManifestPath] = &fstest.MapFile{Data: []byte("{")}
	unreadable := unreadableFS{viteBuild(), "favicon.svg"}

	cases := map[string]struct {
		dist  fs.FS
		entry string
		opts  web.ClientOptions
		want  string
	}{
		"missing manifest": {noManifest, clientEntry, web.ClientOptions{}, web.ManifestPath},
		"corrupt manifest": {badManifest, clientEntry, web.ClientOptions{}, "decode the build's manifest"},
		"missing entry":    {viteBuild(), "src/index.ts", web.ClientOptions{}, `no entry "src/index.ts"`},
		"missing blocking": {viteBuild(), clientEntry, web.ClientOptions{RenderBlocking: []string{"src/register.ts", "src/theme.ts"}}, `no entry "src/theme.ts"`},
		"unencodable":      {viteBuild(), clientEntry, web.ClientOptions{Config: func() {}}, "encode the config"},
		"unreadable file":  {unreadable, clientEntry, web.ClientOptions{}, "open favicon.svg"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := web.NewClient(tc.dist, tc.entry, tc.opts)
			if err == nil {
				t.Fatalf("NewClient succeeded; want an error naming %q", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %v, want it to name %q", err, tc.want)
			}
		})
	}
}

// A malformed base is a wiring mistake, so it panics, naming the base and
// the form it must take.
func TestNewClient_MalformedBasePanics(t *testing.T) {
	for _, base := range []string{"app", "//", "/app//", "/a/../app", "/app/.", "/app?x=1", "/app#top"} {
		t.Run(base, func(t *testing.T) {
			defer func() {
				msg, _ := recover().(string)
				if !strings.Contains(msg, "clean path beginning with '/'") || !strings.HasSuffix(msg, base) {
					t.Errorf("panic = %q, want one naming the fix and %q", msg, base)
				}
			}()
			_, _ = web.NewClient(viteBuild(), clientEntry, web.ClientOptions{Base: base})
		})
	}
}

// The shell writes the base with exactly one trailing slash, empty
// selecting "/", the title escaped as text, the config as JSON in its
// element, and the entry as a module script.
func TestNewClient_Shell(t *testing.T) {
	for base, href := range map[string]string{"": "/", "/": "/", "/app": "/app/", "/app/": "/app/", "/a/b": "/a/b/"} {
		t.Run(base, func(t *testing.T) {
			body := shellOf(t, clientOptions(base))
			for _, want := range []string{
				`<base href="` + href + `">`,
				`<title>Pipeline &lt;emulator&gt;</title>`,
				`<script type="application/json" id="` + web.ConfigElementID + `">{"apiBase":"/api","token":"t"}</script>`,
				`<script type="module" src="assets/main-abc.js"></script>`,
			} {
				if !strings.Contains(body, want) {
					t.Errorf("shell lacks %s:\n%s", want, body)
				}
			}
		})
	}
}

// shellLinks matches the shell's stylesheet links, modulepreload links, and
// module scripts, in document order: the kind, any blocking attribute, and
// the URL.
var shellLinks = regexp.MustCompile(`<link rel="(stylesheet|modulepreload)" href="([^"]*)">|<script type="module"( blocking="render")? src="([^"]*)"></script>`)

// shellOrder lists body's links and module scripts in document order, each
// as "stylesheet URL", "modulepreload URL", "blocking URL", or "module URL".
func shellOrder(body string) []string {
	var order []string
	for _, m := range shellLinks.FindAllStringSubmatch(body, -1) {
		switch {
		case m[1] != "":
			order = append(order, m[1]+" "+m[2])
		case m[3] != "":
			order = append(order, "blocking "+m[4])
		default:
			order = append(order, "module "+m[4])
		}
	}
	return order
}

// With no RenderBlocking the shell loads the entry alone: the CSS of the
// entry and of the chunk it imports statically, a modulepreload for that
// chunk, then the entry. Nothing from the dynamic import appears.
func TestNewClient_ShellLoadsTheEntry(t *testing.T) {
	body := shellOf(t, web.ClientOptions{})
	got := shellOrder(body)
	want := []string{
		"stylesheet assets/main-abc.css",
		"stylesheet assets/shared-def.css",
		"modulepreload assets/shared-def.js",
		"module assets/main-abc.js",
	}
	if !slices.Equal(got, want) {
		t.Errorf("shell loads %q, want %q", got, want)
	}
	if strings.Contains(body, "about-ghi") {
		t.Errorf("shell names the dynamic import's files:\n%s", body)
	}
}

// The render-blocking modules load ahead of the entry, in the order
// RenderBlocking lists them (here against the manifest's own order), each
// a module script with blocking="render"; the entry follows them,
// non-blocking. Their CSS comes first, then the entry's, and every chunk
// any of them imports statically is preloaded once, never a chunk a script
// loads itself.
func TestNewClient_ShellLoadsRenderBlockingModules(t *testing.T) {
	opts := clientOptions("/")
	opts.RenderBlocking = []string{"src/preferences.ts", "src/register.ts"}
	got := shellOrder(shellOf(t, opts))
	want := []string{
		"stylesheet assets/register-jkl.css",
		"stylesheet assets/shared-def.css",
		"stylesheet assets/main-abc.css",
		"modulepreload assets/registry-mno.js",
		"modulepreload assets/shared-def.js",
		"blocking assets/preferences-pqr.js",
		"blocking assets/register-jkl.js",
		"module assets/main-abc.js",
	}
	if !slices.Equal(got, want) {
		t.Errorf("shell loads\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

// configElement matches the config element's content.
var configElement = regexp.MustCompile(`(?s)<script type="application/json" id="app-config">(.*?)</script>`)

// configOf returns the content of body's config element.
func configOf(t *testing.T, body string) string {
	t.Helper()
	m := configElement.FindStringSubmatch(body)
	if m == nil {
		t.Fatalf("no config element in the shell:\n%s", body)
	}
	return m[1]
}

// A config value carrying a closing script tag, a comment opener, and an
// ampersand cannot end the element early: the first </script> after the
// element's opening tag is its own, and its content decodes back to the
// value.
func TestNewClient_ShellEscapesConfig(t *testing.T) {
	hostile := "</script><script>alert(1)</script><!-- & \u2028"
	opts := clientOptions("/")
	opts.Config = clientConfig{APIBase: hostile}
	body := shellOf(t, opts)

	content := configOf(t, body)
	var got clientConfig
	if err := json.Unmarshal([]byte(content), &got); err != nil {
		t.Fatalf("config element %q: %v (the value broke out of the element)", content, err)
	}
	if got.APIBase != hostile {
		t.Errorf("config decodes to %q, want %q", got.APIBase, hostile)
	}
	if n := strings.Count(body, "<script"); n != 2 {
		t.Errorf("shell holds %d script elements, want 2:\n%s", n, body)
	}
}

// A nil config writes null.
func TestNewClient_ShellWritesNilConfigAsNull(t *testing.T) {
	if got := configOf(t, shellOf(t, web.ClientOptions{})); got != "null" {
		t.Errorf("config element = %q, want null", got)
	}
}

// Every path that is neither an asset nor a missing asset is a client
// route, deep links and trailing slashes included, and answers the shell
// as HTML revalidated on every use; so does a path that climbs with "..",
// cleaned inside the build.
func TestNewClient_ClientRoutesAnswerTheShell(t *testing.T) {
	h := newClient(t, clientOptions("/app"))
	want := handlertest.Get(h, "/").Body.String()
	for _, path := range []string{"/", "/about", "/runs/42/stages", "/about/", "/../../etc/passwd/x"} {
		t.Run(path, func(t *testing.T) {
			rec := handlertest.Get(h, path)
			if rec.Code != http.StatusOK || rec.Body.String() != want {
				t.Fatalf("GET %s = %d %q, want the shell", path, rec.Code, rec.Body.String())
			}
			if ct := rec.Header().Get("Content-Type"); ct != "text/html; charset=utf-8" {
				t.Errorf("Content-Type = %q, want text/html; charset=utf-8", ct)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != "no-cache" {
				t.Errorf("Cache-Control = %q, want no-cache", cc)
			}
		})
	}
}

// expectClientProblem fails the test unless rec holds a problem document
// with status and its title, and the path the browser asked for as
// instance: the client's answer, never the shell.
func expectClientProblem(t *testing.T, rec *httptest.ResponseRecorder, status int, instance string) {
	t.Helper()
	if rec.Code != status {
		t.Errorf("status = %d, want %d", rec.Code, status)
	}
	if ct := rec.Header().Get("Content-Type"); ct != web.ProblemMediaType {
		t.Errorf("Content-Type = %q, want %q", ct, web.ProblemMediaType)
	}
	var p web.Problem
	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("body is not a problem document: %v: %s", err, rec.Body.String())
	}
	if p.Status != status || p.Title != http.StatusText(status) || p.Instance != instance {
		t.Errorf("problem = %+v, want status %d, title %q, instance %q", p, status, http.StatusText(status), instance)
	}
}

// A build file answers its bytes, typed by its extension. A hashed file,
// under the manifest's directories, is cached for a year as immutable, with
// no ETag; a build file Vite does not hash is revalidated on every use
// against its ETag.
func TestNewClient_Assets(t *testing.T) {
	h := newClient(t, clientOptions("/app"))
	cases := []struct {
		path, body, ctype, cache string
		etag                     bool
	}{
		{"/assets/main-abc.js", "console.log('main')", "text/javascript; charset=utf-8", "public, max-age=31536000, immutable", false},
		{"/assets/main-abc.css", "body{}", "text/css; charset=utf-8", "public, max-age=31536000, immutable", false},
		{"/favicon.svg", "<svg/>", "image/svg+xml", "no-cache", true},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			rec := handlertest.Get(h, tc.path)
			if rec.Code != http.StatusOK || rec.Body.String() != tc.body {
				t.Fatalf("GET %s = %d %q, want 200 %q", tc.path, rec.Code, rec.Body.String(), tc.body)
			}
			if ct := rec.Header().Get("Content-Type"); ct != tc.ctype {
				t.Errorf("Content-Type = %q, want %q", ct, tc.ctype)
			}
			if cc := rec.Header().Get("Cache-Control"); cc != tc.cache {
				t.Errorf("Cache-Control = %q, want %q", cc, tc.cache)
			}
			if got := rec.Header().Get("ETag") != ""; got != tc.etag {
				t.Errorf("ETag = %q, want one: %t", rec.Header().Get("ETag"), tc.etag)
			}
		})
	}
}

// strongETag matches a strong entity tag: quoted, with no W/ prefix.
var strongETag = regexp.MustCompile(`^"[^"]+"$`)

// The shell and a no-cache build file each carry a strong ETag, the same on
// every request and distinct from each other; an If-None-Match naming it
// answers 304 with no body, and one naming another tag answers the bytes.
func TestNewClient_RevalidatedFilesAnswerConditionalRequests(t *testing.T) {
	h := newClient(t, clientOptions("/"))
	seen := map[string]string{}
	for _, path := range []string{"/", "/runs/42", "/favicon.svg"} {
		t.Run(path, func(t *testing.T) {
			first := handlertest.Get(h, path)
			tag := first.Header().Get("ETag")
			if !strongETag.MatchString(tag) {
				t.Fatalf("ETag = %q, want a strong entity tag", tag)
			}
			if again := handlertest.Get(h, path).Header().Get("ETag"); again != tag {
				t.Errorf("ETag changed between requests: %q, then %q", tag, again)
			}
			seen[path] = tag

			rec := conditionalGet(h, path, tag)
			if rec.Code != http.StatusNotModified || rec.Body.Len() != 0 {
				t.Errorf("If-None-Match %s = %d with %d body bytes, want 304 and none", tag, rec.Code, rec.Body.Len())
			}
			if got := rec.Header().Get("ETag"); got != tag {
				t.Errorf("304 ETag = %q, want %q", got, tag)
			}
			rec = conditionalGet(h, path, `"stale"`)
			if rec.Code != http.StatusOK || rec.Body.String() != first.Body.String() {
				t.Errorf("If-None-Match \"stale\" = %d %q, want 200 and the bytes", rec.Code, rec.Body.String())
			}
		})
	}
	if seen["/"] != seen["/runs/42"] {
		t.Errorf("the shell's ETag differs by route: %q and %q", seen["/"], seen["/runs/42"])
	}
	if seen["/"] == seen["/favicon.svg"] {
		t.Errorf("the shell and favicon.svg share ETag %q", seen["/"])
	}
}

// conditionalGet serves a GET of path through h with If-None-Match tag.
func conditionalGet(h http.Handler, path, tag string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("If-None-Match", tag)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// An ETag follows content: a build whose file or shell changes gives each a
// new one, so a browser holding the old tag gets the new bytes.
func TestNewClient_ETagFollowsContent(t *testing.T) {
	changed := viteBuild()
	changed["favicon.svg"] = &fstest.MapFile{Data: []byte("<svg></svg>")}
	h2, err := web.NewClient(changed, clientEntry, clientOptions("/"))
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	h1 := newClient(t, clientOptions("/"))
	retitled := clientOptions("/")
	retitled.Title = "Pipeline"
	h3 := newClient(t, retitled)

	for _, tc := range []struct {
		path   string
		before http.Handler
		after  http.Handler
	}{
		{"/favicon.svg", h1, h2},
		{"/", h1, h3},
	} {
		old := handlertest.Get(tc.before, tc.path).Header().Get("ETag")
		if rec := conditionalGet(tc.after, tc.path, old); rec.Code != http.StatusOK || rec.Header().Get("ETag") == old {
			t.Errorf("GET %s with the old ETag %s = %d, ETag %q; want 200 and a new tag", tc.path, old, rec.Code, rec.Header().Get("ETag"))
		}
	}
}

// A missing asset is a 404 problem, never the shell: a path under the
// hashed directory, a path with an extension, the manifest and every other
// dotfile, which exist but are never served. The instance names the path
// the browser asked for, the base included.
func TestNewClient_MissingAssetIsNotFound(t *testing.T) {
	for _, base := range []string{"/", "/app"} {
		h := newClient(t, clientOptions(base))
		for _, path := range []string{
			"/assets/main-old.js",
			"/assets/nested/route",
			"/robots.txt",
			"/runs/report.pdf",
			"/.vite/manifest.json",
			"/.vite/",
			"/.gitkeep",
			"/.env",
		} {
			t.Run(base+path, func(t *testing.T) {
				expectClientProblem(t, handlertest.Get(h, path), http.StatusNotFound, strings.TrimSuffix(base, "/")+path)
			})
		}
	}
}

// HEAD answers the GET's status, Content-Type, Content-Length,
// Cache-Control and ETag with no body, for the shell and an asset alike.
func TestNewClient_Head(t *testing.T) {
	h := newClient(t, clientOptions("/app"))
	for _, path := range []string{"/about", "/assets/main-abc.js", "/favicon.svg"} {
		t.Run(path, func(t *testing.T) {
			get, head := handlertest.Get(h, path), handlertest.Serve(h, http.MethodHead, path)
			if head.Code != http.StatusOK || head.Body.Len() != 0 {
				t.Errorf("HEAD = %d with %d body bytes, want 200 and none", head.Code, head.Body.Len())
			}
			for _, name := range []string{"Content-Type", "Content-Length", "Cache-Control", "ETag"} {
				g, hd := get.Header().Get(name), head.Header().Get(name)
				if g != hd || g == "" && name != "ETag" {
					t.Errorf("HEAD %s = %q, want the GET's %q", name, hd, g)
				}
			}
		})
	}
}

// Any method but GET and HEAD is a 405 problem naming the two it takes,
// on an asset and a client route alike.
func TestNewClient_OtherMethodsAreNotAllowed(t *testing.T) {
	h := newClient(t, clientOptions("/app"))
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		for _, path := range []string{"/assets/main-abc.js", "/runs/42"} {
			t.Run(method+" "+path, func(t *testing.T) {
				rec := handlertest.Serve(h, method, path)
				expectClientProblem(t, rec, http.StatusMethodNotAllowed, "/app"+path)
				if allow := rec.Header().Get("Allow"); allow != "GET, HEAD" {
					t.Errorf("Allow = %q, want GET, HEAD", allow)
				}
			})
		}
	}
}

// Set as the router's not-found handler at the root, the client answers
// only what no module and no native route claims: the module's routes, its
// misses, the native routes, and the probes answer as before, and every
// other path is the client's.
func TestNewClient_AsTheRoutersNotFound(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/things"))
	r.Handle("GET /status", ok())
	web.RegisterHealth(r, idleCoordinator(t), web.Problem{})
	r.SetNotFound(newClient(t, clientOptions("/")))
	shell := shellOf(t, clientOptions("/"))

	for _, path := range []string{"/api/things", "/status", web.HealthPath} {
		if rec := handlertest.Get(r, path); rec.Code != http.StatusOK || rec.Body.String() == shell {
			t.Errorf("GET %s = %d, want 200 from its own route", path, rec.Code)
		}
	}
	if got := handlertest.Get(r, web.ReadyPath).Code; got != http.StatusServiceUnavailable {
		t.Errorf("GET %s = %d, want 503 before the coordinator runs", web.ReadyPath, got)
	}
	expectProblem(t, handlertest.Get(r, "/api/nowhere"), http.StatusNotFound, "Not Found", "/api/nowhere")

	for _, path := range []string{"/", "/about", "/runs/42/stages"} {
		if rec := handlertest.Get(r, path); rec.Code != http.StatusOK || rec.Body.String() != shell {
			t.Errorf("GET %s = %d %q, want the shell", path, rec.Code, rec.Body.String())
		}
	}
	if rec := handlertest.Get(r, "/assets/main-abc.js"); rec.Code != http.StatusOK || rec.Body.String() != "console.log('main')" {
		t.Errorf("GET /assets/main-abc.js = %d %q, want the asset", rec.Code, rec.Body.String())
	}
	expectClientProblem(t, handlertest.Get(r, "/assets/gone.js"), http.StatusNotFound, "/assets/gone.js")
}

// Mounted with NewHandlerModule under a prefix, the client sees the
// stripped path: the bare prefix redirects to the base with its query kept,
// and every path beneath it answers as at the root, a problem's instance
// naming the full path the browser asked for.
func TestNewClient_UnderAPrefix(t *testing.T) {
	r := web.NewRouter()
	r.Mount(module("/api", "/things"))
	r.Mount(web.NewHandlerModule("/app", newClient(t, clientOptions("/app"))))

	rec := handlertest.Get(r, "/app?x=1")
	if rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/app/?x=1" {
		t.Errorf("GET /app?x=1 = %d to %q, want 301 to /app/?x=1", rec.Code, rec.Header().Get("Location"))
	}
	if rec := handlertest.Get(r, "/app"); rec.Code != http.StatusMovedPermanently || rec.Header().Get("Location") != "/app/" {
		t.Errorf("GET /app = %d to %q, want 301 to /app/", rec.Code, rec.Header().Get("Location"))
	}
	for _, path := range []string{"/app/", "/app/runs/1"} {
		rec := handlertest.Get(r, path)
		if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `<base href="/app/">`) {
			t.Errorf("GET %s = %d %q, want the shell", path, rec.Code, rec.Body.String())
		}
	}
	rec = handlertest.Get(r, "/app/assets/main-abc.js")
	if rec.Code != http.StatusOK || rec.Body.String() != "console.log('main')" || rec.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Errorf("GET /app/assets/main-abc.js = %d %q, Cache-Control %q; want the immutable asset", rec.Code, rec.Body.String(), rec.Header().Get("Cache-Control"))
	}
	tag := handlertest.Get(r, "/app/favicon.svg").Header().Get("ETag")
	if rec := conditionalGet(r, "/app/favicon.svg", tag); tag == "" || rec.Code != http.StatusNotModified {
		t.Errorf("GET /app/favicon.svg with its ETag %q = %d, want 304", tag, rec.Code)
	}
	for _, path := range []string{"/app/assets/gone.js", "/app/.vite/manifest.json"} {
		t.Run(path, func(t *testing.T) {
			expectClientProblem(t, handlertest.Get(r, path), http.StatusNotFound, path)
		})
	}
	rec = handlertest.Serve(r, http.MethodPost, "/app/runs/1")
	expectClientProblem(t, rec, http.StatusMethodNotAllowed, "/app/runs/1")
	if got := handlertest.Get(r, "/api/things").Code; got != http.StatusOK {
		t.Errorf("GET /api/things = %d, want 200 from its module", got)
	}
}
