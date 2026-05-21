package ai

import (
	"bytes"
	"io"
	"strings"
)

func readerFromString(s string) io.Reader { return strings.NewReader(s) }
func readerFromBytes(b []byte) io.Reader  { return bytes.NewReader(b) }
