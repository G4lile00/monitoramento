# syntax=docker/dockerfile:1
# API REST + Swagger. Construa a partir da raiz do repositório:
#   podman build -f cmd/api/Dockerfile -t monitoramento-api .

FROM docker.io/library/golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
