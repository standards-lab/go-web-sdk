package web

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"net/http"
	"path"
	"slices"
	"strings"
	"time"
)

// ManifestPath is where Vite writes its build manifest (build.manifest:
// true) inside the build's output directory.
const ManifestPath = ".vite/manifest.json"

// ConfigElementID is the id of the <script type="application/json"> element
// the shell writes the client's configuration into, the element the client
// reads at start.
const ConfigElementID = "app-config"

// The Cache-Control values the client handler sends. A hashed asset's name
// changes with its content, so a browser may keep it for a year and never
// revalidate it. The shell and a build file Vite did not hash, such as one
// copied from the app's public directory, keep their name across builds, so
// a browser revalidates them on every use: the shell names the current
// build's hashed entry, and a stale one would load the previous build.
const (
	cacheImmutable = "public, max-age=31536000, immutable"
	cacheNoCache   = "no-cache"
)

// ClientOptions configures [NewClient]. The zero value selects every
// default.
type ClientOptions struct {
	// Base is the path the client is served at, as the router mounts it:
	// "/" at the server's root, or a prefix such as "/app", with or without
	// its trailing slash; empty means "/". The shell writes it, with one
	// trailing slash, as its <base href>, against which the build's relative
	// asset URLs (Vite's base "./") and the client's router resolve.
	Base string
	// Title is the shell's <title>.
	Title string
	// RenderBlocking lists the manifest keys of modules the shell loads
	// ahead of the entry with blocking="render", in the order given, each
	// with its CSS and a modulepreload for each chunk it imports statically.
	// The browser paints nothing until each has run, so a module that writes
	// on the root element before first paint, such as a stored theme choice,
	// shows no default first.
	RenderBlocking []string
	// Config is the value the shell writes, JSON-encoded, as the content of
	// its #app-config element; nil writes null.
	Config any
}

// NewClient returns the handler that serves a client application from its
// Vite build, dist: the build's output directory, holding the manifest at
// [ManifestPath] and the files it names. entry is the manifest key of the
// client's entry, its source path relative to Vite's root, such as
// "src/main.ts".
//
// It reads the manifest and renders the shell once, here. It panics when
// opts.Base is malformed, and returns an error naming the problem when the
// manifest is missing or corrupt, it lacks entry or a key RenderBlocking
// lists, or Config does not encode as JSON.
//
// A request path, cleaned inside the build, is an asset, a missing asset,
// or a client route; a client route answers the shell, so a deep link or a
// reload reaches the client's router. The shell and every build file
// outside the manifest's directories, which a browser revalidates, carry a
// strong ETag hashed from their content here, once; a request whose
// If-None-Match matches it answers 304 with no body.
func NewClient(dist fs.FS, entry string, opts ClientOptions) (http.Handler, error) {
	base := clientBase(opts.Base)
	manifest, err := readManifest(dist)
	if err != nil {
		return nil, err
	}
	shell, err := renderShell(manifest, base, entry, opts)
	if err != nil {
		return nil, err
	}
	c := &client{
		dist:      dist,
		base:      base,
		hashed:    manifest.dirs(),
		shell:     shell,
		shellETag: etag(shell),
	}
	if c.etags, err = c.hashRevalidated(); err != nil {
		return nil, err
	}
	return c, nil
}

// etag returns data's strong entity tag: the quoted hex of the first 16
// bytes of its SHA-256, enough that two builds' versions of a file never
// share one.
func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

