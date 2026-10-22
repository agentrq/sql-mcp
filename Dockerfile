FROM node:24-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS api
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /sql-mcp ./cmd/server && mkdir -p /data

FROM gcr.io/distroless/static-debian12:nonroot
WORKDIR /app
COPY --from=api /sql-mcp /app/sql-mcp
COPY --from=web /src/frontend/dist /app/public
# The nonroot user must own the volume to create the database and certificates.
COPY --from=api --chown=65532:65532 /data /data
ENV PUBLIC_DIR=/app/public APP_DB_DSN=/data/sqlmcp.db SSL_CACHE_DIR=/data/certs
VOLUME /data
EXPOSE 3000 3443
ENTRYPOINT ["/app/sql-mcp"]
