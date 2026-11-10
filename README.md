# Persistência híbrida para monitoramento ambiental (MVP)

Fluxo: mensagem de uplink (TTN) → **MongoDB** (bruto + fila) → worker de
processamento → **PostgreSQL/TimescaleDB/PostGIS** (estruturado) → **API REST**.

Neste momento a entrada de mensagens é feita por **arquivo JSON** (`cmd/carregar`),
no lugar do broker MQTT. Ele usa o mesmo `ingestao.Servico` que o worker MQTT
usará, então trocar a fonte não altera validação, idempotência nem rejeições.

## Estrutura

| Caminho | Conteúdo |
|---|---|
| `cmd/carregar` | Ingere um arquivo JSON no MongoDB (substitui o MQTT nos testes) |
| `cmd/processador` | Worker: MongoDB (pendentes) → PostgreSQL |
| `cmd/api` | API REST de consulta |
| `internal/contrato` | Contrato do uplink TTN, validação, chave de idempotência, parsing das medições |
| `internal/fonte` | Leitura de mensagens de arquivo JSON |
| `internal/ingestao` | Validar e persistir (ou rejeitar) cada mensagem |
| `internal/processamento` | Laço do worker, reserva, retentativas |
| `internal/api` | Rotas HTTP |
| `internal/mongodb`, `internal/postgres` | Adaptadores de banco |
| `migrations/` | SQL do modelo (`modelo_banco_hibrido.dbml`) |
| `testdata/mensagens_exemplo.json` | 6 mensagens: 3 válidas, 1 duplicada, 1 rejeitada (sem `dev_eui`) e 1 com medição ilegível |

## Rodando localmente

```bash
cp .env.example .env
make up          # MongoDB + PostgreSQL + migrações (Docker)
make carregar    # JSON -> MongoDB
make processar   # MongoDB -> PostgreSQL (Ctrl+C quando esvaziar a fila)
make api         # http://localhost:8080
```

Como as mensagens de exemplo são de 2025, informe o intervalo (sem `de`/`ate`
a API consulta só as últimas 24 h):

```bash
curl localhost:8080/saude
curl localhost:8080/sensores
curl "localhost:8080/leituras?de=2025-08-21T00:00:00Z&ate=2025-08-22T00:00:00Z"
curl "localhost:8080/leituras?de=2025-08-21T00:00:00Z&ate=2025-08-22T00:00:00Z&dev_eui=70B3D57ED00724FA&grandeza=temperatura&limite=10"
```

Resultado esperado do exemplo: no MongoDB, 4 documentos em `mensagens_brutas`
(a duplicada é ignorada) e 1 em `mensagens_rejeitadas`; após o processamento,
3 documentos `processado` (6 leituras) e 1 `erro` (valor `abc` não numérico).

## Rotas

| Rota | Descrição |
|---|---|
| `GET /saude` | Verifica a conexão com o PostgreSQL |
| `GET /sensores` | Lista sensores (com latitude/longitude quando cadastradas) |
| `GET /leituras` | Filtros: `dev_eui`, `grandeza`, `de`, `ate` (RFC 3339), `limite` (1–1000, padrão 100) |

## Testes

```bash
make test
```

Os testes usam apenas bancos falsos (sem Docker). A carga do arquivo de
exemplo também é verificada em `internal/fonte`.
