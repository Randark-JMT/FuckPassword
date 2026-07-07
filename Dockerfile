# syntax=docker/dockerfile:1

# ---- Stage 1: build the React frontend ----
FROM node:20-alpine AS frontend
WORKDIR /app
COPY frontend/package.json frontend/package-lock.json* ./
RUN npm ci || npm install --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

# ---- Stage 2: build the Go backend (embeds the frontend) ----
FROM golang:1.23-alpine AS backend
ENV GOPROXY=https://goproxy.cn,direct \
    CGO_ENABLED=0 \
    GOOS=linux
WORKDIR /src
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
# Replace the placeholder dist with the real built frontend.
COPY --from=frontend /app/dist ./web/dist
RUN go build -trimpath -ldflags="-s -w" -o /out/server .

# ---- Stage 3: minimal runtime ----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && \
    addgroup -S app && adduser -S -G app -u 10001 app && \
    mkdir -p /data/uploads && chown -R app:app /data
WORKDIR /data
ENV LISTEN_ADDR=:8080 \
    UPLOAD_DIR=/data/uploads \
    DSN="postgres://postgres:postgres@db:5432/fuckpassword?sslmode=disable" \
    STATEMENT_TIMEOUT=60s \
    MAX_QUEUE=20 \
    TTL_DAYS=7
COPY --from=backend /out/server /server
USER app
EXPOSE 8080
ENTRYPOINT ["/server"]
