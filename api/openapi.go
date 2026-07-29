package api

import _ "embed"

// OpenAPIDocument is the exact checked-in contract served to authenticated callers.
//
//go:embed openapi.yaml
var OpenAPIDocument []byte
