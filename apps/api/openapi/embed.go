// Package openapi embeds the bundled OpenAPI document into the API binary,
// so GET /openapi.yaml and /openapi.json (internal/http) can serve it
// without any filesystem dependency at runtime — the same pattern
// apps/api/migrations/embed.go uses for the SQL migrations.
//
// openapi.bundled.yaml is generated (never hand-edited) by `make generate`
// from openapi.yaml and paths/*.yaml, components/schemas/*.yaml via
// `redocly bundle`; it is committed to the repo so the binary can embed it
// without a Node toolchain at build time.
package openapi

import _ "embed"

// Bundled holds the fully dereferenced OpenAPI 3.1 document, in YAML.
//
//go:embed openapi.bundled.yaml
var Bundled []byte
