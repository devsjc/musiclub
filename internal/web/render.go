package web

import (
	"embed"
	"html/template"
	"log/slog"
	"net/http"

	"musiclub/internal/domain"
)

//go:embed views/*.html
var viewsFS embed.FS

// page is the data every template receives. Club and Player are nil outside
// club routes or when nobody has picked an identity.
type page struct {
	Title  string
	Club   *domain.Club
	Player *domain.Player
	Error  string
	Data   any
}

// parseViews builds one template set per page (layout + page), so each page can
// define its own "content" block.
func parseViews() map[string]*template.Template {
	names := []string{"index", "home", "whoami", "admin", "round", "error"}
	out := make(map[string]*template.Template, len(names))
	for _, n := range names {
		out[n] = template.Must(template.ParseFS(viewsFS, "views/layout.html", "views/"+n+".html"))
	}
	return out
}

func (s *Server) render(w http.ResponseWriter, r *http.Request, status int, name string, p page) {
	if c, ok := r.Context().Value(clubKey).(domain.Club); ok { // absent outside club routes
		p.Club = &c
	}
	p.Player = playerFrom(r.Context())

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.views[name].ExecuteTemplate(w, "layout", p); err != nil {
		slog.Error("render", "view", name, "err", err)
	}
}

func (s *Server) fail(w http.ResponseWriter, r *http.Request, status int, msg string) {
	s.render(w, r, status, "error", page{Title: http.StatusText(status), Error: msg})
}
