package sse

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type Writer struct {
	w http.ResponseWriter
	f http.Flusher
}

func NewWriter(w http.ResponseWriter) (*Writer, error) {
	f, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("streaming unsupported")
	}
	return &Writer{w: w, f: f}, nil
}

func (s *Writer) Send(message any) {
	b, _ := json.Marshal(message)
	fmt.Fprintf(s.w, "data: %s\n", b)
	s.f.Flush()
}
