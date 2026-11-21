# The client handler

This page explains how [`NewClient`](../client.go) serves a Vite build, and why. `NewClient`'s
documentation and the package documentation say what it serves.

## Caching

A hashed asset's name changes with its content, so a browser may keep it for a year and never
revalidate it. The shell and a build file Vite did not hash, such as one copied from the app's
public directory, keep their name across builds, so a browser revalidates them on every use: the
shell names the current build's hashed entry, and a stale one would load the previous build.

Every file the client serves as no-cache, that is, each file outside the directories of hashed
files (the manifest's directories, Vite's `build.assetsDir`), carries a strong ETag: the quoted
hex of the first 16 bytes of its SHA-256, enough that two builds' versions of a file never share
one. Dotfiles and dot directories get none, since they are never served. An embedded file has no
modification time, so no `Last-Modified` is sent; the ETag is the validator `http.ServeContent`
answers a conditional request against.

## The base

The base is written with exactly one trailing slash as the shell's `<base href>`. The root is
`/` alone; any other base must clean to itself without its one trailing slash, which `//` and
`/app//` do not, so they panic as malformed.

## The manifest and the shell

A manifest chunk records a module's output file, the CSS it pulls in, and the chunks it imports
statically. Dynamic imports are left out: the browser loads those when the module asks, and the
shell names nothing for them, so they are never required to exist.

Resolving a list of modules yields each module's file, the chunks it imports statically,
transitively, for modulepreload links, and the CSS of every one of them, in import order. A file
is named once across every module, and a chunk a script loads is never also preloaded. A static
import the manifest lacks makes the manifest corrupt, and the error names the key that imports
it. The entry resolves last, so the render-blocking modules' CSS comes first and the entry's
script after theirs.

`html/template` escapes the base and title as an attribute and as text, and the asset paths as
URLs. The config arrives as `template.JS`, already JSON that `json.Marshal` has escaped for HTML
(`<`, `>`, and `&` as `\u003c`, `\u003e`, and `\u0026`), so its text can never close the script
element and is written as it is. The render-blocking modules' scripts precede the entry's, in
their order, so the browser runs each before it and paints nothing until they have all run. The
body is empty: the client appends its own content.

## Requests

A request path is cleaned as a rooted path, so `..` never climbs out of the build, and the root
itself is a client route. Only the bare prefix, stripped to empty by `NewHandlerModule`, arrives
empty; it is redirected to the base. A missing asset's problem names, as its instance, the path
the browser asked for: the base joined with the path the handler sees, which a prefix module has
stripped.