// hashRevalidated returns the ETag of every file the client serves as
// no-cache, by name: each file in dist outside the directories of hashed
// files, dotfiles and dot directories left out, since they are never
// served. A hashed file needs none: its name changes with its content, and
// a browser never revalidates it.
func (c *client) hashRevalidated() (map[string]string, error) {
	etags := map[string]string{}
	err := fs.WalkDir(c.dist, ".", func(name string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if name != "." && strings.HasPrefix(d.Name(), ".") {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}
		if d.IsDir() || c.underHashed(name) {
			return nil
		}
		data, err := fs.ReadFile(c.dist, name)
		if err != nil {
			return err
		}
		etags[name] = etag(data)
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("web: NewClient: hash the build's files: %w", err)
	}
	return etags, nil
}

// clientBase returns base with exactly one trailing slash, the shell's
// <base href>, empty selecting "/"; it panics when base is malformed.
func clientBase(base string) string {
	if base == "" {
		return "/"
	}
	// The root is "/" alone; any other base cleans to itself without its
	// one trailing slash, which "//" and "/app//" do not.
	prefix := strings.TrimSuffix(base, "/")
	clean := base == "/" || prefix != "/" && path.Clean(base) == prefix
	if !strings.HasPrefix(base, "/") || !clean || strings.ContainsAny(base, "?#") {
		panic(`web: NewClient: base must be a clean path beginning with '/', with no query or fragment, such as "/" or "/app": ` + base)
	}
	return prefix + "/"
}

// chunk is one entry of Vite's build manifest: a source module's output
// file, the CSS it pulls in, and the keys of the chunks it imports
// statically. Dynamic imports are left out: the browser loads those when the
// module asks, and the shell names nothing for them.
type chunk struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Imports []string `json:"imports"`
}

// manifest is Vite's build manifest, keyed by source path.
type manifest map[string]chunk

