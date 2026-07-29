// Package api owns the canonical OpenAPI contract and its generation command.
package api

//go:generate go tool ogen --strict --clean --config ogen.yml --target ../internal/api/v1generated openapi.yaml
