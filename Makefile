-include .env
export

.PHONY: up down reset test carregar processar api

up:
	docker compose up -d

down:
	docker compose down

# Apaga os volumes (zera MongoDB e PostgreSQL).
reset:
	docker compose down -v

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
