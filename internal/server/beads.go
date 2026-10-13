package server

import (
	"io"
	"net/http"

	"github.com/joalavedra/handoff/internal/beads"
)

func (s *Server) beadsExport(w http.ResponseWriter, r *http.Request) {
	p := s.apiPrincipal(w, r)
	if p == nil {
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="issues.jsonl"`)
	if _, err := beads.Export(r.Context(), s.Svc, p, r.URL.Query().Get("project"), w); err != nil {
		writeCoreErr(w, err)
	}
}

func (s *Server) beadsImport(w http.ResponseWriter, r *http.Request) {
	p := s.apiPrincipal(w, r)
	if p == nil {
		return
	}
	res, err := beads.Import(r.Context(), s.Svc, p, r.URL.Query().Get("project"), io.LimitReader(r.Body, 32<<20))
	if err != nil {
		writeCoreErr(w, err)
		return
	}
	writeJSON(w, 200, res)
}
