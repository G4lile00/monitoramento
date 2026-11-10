package postgres

import (
	"tcc-monitoramento/internal/api"
	"tcc-monitoramento/internal/processamento"
)

// Verificações em tempo de compilação: Repo cumpre as interfaces dos
// pacotes de domínio.
var (
	_ processamento.Repositorio = (*Repo)(nil)
	_ api.Consultas             = (*Repo)(nil)
)
