# Persistência híbrida para monitoramento ambiental (MVP)

Fluxo: mensagem de uplink → **MongoDB** (bruto + fila) → worker de
processamento → **PostgreSQL/TimescaleDB/PostGIS** (estruturado) → **API REST**.

A plataforma suporta **dois formatos de uplink** em paralelo, sem alterar o
processador nem a API:

| Origem | Formato | Worker | Variáveis |
|---|---|---|---|
| **The Things Network** (TTN v3) | `end_device_ids` + `uplink_message.decoded_payload.message` (CSV) | `cmd/ingestor` | `MQTT_*` |
| **ChirpStack v4** (sensor de deslizamento) | `deduplicationId` + `deviceInfo` + `object` (JSON) | `cmd/sensor_deslizamento` | `SD_MQTT_*` |

A detecção é automática em `internal/contrato.ParseAutomatico`: pela
presença de `uplink_message` (TTN) ou `deviceInfo` (ChirpStack). O mesmo
`ingestao.Servico` e o mesmo `processador` consomem os dois — basta que as
grandezas estejam cadastradas no PostgreSQL.

A entrada de mensagens pode ser o **broker MQTT** (`cmd/ingestor` e/ou
`cmd/sensor_deslizamento`) ou um **arquivo JSON** (`cmd/carregar`, para testes
locais). Todos usam o mesmo `ingestao.Servico`, então validação, idempotência
e rejeições são idênticas.

## Estrutura

| Caminho | Conteúdo |
|---|---|
| `cmd/ingestor` | Worker MQTT: TTN v3 → MongoDB (ACK só depois de persistir) |
| `cmd/sensor_deslizamento` | Worker MQTT: ChirpStack v4 → MongoDB (sensor de deslizamento) |
| `cmd/carregar` | Ingere um arquivo JSON no MongoDB (substitui o MQTT nos testes) |
| `cmd/processador` | Worker: MongoDB (pendentes) → PostgreSQL |
| `cmd/api` | API REST de consulta e Swagger |
| `cmd/*/Dockerfile` | Uma imagem por componente (contexto de build na raiz) |
| `internal/contrato` | Contratos (TTN `uplink.go`, ChirpStack `chirpstack.go`, dispatcher `mensagem.go`), validação, idempotência, parsing das medições |
| `internal/fonte` | Leitura de mensagens de arquivo JSON |
| `internal/ingestao` | Validar e persistir (ou rejeitar) cada mensagem |
| `internal/processamento` | Laço do worker, reserva, retentativas |
| `internal/api` | Rotas HTTP |
| `internal/mongodb`, `internal/postgres` | Adaptadores de banco |
| `migrations/` | SQL do modelo (`0001_init` + `0002_grandezas_sd` com as grandezas do sensor de deslizamento) |
| `testdata/mensagens_exemplo.json` | 6 mensagens TTN: 3 válidas, 1 duplicada, 1 rejeitada (sem `dev_eui`) e 1 com medição ilegível |
| `testdata/mensagens_sd_exemplo.json` | Mensagens ChirpStack v4 para teste do sensor de deslizamento |

## Catálogo de grandezas

Definido em `migrations/0001_init.up.sql` e `migrations/0002_grandezas_sd.up.sql`.

| Código | Nome | Unidade | Origem |
|---|---|---|---|
| `temperatura` | Temperatura | C | TTN + ChirpStack |
| `umidade` | Umidade relativa | % | TTN + ChirpStack |
| `vibracao` | Vibração (pico) | m/s² | ChirpStack (`vibracao_pico_ms2`) |
| `bateria` | Tensão da bateria | V | ChirpStack (`bateria_v`) |
| `pressao` | Pressão atmosférica | hPa | ChirpStack (`pressao_hpa`) |
| `inclinacao` | Inclinação do sensor | ° | ChirpStack (`inclinacao_sensor_deg`) |
| `solo` | Umidade do solo | % | ChirpStack (`solo_pct`) |

Campos desconhecidos em `object` são ignorados (não derrubam o documento),
então um novo campo no ChirpStack só exige cadastrar a grandeza no PostgreSQL.

## Rodando localmente

### Tudo em containers (podman)

```bash
cp .env.example .env
make up          # MongoDB, PostgreSQL, migrações (0001+0002), processador e API
make carregar    # JSON TTN  -> MongoDB
make carregar-sd # JSON ChirpStack -> MongoDB
make sd          # opcional: sobe o ingestor ChirpStack (preencha SD_MQTT_* no .env)
make mqtt        # opcional: sobe o ingestor TTN (preencha MQTT_* no .env)
make logs
```

Para subir os dois ingestores ao mesmo tempo:

```bash
make mqtt+sd     # TTN + ChirpStack em paralelo
```

Swagger (com exemplos de queries): <http://localhost:8080/swagger>
· especificação: <http://localhost:8080/openapi.json>

