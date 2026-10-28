# syntax=docker/dockerfile:1
FROM golang:1.27-alpine AS build
WORKDIR /src
# Сначала только go.mod/go.sum — слой не сбрасывается при правке кода
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# Статика и схема попадают в бинарник через go:embed, поэтому COPY web/ и db/ обязателен
RUN CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/tracker ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=build /out/tracker /app/tracker
EXPOSE 8080
USER nonroot:nonroot
ENTRYPOINT ["/app/tracker"]
