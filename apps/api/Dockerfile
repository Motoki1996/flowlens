# syntax=docker/dockerfile:1

# ---- base ------------------------------------------------------------
FROM golang:1.26-alpine AS base
WORKDIR /app
RUN apk add --no-cache git
COPY go.mod go.sum ./
RUN go mod download

# ---- dev: hot reload with air ---------------------------------------
FROM base AS dev
RUN go install github.com/air-verse/air@v1.62.0
COPY . .
EXPOSE 8080
CMD ["air", "-c", ".air.toml"]

# ---- build: compile a static binary ---------------------------------
FROM base AS build
COPY . .
RUN CGO_ENABLED=0 go build -o /out/api ./cmd/api

# ---- runtime: minimal production image ------------------------------
FROM alpine:3.20 AS runtime
RUN apk add --no-cache ca-certificates
WORKDIR /app
COPY --from=build /out/api /app/api
EXPOSE 8080
USER nobody
ENTRYPOINT ["/app/api"]