> O `Makefile` usa `podman compose`; com Docker, troque por `docker compose`.
>
> Os dois ingestores MQTT dependem do cliente `paho`, que ainda não está no
> `go.mod`. Rode uma vez `go get github.com/eclipse/paho.mqtt.golang && go mod tidy`
> e faça commit de `go.mod` e `go.sum`. Até lá, os Dockerfiles resolvem a
> dependência no próprio build (`go mod tidy`); os demais componentes usam o
> `go.sum`.

### Comandos Go na máquina (sem containers de aplicação)

```bash
cp .env.example .env
make up-infra        # só MongoDB + PostgreSQL + migrações
make carregar        # JSON TTN -> MongoDB
make carregar-sd     # JSON ChirpStack -> MongoDB
make processar       # MongoDB -> PostgreSQL (Ctrl+C quando esvaziar a fila)
make api             # http://localhost:8080
```

Como as mensagens de exemplo são de 2025/2026, informe o intervalo (sem `de`/`ate`
a API consulta só as últimas 24 h):

```bash
curl localhost:8080/saude
curl localhost:8080/sensores
# TTN:
curl "localhost:8080/leituras?de=2025-08-21T00:00:00Z&ate=2025-08-22T00:00:00Z&dev_eui=70B3D57ED00724FA&grandeza=temperatura&limite=10"
# Sensor de deslizamento (ChirpStack):
curl "localhost:8080/leituras?de=2026-10-16T00:00:00Z&ate=2026-10-22T00:00:00Z&dev_eui=60B4D64FA899E3EB&grandeza=inclinacao&limite=10"
```

Resultado esperado com os exemplos:

- **TTN** (`testdata/mensagens_exemplo.json`): no MongoDB, 4 documentos em
  `mensagens_brutas` (a duplicada é ignorada) e 1 em `mensagens_rejeitadas`;
  após o processamento, 3 documentos `processado` (6 leituras) e 1 `erro`
  (valor `abc` não numérico).
- **ChirpStack** (`testdata/mensagens_sd_exemplo.json`): cada uplink vira
  7 leituras (vibração, temperatura, bateria, umidade, pressão, inclinação,
  solo) — exceto quando um campo aparece com valor 0, que ainda é gravado.
  Eventos sem `object` (status/join/erro do ChirpStack) vão para
  `mensagens_rejeitadas` com motivo `object ausente`. O sensor ganha
  `localizacao` automaticamente a partir das tags `Latitude`/`Longitude`/
  `Altitude` do `deviceInfo`.

## Contrato ChirpStack v4 (sensor de deslizamento)

Exemplo simplificado do payload esperado em `application/+/device/+/event/up`:

```json
{
  "deduplicationId": "833657f0-20de-4ad0-b56c-c06d776b0cd0",
  "time": "2026-10-21T17:11:58.016+00:00",
  "deviceInfo": {
    "applicationId": "4e97de35-d0ea-4b6d-8cfd-6a8cf88e9451",
    "applicationName": "TESTE_HELTEC_LORAWAN",
    "deviceName": "HELTEC_TESTE_017",
    "devEui": "60b4d64fa899e3eb",
    "tags": {
      "Latitude": "-26.917836273741372",
      "Longitude": "-48.66544228404989",
      "Altitude": "6.8 metros "
    }
  },
  "object": {
    "vibracao_pico_ms2": 1,
    "temperatura_c": 24.1,
    "bateria_v": 4.34,
    "umidade_pct": 49,
    "pressao_hpa": 1021,
    "inclinacao_sensor_deg": 155.48,
    "solo_pct": 7
  }
}
```

- **Idempotência**: `"cs:" + deduplicationId` (UUID gerado pelo ChirpStack).
- **MedidoEm**: campo `time` (UTC).
- **Localização**: `deviceInfo.tags.{Latitude,Longitude,Altitude}` — preenche
  `sensor.localizacao` e `sensor.altitude_m` na primeira vez que o sensor é
  visto, sem sobrescrever cadastro manual posterior.
- **Eventos rejeitados**: sem `deduplicationId` (logs do ChirpStack),
  sem `object` (joins/status), ou com `devEui` inválido.

## Rotas

| Rota | Descrição |
|---|---|
| `GET /swagger` | Documentação interativa (Swagger UI; carrega os scripts de um CDN) |
| `GET /openapi.json` | Especificação OpenAPI 3 |
| `GET /saude` | Verifica a conexão com o PostgreSQL |
| `GET /sensores` | Lista sensores (com latitude/longitude quando cadastradas) |
| `GET /leituras` | Filtros: `dev_eui`, `grandeza`, `de`, `ate` (RFC 3339), `limite` (1–1000, padrão 100) |

## Perfis do docker compose

| Comando | Serviços de aplicação sobem |
|---|---|
| `make up` | `processador` + `api` |
| `make mqtt` | + `ingestor` (TTN) |
| `make sd` | + `sensor_deslizamento` (ChirpStack) |
| `make mqtt+sd` | + os dois ingestores |

MongoDB, PostgreSQL e `migrar` sempre sobem.

## Testes

```bash
make test
```

Os testes usam apenas bancos falsos (sem Docker). A carga dos arquivos de
exemplo (TTN e ChirpStack) também é verificada em `internal/fonte` e
`internal/contrato`.
