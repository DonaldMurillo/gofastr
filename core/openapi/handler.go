package openapi

import (
	"encoding/json"
	"net/http"
)

// Handler returns an http.Handler that serves the OpenAPI spec as JSON
// at the root path (typically mounted at /openapi.json or /docs/openapi.json).
func Handler(spec *Spec) http.Handler {
	doc := spec.Build()
	data, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		data = []byte(`{"error":"failed to marshal spec"}`)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Write(data)
	})
}

// SwaggerUIHandler returns an http.Handler that serves a minimal Swagger UI
// page which loads the spec from basePath+"/openapi.json".
func SwaggerUIHandler(spec *Spec, basePath string) http.Handler {
	specHandler := Handler(spec)
	uiHTML := `<!DOCTYPE html>
<html>
<head>
  <title>Swagger UI</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
  <script>
    SwaggerUIBundle({
      url: "` + basePath + `/openapi.json",
      dom_id: '#swagger-ui',
    });
  </script>
</body>
</html>`

	mux := http.NewServeMux()
	mux.Handle("/openapi.json", specHandler)
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(uiHTML))
	})

	// Strip the basePath prefix so internal routes (/openapi.json, /) match
	// when mounted at a sub-path like /docs/
	return http.StripPrefix(basePath, mux)
}
