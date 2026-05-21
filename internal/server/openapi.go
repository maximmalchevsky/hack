package server

import _ "embed"

//go:embed openapi.yaml
var openAPISpecRaw []byte

func openAPIYAML() ([]byte, error) {
	return openAPISpecRaw, nil
}
