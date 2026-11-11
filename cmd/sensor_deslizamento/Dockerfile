# syntax=docker/dockerfile:1
# Worker MQTT (ChirpStack v4) -> MongoDB. Construa a partir da raiz:
#   podman build -f cmd/sensor_deslizamento/Dockerfile -t monitoramento-sensor-deslizamento .

FROM docker.io/library/golang:1.25-alpine AS build
WORKDIR /src
COPY . .
# O cliente MQTT (paho) ainda não está no go.mod/go.sum. "go mod tidy" o
# resolve no build. Depois de rodar "go get github.com/eclipse/paho.mqtt.golang"
# e fazer commit de go.mod/go.sum, troque por "go mod download".
RUN go mod tidy && \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/sensor_deslizamento ./cmd/sensor_deslizamento

# distroless/static inclui certificados CA (necessários para MQTT com TLS).
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/sensor_deslizamento /sensor_deslizamento
ENTRYPOINT ["/sensor_deslizamento"]
