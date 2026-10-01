package ogimage

import (
	"html/template"
	"net/http"
	"sort"
)

// previewPrefix is where, in development, the cards this process recorded are
// listed beside their pages.
const previewPrefix = "/_og-preview/"

var previewPage = template.Must(template.New("preview").Parse(`<!doctype html>
<meta charset="utf-8"><title>Cards — elagoht/ogimage</title>
<meta http-equiv="refresh" content="5">
<style>
body { font: 14px system-ui, sans-serif; margin: 24px; background: #f8fafc; color: #0f172a }
.card { margin: 0 0 32px } img { width: 600px; height: auto; border: 1px solid #cbd5e1; display: block }
code { color: #475569 }
</style>
<h1>Cards</h1>
<p>The card each page carried on its last render, newest pages first as they render. Open a page, and its card appears here; edit a card template and reload the page. This list refreshes every 5 seconds.</p>
{{range .}}<div class="card"><p><a href="{{.Path}}">{{.Path}}</a> · <code>{{.Template}}</code></p><img src="{{.URL}}" alt="{{.Path}}"></div>
{{else}}<p>No page has rendered yet.</p>{{end}}`))

// preview serves the list of cards, in development only.
func (p *Plugin) preview() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		type row struct{ Path, Template, URL string }
		p.mu.Lock()
		rows := make([]row, 0, len(p.recorded))
		for path, rec := range p.recorded {
			rows = append(rows, row{path, rec.Template, p.cfg.Prefix + hashOf(rec.URL) + ".png"})
		}
		p.mu.Unlock()
		sort.Slice(rows, func(i, j int) bool { return rows[i].Path < rows[j].Path })
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = previewPage.Execute(w, rows)
	})
}