// readManifest reads and decodes dist's manifest.
func readManifest(dist fs.FS) (manifest, error) {
	data, err := fs.ReadFile(dist, ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("web: NewClient: read the build's manifest %s (is the app built?): %w", ManifestPath, err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("web: NewClient: decode the build's manifest %s: %w", ManifestPath, err)
	}
	return m, nil
}

// dirs returns the directories the manifest's files live in, Vite's
// build.assetsDir: the directories of hashed files.
func (m manifest) dirs() []string {
	var dirs []string
	for _, c := range m {
		for _, f := range append([]string{c.File}, c.CSS...) {
			if d := path.Dir(f); d != "." && !slices.Contains(dirs, d) {
				dirs = append(dirs, d)
			}
		}
	}
	return dirs
}

// assets is what the shell loads for a list of modules: each module's file,
// in order, the files of the chunks they import statically, for
// modulepreload links, and the CSS of every one of them, each once.
type assets struct {
	scripts, preload, css []string
}

// resolve resolves each key's chunk, in order, into a: its file, then the
// chunks it imports statically, transitively, and the CSS of it and every
// one of those chunks, in import order. A file is named once across every
// key, and a chunk a script loads is never also preloaded. It fails naming
// the first key the manifest lacks.
func (m manifest) resolve(keys ...string) (assets, error) {
	var a assets
	for _, key := range keys {
		root, ok := m[key]
		if !ok || root.File == "" {
			return assets{}, fmt.Errorf("web: NewClient: the build's manifest %s has no entry %q", ManifestPath, key)
		}
		a.scripts = append(a.scripts, root.File)
	}
	seen := map[string]bool{}
	var walk func(c chunk)
	walk = func(c chunk) {
		for _, f := range c.CSS {
			if !slices.Contains(a.css, f) {
				a.css = append(a.css, f)
			}
		}
		for _, k := range c.Imports {
			if seen[k] {
				continue
			}
			seen[k] = true
			imported := m[k]
			if !slices.Contains(a.scripts, imported.File) && !slices.Contains(a.preload, imported.File) {
				a.preload = append(a.preload, imported.File)
			}
			walk(imported)
		}
	}
	for _, key := range keys {
		seen[key] = true
		walk(m[key])
	}
	return a, nil
}

// shellTemplate is the client's HTML shell. html/template escapes the base
// and title as an attribute and as text, and the asset paths as URLs; the
// config arrives as template.JS, already JSON that json.Marshal has escaped
// for HTML (<, >, and & as <, >, and &), so its text can
// never close the script element, and is written as it is. The asset paths
// are relative, resolved against <base href>. The render-blocking modules'
// scripts precede the entry's, in their order, so the browser runs each
// before it, and paints nothing until they have all run. The body is empty:
// the client appends its own content.
var shellTemplate = template.Must(template.New("shell").Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<base href="{{.Base}}">
<title>{{.Title}}</title>
{{- range .CSS}}
<link rel="stylesheet" href="{{.}}">
{{- end}}
{{- range .Preload}}
<link rel="modulepreload" href="{{.}}">
{{- end}}
<script type="application/json" id="` + ConfigElementID + `">{{.Config}}</script>
{{- range .Blocking}}
<script type="module" blocking="render" src="{{.}}"></script>
{{- end}}
<script type="module" src="{{.Entry}}"></script>
</head>
<body></body>
</html>
`))

// renderShell renders the shell for entry and opts against m, once.
func renderShell(m manifest, base, entry string, opts ClientOptions) ([]byte, error) {
	// The entry resolves last, so the blocking modules' CSS comes first, and
	// its script after theirs.
	a, err := m.resolve(append(slices.Clone(opts.RenderBlocking), entry)...)
	if err != nil {
		return nil, err
	}
	blocking, entryFile := a.scripts[:len(a.scripts)-1], a.scripts[len(a.scripts)-1]
	config, err := json.Marshal(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("web: NewClient: encode the config: %w", err)
	}
	var buf bytes.Buffer
	err = shellTemplate.Execute(&buf, struct {
		Base, Title, Entry     string
		CSS, Preload, Blocking []string
		Config                 template.JS
	}{base, opts.Title, entryFile, a.css, a.preload, blocking, template.JS(config)})
	if err != nil {
		return nil, fmt.Errorf("web: NewClient: render the shell: %w", err)
	}
	return buf.Bytes(), nil
}

// client is NewClient's handler.
type client struct {
	dist      fs.FS
	base      string   // the <base href>, with its trailing slash
	hashed    []string // the directories of hashed files
	shell     []byte
	shellETag string
	etags     map[string]string // the ETags of the build's no-cache files, by name
}

// ServeHTTP answers a request path, after path.Clean, with one of three
// answers: an asset, a file in dist other than a dotfile or a file under a
// dot directory; a missing asset, a 404 problem for a path under a
// directory of hashed files, a dotfile path, or a path whose last segment
// has an extension, with no file behind it; or, for every other path, the
// shell. Only GET and HEAD are answered; the bare prefix NewHandlerModule
// strips to empty is redirected to the base.
func (c *client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		c.problem(w, r, http.StatusMethodNotAllowed, "the client answers only GET and HEAD")
		return
	}
	// Only the bare prefix, stripped by NewHandlerModule, arrives empty.
	if r.URL.Path == "" {
		target := c.base
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	// Cleaned as a rooted path, so ".." never climbs out of the build;
	// the root itself, "", is a client route.
	name := strings.TrimPrefix(path.Clean("/"+r.URL.Path), "/")
	if name == "" {
		c.serveShell(w, r)
		return
	}
	hidden := slices.ContainsFunc(strings.Split(name, "/"), func(s string) bool {
		return strings.HasPrefix(s, ".")
	})
	if !hidden {
		if data, err := fs.ReadFile(c.dist, name); err == nil {
			c.serveAsset(w, r, name, data)
			return
		}
	}
	if hidden || c.underHashed(name) || path.Ext(name) != "" {
		c.problem(w, r, http.StatusNotFound, "no asset "+name+" in the client's build")
		return
	}
	c.serveShell(w, r)
}

// underHashed reports whether name lies under a directory of hashed files.
func (c *client) underHashed(name string) bool {
	return slices.ContainsFunc(c.hashed, func(d string) bool {
		return strings.HasPrefix(name, d+"/")
	})
}

// serveShell answers the shell. http.ServeContent writes its Content-Type
// and Content-Length, leaves the body off a HEAD, and answers 304 to an
// If-None-Match naming the ETag set here.
func (c *client) serveShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheNoCache)
	w.Header().Set("ETag", c.shellETag)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(c.shell))
}

// serveAsset answers a build file's bytes, typed by its extension (a file
// with an unknown one is sniffed), with Range and HEAD handled by
// http.ServeContent. An embedded file has no modification time, so no
// Last-Modified is sent; a no-cache file carries its ETag instead, against
// which http.ServeContent answers a conditional request.
func (c *client) serveAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if c.underHashed(name) {
		w.Header().Set("Cache-Control", cacheImmutable)
	} else {
		w.Header().Set("Cache-Control", cacheNoCache)
		w.Header().Set("ETag", c.etags[name])
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

// problem writes status's problem, its instance the path the browser asked
// for: the base joined with the path the handler sees, which a prefix
// module has stripped.
func (c *client) problem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	instance := strings.TrimSuffix(c.base, "/") + r.URL.Path
	_ = Problem{Status: status, Detail: detail, Instance: instance}.WriteFor(w, r)
}
