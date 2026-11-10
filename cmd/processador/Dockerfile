# syntax=docker/dockerfile:1
# Worker MongoDB -> PostgreSQL. Construa a partir da raiz do repositório:
#   podman build -f cmd/processador/Dockerfile -t monitoramento-processador .

FROM docker.io/library/golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/processador ./cmd/processador

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/processador /processador
ENTRYPOINT ["/processador"]
