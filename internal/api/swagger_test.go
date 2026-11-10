package api

import (
	"encoding/json"
	"net/http"
	"sort"
	"strings"
	"testing"
)

type especificacao struct {
	OpenAPI string `json:"openapi"`
	Paths   map[string]map[string]struct {
		Parameters []struct {
			Name    string `json:"name"`
			In      string `json:"in"`
			Example any    `json:"example"`
		} `json:"parameters"`
	} `json:"paths"`
}

func lerEspecificacao(t *testing.T) especificacao {
	t.Helper()
	rec := chamar(t, &consultasFalsas{}, "GET", "/openapi.json")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
	var e especificacao
	if err := json.Unmarshal(rec.Body.Bytes(), &e); err != nil {
		t.Fatalf("openapi.json não é JSON válido: %v", err)
	}
	return e
}

func TestOpenAPI_DocumentaTodasAsRotasDeDados(t *testing.T) {
	e := lerEspecificacao(t)
	if !strings.HasPrefix(e.OpenAPI, "3.") {
		t.Errorf("versão OpenAPI = %q", e.OpenAPI)
	}
	for _, rota := range []string{"/saude", "/sensores", "/leituras"} {
		if _, ok := e.Paths[rota]["get"]; !ok {
			t.Errorf("rota %s não documentada", rota)
		}
	}
}

// Os parâmetros documentados devem ser exatamente os que o handler lê, e os
// exemplos documentados devem ser aceitos pela própria API.
func TestOpenAPI_ParametrosDeLeiturasConferemComOHandler(t *testing.T) {
	e := lerEspecificacao(t)

	var nomes []string
	exemplo := map[string]string{}
	for _, p := range e.Paths["/leituras"]["get"].Parameters {
		if p.In != "query" {
			t.Errorf("parâmetro %s com in=%s", p.Name, p.In)
		}
		nomes = append(nomes, p.Name)
		exemplo[p.Name] = strings.TrimSuffix(strings.TrimSpace(formatar(p.Example)), ".0")
	}
	sort.Strings(nomes)
	if got, quer := strings.Join(nomes, ","), "ate,de,dev_eui,grandeza,limite"; got != quer {
		t.Fatalf("parâmetros documentados = %s, handler lê %s", got, quer)
	}

	alvo := "/leituras?de=" + exemplo["de"] + "&ate=" + exemplo["ate"] +
		"&dev_eui=" + exemplo["dev_eui"] + "&grandeza=" + exemplo["grandeza"] + "&limite=" + exemplo["limite"]
	if rec := chamar(t, &consultasFalsas{}, "GET", alvo); rec.Code != http.StatusOK {
		t.Errorf("a consulta de exemplo %q foi recusada: %d %s", alvo, rec.Code, rec.Body)
	}
}

func formatar(v any) string {
	b, _ := json.Marshal(v)
	return strings.Trim(string(b), `"`)
}

func TestSwagger_PaginaHTML(t *testing.T) {
	rec := chamar(t, &consultasFalsas{}, "GET", "/swagger")
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("Content-Type = %q", ct)
	}
	corpo := rec.Body.String()
	for _, trecho := range []string{"swagger-ui", "/openapi.json"} {
		if !strings.Contains(corpo, trecho) {
			t.Errorf("página sem %q", trecho)
		}
	}
}

func TestRaiz_RedirecionaParaSwagger(t *testing.T) {
	rec := chamar(t, &consultasFalsas{}, "GET", "/")
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/swagger" {
		t.Errorf("status=%d location=%q", rec.Code, rec.Header().Get("Location"))
	}
	// Rotas inexistentes continuam 404 (a raiz não vira coringa).
	if rec := chamar(t, &consultasFalsas{}, "GET", "/nao-existe"); rec.Code != http.StatusNotFound {
		t.Errorf("status = %d, quer 404", rec.Code)
	}
}
