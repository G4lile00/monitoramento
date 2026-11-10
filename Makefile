-include .env
export

.PHONY: up up-infra mqtt down reset logs test carregar processar api

# Tudo em containers: MongoDB, PostgreSQL, migrações, processador e API.
up:
	podman compose up -d --build

# Só MongoDB, PostgreSQL e migrações (para rodar os comandos Go na máquina).
up-infra:
	podman compose up -d mongo postgres migrar

# Inclui o ingestor MQTT (configure MQTT_* no .env).
mqtt:
	podman compose --profile mqtt up -d --build

logs:
	podman compose --profile mqtt logs -f --tail=50

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

# Move documentos pendentes do MongoDB para o PostgreSQL. Encerre com Ctrl+C.
processar:
	go run ./cmd/processador

api:
	go run ./cmd/api
