# syntax=docker/dockerfile:1
# Comando carregar: lê um arquivo JSON e ingere no MongoDB pelo mesmo
# ingestao.Servico dos workers MQTT (validação, idempotência, rejeições).
# Construa a partir da raiz:
#   podman build -f cmd/carregar/Dockerfile -t monitoramento-carregar .

FROM docker.io/library/golang:1.25-alpine AS build
WORKDIR /src
COPY . .
RUN go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/carregar ./cmd/carregar

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/carregar /carregar
ENTRYPOINT ["/carregar"]
