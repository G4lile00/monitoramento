# Persistência híbrida para monitoramento ambiental (MVP)

Fluxo: mensagem de uplink (TTN) → **MongoDB** (bruto + fila) → worker de
processamento → **PostgreSQL/TimescaleDB/PostGIS** (estruturado) → **API REST**.

A entrada de mensagens pode ser o **broker MQTT** (`cmd/ingestor`) ou um **arquivo
JSON** (`cmd/carregar`, para testes locais). Os dois usam o mesmo
`ingestao.Servico`, então validação, idempotência e rejeições são idênticas.

## Estrutura

| Caminho | Conteúdo |
|---|---|
| `cmd/ingestor` | Worker: MQTT → MongoDB (ACK só depois de persistir) |
| `cmd/carregar` | Ingere um arquivo JSON no MongoDB (substitui o MQTT nos testes) |
| `cmd/processador` | Worker: MongoDB (pendentes) → PostgreSQL |
| `cmd/api` | API REST de consulta e Swagger |
| `cmd/*/Dockerfile` | Uma imagem por componente (contexto de build na raiz) |
| `internal/contrato` | Contrato do uplink TTN, validação, chave de idempotência, parsing das medições |
| `internal/fonte` | Leitura de mensagens de arquivo JSON |
| `internal/ingestao` | Validar e persistir (ou rejeitar) cada mensagem |
| `internal/processamento` | Laço do worker, reserva, retentativas |
| `internal/api` | Rotas HTTP |
| `internal/mongodb`, `internal/postgres` | Adaptadores de banco |
| `migrations/` | SQL do modelo (`modelo_banco_hibrido.dbml`) |
| `testdata/mensagens_exemplo.json` | 6 mensagens: 3 válidas, 1 duplicada, 1 rejeitada (sem `dev_eui`) e 1 com medição ilegível |

## Rodando localmente

### Tudo em containers (podman)

```bash
cp .env.example .env
make up          # MongoDB, PostgreSQL, migrações, processador e API
make carregar    # JSON -> MongoDB (o processador em container move para o PostgreSQL)
make mqtt        # opcional: inclui o ingestor MQTT (preencha MQTT_* no .env)
make logs
```

Swagger (com exemplos de queries): <http://localhost:8080/swagger>
· especificação: <http://localhost:8080/openapi.json>

> O `Makefile` usa `podman compose`; com Docker, troque por `docker compose`.
>
> O ingestor MQTT depende do cliente `paho`, que ainda não está no `go.mod`.
> Rode uma vez `go get github.com/eclipse/paho.mqtt.golang && go mod tidy` e
> faça commit de `go.mod` e `go.sum`. Até lá, o Dockerfile do ingestor resolve
> a dependência no próprio build (`go mod tidy`); os outros dois usam o `go.sum`.

### Comandos Go na máquina (sem containers de aplicação)

```bash
cp .env.example .env
make up-infra    # só MongoDB + PostgreSQL + migrações
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
| `GET /swagger` | Documentação interativa (Swagger UI; carrega os scripts de um CDN) |
| `GET /openapi.json` | Especificação OpenAPI 3 |
| `GET /saude` | Verifica a conexão com o PostgreSQL |
| `GET /sensores` | Lista sensores (com latitude/longitude quando cadastradas) |
| `GET /leituras` | Filtros: `dev_eui`, `grandeza`, `de`, `ate` (RFC 3339), `limite` (1–1000, padrão 100) |

## Testes

```bash
make test
```

Os testes usam apenas bancos falsos (sem Docker). A carga do arquivo de
exemplo também é verificada em `internal/fonte`.
