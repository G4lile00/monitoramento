package api

import (
	_ "embed"
	"net/http"
)

// especOpenAPI é a especificação OpenAPI 3 da API, embutida no binário.
// Mantenha-a em sincronia com as rotas (api_test.go verifica os parâmetros).
//
//go:embed openapi.json
var especOpenAPI []byte

// paginaSwagger carrega o Swagger UI de um CDN (o navegador precisa de acesso
// à internet; a API em si não depende dele).
const paginaSwagger = `<!doctype html>
<html lang="pt-BR">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>API de monitoramento ambiental</title>
  <link rel="stylesheet" href="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui.css">
</head>
<body>
  <div id="swagger-ui"></div>
  <script src="https://unpkg.com/swagger-ui-dist@5.17.14/swagger-ui-bundle.js"></script>
  <script>
    window.ui = SwaggerUIBundle({
      url: "/openapi.json",
      dom_id: "#swagger-ui",
      deepLinking: true
    });
  </script>
</body>
</html>
`

func (s *servidor) swagger(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write([]byte(paginaSwagger))
}

func (s *servidor) openapi(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write(especOpenAPI)
}

func (s *servidor) raiz(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, "/swagger", http.StatusFound)
}
