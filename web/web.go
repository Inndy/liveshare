package web

import (
	"bytes"
	"embed"
	"net/http"
)

//go:embed index.html style.css app.js
var fs embed.FS

var (
	htmlContent []byte
)

func init() {
	indexHTML, err := fs.ReadFile("index.html")
	if err != nil {
		panic("missing web/index.html")
	}

	styleCSS, err := fs.ReadFile("style.css")
	if err != nil {
		panic("missing web/style.css")
	}

	appJS, err := fs.ReadFile("app.js")
	if err != nil {
		panic("missing web/app.js")
	}

	// Inject CSS
	cssInject := []byte("<style>\n" + string(styleCSS) + "\n</style>")
	indexHTML = bytes.Replace(indexHTML, []byte("<!-- CSS_INJECTION_POINT --><link href=\"style.css\" rel=\"stylesheet\">"), cssInject, 1)

	// Inject JS
	jsInject := []byte("<script>\n" + string(appJS) + "\n</script>")
	indexHTML = bytes.Replace(indexHTML, []byte("<!-- JS_INJECTION_POINT --><script src=\"app.js\"></script>"), jsInject, 1)

	htmlContent = indexHTML
}

// Handler returns an http.Handler that serves the embedded single-page application.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		w.Write(htmlContent)
	})
}
