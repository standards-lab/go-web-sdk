package upload

// DraftRevision is the revision of draft-ietf-httpbis-resumable-upload
// the package implements.
const DraftRevision = 16

// InteropVersion is the Upload-Draft-Interop-Version that draft revision
// [DraftRevision] assigns: the client sends it, and the handler answers a
// creation that carries it with a 104 (Upload Resumption Supported).
const InteropVersion = 11
