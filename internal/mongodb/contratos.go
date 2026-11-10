package mongodb

import (
	"monitoramento/internal/ingestao"
	"monitoramento/internal/processamento"
)

// Verificações em tempo de compilação: Store cumpre as interfaces dos
// pacotes de domínio.
var (
	_ ingestao.Armazenamento = (*Store)(nil)
	_ processamento.Fila     = (*Store)(nil)
)
