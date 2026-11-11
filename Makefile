-include .env
export

.PHONY: up up-infra mqtt sd down reset logs test carregar processar api

# Tudo em containers: MongoDB, PostgreSQL, migrações, processador e API.
up:
	podman compose up -d --build

# Só MongoDB, PostgreSQL e migrações (para rodar os comandos Go na máquina).
up-infra:
	podman compose up -d mongo postgres migrar

# Inclui o ingestor MQTT TTN (configure MQTT_* no .env).
mqtt:
	podman compose --profile mqtt up -d --build

# Inclui o ingestor MQTT do sensor de deslizamento (ChirpStack v4).
# Configure SD_MQTT_* no .env.
sd:
	podman compose --profile sensor_deslizamento up -d --build

# Sobe os dois ingestores ao mesmo tempo (TTN + ChirpStack).
mqtt+sd:
	podman compose --profile mqtt --profile sensor_deslizamento up -d --build

logs:
	podman compose --profile mqtt --profile sensor_deslizamento logs -f --tail=50

down:
	podman compose down

# Apaga os volumes (zera MongoDB e PostgreSQL).
reset:
	podman compose down -v

test:
	go test ./...

# Ingere testdata/mensagens_exemplo.json no MongoDB (sem MQTT).
carregar:
	go run ./cmd/carregar -arquivo testdata/mensagens_exemplo.json

# Ingere testdata/mensagens_sd_exemplo.json (ChirpStack v4) no MongoDB.
carregar-sd:
	go run ./cmd/carregar -arquivo testdata/mensagens_sd_exemplo.json

# Move documentos pendentes do MongoDB para o PostgreSQL. Encerre com Ctrl+C.
processar:
	go run ./cmd/processador

api:
	go run ./cmd/api
