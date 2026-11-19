package web_test

import (
	"encoding/json"
	"net/http"
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

func TestNewClient_FailsNamingTheProblem(t *testing.T) {
	noManifest := viteBuild()
	delete(noManifest, web.ManifestPath)
	badManifest := viteBuild()
	badManifest[web.ManifestPath] = &fstest.MapFile{Data: []byte("{")}

	cases := map[string]struct {
		dist  fstest.MapFS
		entry string
		opts  web.ClientOptions
		want  string
	}{
		"missing manifest": {noManifest, clientEntry, web.ClientOptions{}, web.ManifestPath},
		"corrupt manifest": {badManifest, clientEntry, web.ClientOptions{}, "decode the build's manifest"},
		"missing entry":    {viteBuild(), "src/index.ts", web.ClientOptions{}, `no entry "src/index.ts"`},
		"missing blocking": {viteBuild(), clientEntry, web.ClientOptions{RenderBlocking: []string{"src/register.ts", "src/theme.ts"}}, `no entry "src/theme.ts"`},
		"unencodable":      {viteBuild(), clientEntry, web.ClientOptions{Config: func() {}}, "encode the config"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h, err := web.NewClient(tc.dist, tc.entry, tc.opts)
			if err == nil {
				t.Fatalf("NewClient = %v, nil; want an error naming %q", h, tc.want)
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
