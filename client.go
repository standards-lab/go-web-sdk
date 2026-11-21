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

// See docs/client.md

// ManifestPath is where Vite writes its build manifest (build.manifest:
// true) inside the build's output directory.
const ManifestPath = ".vite/manifest.json"

// ConfigElementID is the id of the <script type="application/json"> element
// the shell writes the client's configuration into, the element the client
// reads at start.
const ConfigElementID = "app-config"

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
// opts.Base is malformed. It returns an error naming the problem when the
// manifest is missing or corrupt; when the manifest lacks entry, a key
// RenderBlocking lists, or a chunk one of them imports statically; when
// Config does not encode as JSON; or when a build file cannot be read.
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

func etag(data []byte) string {
	sum := sha256.Sum256(data)
	return `"` + hex.EncodeToString(sum[:16]) + `"`
}

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
		return nil, fmt.Errorf("hash the build's files: %w", err)
	}
	return etags, nil
}

func clientBase(base string) string {
	if base == "" {
		return "/"
	}
	prefix := strings.TrimSuffix(base, "/")
	clean := base == "/" || prefix != "/" && path.Clean(base) == prefix
	if !strings.HasPrefix(base, "/") || !clean || strings.ContainsAny(base, "?#") {
		panic(`web: NewClient: base must be a clean path beginning with '/', with no query or fragment, such as "/" or "/app": ` + base)
	}
	return prefix + "/"
}

type chunk struct {
	File    string   `json:"file"`
	CSS     []string `json:"css"`
	Imports []string `json:"imports"`
}

type manifest map[string]chunk

func readManifest(dist fs.FS) (manifest, error) {
	data, err := fs.ReadFile(dist, ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("read the build's manifest %s (is the app built?): %w", ManifestPath, err)
	}
	var m manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("decode the build's manifest %s: %w", ManifestPath, err)
	}
	return m, nil
}

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

type assets struct {
	scripts, preload, css []string
}

func (m manifest) resolve(keys ...string) (assets, error) {
	var a assets
	for _, key := range keys {
		root, ok := m[key]
		if !ok || root.File == "" {
			return assets{}, fmt.Errorf("the build's manifest %s has no entry %q", ManifestPath, key)
		}
		a.scripts = append(a.scripts, root.File)
	}
	seen := map[string]bool{}
	var walk func(key string) error
	walk = func(key string) error {
		c := m[key]
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
			imported, ok := m[k]
			if !ok || imported.File == "" {
				return fmt.Errorf("the build's manifest %s has no entry %q, which %q imports", ManifestPath, k, key)
			}
			if !slices.Contains(a.scripts, imported.File) && !slices.Contains(a.preload, imported.File) {
				a.preload = append(a.preload, imported.File)
			}
			if err := walk(k); err != nil {
				return err
			}
		}
		return nil
	}
	for _, key := range keys {
		seen[key] = true
		if err := walk(key); err != nil {
			return assets{}, err
		}
	}
	return a, nil
}

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

func renderShell(m manifest, base, entry string, opts ClientOptions) ([]byte, error) {
	a, err := m.resolve(append(slices.Clone(opts.RenderBlocking), entry)...)
	if err != nil {
		return nil, err
	}
	blocking, entryFile := a.scripts[:len(a.scripts)-1], a.scripts[len(a.scripts)-1]
	config, err := json.Marshal(opts.Config)
	if err != nil {
		return nil, fmt.Errorf("encode the config: %w", err)
	}
	var buf bytes.Buffer
	err = shellTemplate.Execute(&buf, struct {
		Base, Title, Entry     string
		CSS, Preload, Blocking []string
		Config                 template.JS
	}{base, opts.Title, entryFile, a.css, a.preload, blocking, template.JS(config)})
	if err != nil {
		return nil, fmt.Errorf("render the shell: %w", err)
	}
	return buf.Bytes(), nil
}

type client struct {
	dist      fs.FS
	base      string
	hashed    []string
	shell     []byte
	shellETag string
	etags     map[string]string
}

func (c *client) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		c.problem(w, r, http.StatusMethodNotAllowed, "the client answers only GET and HEAD")
		return
	}
	if r.URL.Path == "" {
		target := c.base
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

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

func (c *client) underHashed(name string) bool {
	return slices.ContainsFunc(c.hashed, func(d string) bool {
		return strings.HasPrefix(name, d+"/")
	})
}

func (c *client) serveShell(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", cacheNoCache)
	w.Header().Set("ETag", c.shellETag)
	http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(c.shell))
}

func (c *client) serveAsset(w http.ResponseWriter, r *http.Request, name string, data []byte) {
	if c.underHashed(name) {
		w.Header().Set("Cache-Control", cacheImmutable)
	} else {
		w.Header().Set("Cache-Control", cacheNoCache)
		w.Header().Set("ETag", c.etags[name])
	}
	http.ServeContent(w, r, name, time.Time{}, bytes.NewReader(data))
}

func (c *client) problem(w http.ResponseWriter, r *http.Request, status int, detail string) {
	instance := strings.TrimSuffix(c.base, "/") + r.URL.Path
	_ = Problem{Status: status, Detail: detail, Instance: instance}.WriteFor(w, r)
}
