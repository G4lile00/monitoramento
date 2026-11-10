// Package api expõe a API REST de consulta aos dados estruturados (RF08, RF09).
// Usa apenas net/http (ServeMux com padrões de método, Go 1.22+).
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"monitoramento/internal/modelo"
)

const (
	limitePadrao = 100
	limiteMaximo = 1000
	janelaPadrao = 24 * time.Hour
)

// Consultas é o acesso de leitura ao PostgreSQL.
type Consultas interface {
	Pingar(ctx context.Context) error
	ListarSensores(ctx context.Context) ([]modelo.Sensor, error)
	ConsultarLeituras(ctx context.Context, f modelo.FiltroLeituras) ([]modelo.Leitura, error)
}

type servidor struct {
	c     Consultas
	log   *slog.Logger
	agora func() time.Time
}

// Handler monta as rotas da API.
func Handler(c Consultas, log *slog.Logger) http.Handler {
	return novo(c, log, time.Now)
}

func novo(c Consultas, log *slog.Logger, agora func() time.Time) http.Handler {
	s := &servidor{c: c, log: log, agora: agora}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /saude", s.saude)
	mux.HandleFunc("GET /sensores", s.sensores)
	mux.HandleFunc("GET /leituras", s.leituras)
	return mux
}

func (s *servidor) saude(w http.ResponseWriter, r *http.Request) {
	if err := s.c.Pingar(r.Context()); err != nil {
		s.log.Error("verificação de saúde falhou", "erro", err)
		escrever(w, http.StatusServiceUnavailable, map[string]string{"status": "indisponivel"})
		return
	}
	escrever(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *servidor) sensores(w http.ResponseWriter, r *http.Request) {
	sensores, err := s.c.ListarSensores(r.Context())
	if err != nil {
		s.log.Error("listar sensores", "erro", err)
		erro(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if sensores == nil {
		sensores = []modelo.Sensor{}
	}
	escrever(w, http.StatusOK, sensores)
}

// leituras aceita: dev_eui, grandeza, de, ate (RFC 3339) e limite.
// Sem de/ate, consulta as últimas 24 horas.
func (s *servidor) leituras(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	f := modelo.FiltroLeituras{Limite: limitePadrao, Ate: s.agora().UTC()}
	var err error

	if v := q.Get("ate"); v != "" {
		if f.Ate, err = time.Parse(time.RFC3339, v); err != nil {
			erro(w, http.StatusBadRequest, "parâmetro 'ate' deve estar em RFC 3339")
			return
		}
	}
	f.De = f.Ate.Add(-janelaPadrao)
	if v := q.Get("de"); v != "" {
		if f.De, err = time.Parse(time.RFC3339, v); err != nil {
			erro(w, http.StatusBadRequest, "parâmetro 'de' deve estar em RFC 3339")
			return
		}
	}
	if !f.De.Before(f.Ate) {
		erro(w, http.StatusBadRequest, "'de' deve ser anterior a 'ate'")
		return
	}

	if v := q.Get("limite"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > limiteMaximo {
			erro(w, http.StatusBadRequest, "'limite' deve estar entre 1 e "+strconv.Itoa(limiteMaximo))
			return
		}
		f.Limite = n
	}
	if v := strings.ToUpper(strings.TrimSpace(q.Get("dev_eui"))); v != "" {
		f.DevEUI = &v
	}
	if v := strings.ToLower(strings.TrimSpace(q.Get("grandeza"))); v != "" {
		f.Grandeza = &v
	}

	leituras, err := s.c.ConsultarLeituras(r.Context(), f)
	if err != nil {
		s.log.Error("consultar leituras", "erro", err)
		erro(w, http.StatusInternalServerError, "erro interno")
		return
	}
	if leituras == nil {
		leituras = []modelo.Leitura{}
	}
	escrever(w, http.StatusOK, leituras)
}

func erro(w http.ResponseWriter, status int, msg string) {
	escrever(w, status, map[string]string{"erro": msg})
}

func escrever(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
