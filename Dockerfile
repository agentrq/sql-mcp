FROM node:24-alpine AS web
WORKDIR /src/frontend
COPY frontend/package.json frontend/package-lock.json ./
RUN npm ci --no-audit --no-fund
COPY frontend/ ./
RUN npm run build

FROM golang:1.27-alpine AS api
RUN apk add --no-cache ca-certificates
WORKDIR /src/backend
COPY backend/go.mod backend/go.sum ./
RUN go mod download
COPY backend/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /sql-mcp ./cmd/server
# scratch has no users: name UID 65532 (distroless's nonroot, kept so volumes
# chowned for the old image still work) and create the volume it must own.
RUN echo 'nonroot:x:65532:65532:nonroot:/:' > /etc_passwd && mkdir -p /data

# scratch holds only the binary, the web app, CA certs (Postgres TLS, tmphost,
# ACME) and /data: no shell or package manager to exploit.
FROM scratch
COPY --from=api /etc/ssl/certs/ca-certificates.crt /etc/ssl/certs/
COPY --from=api /etc_passwd /etc/passwd
COPY --from=api /sql-mcp /app/sql-mcp
COPY --from=web /src/frontend/dist /app/public
COPY --from=api --chown=65532:65532 /data /data
WORKDIR /app
ENV PUBLIC_DIR=/app/public APP_DB_DSN=/data/sqlmcp.db SSL_CACHE_DIR=/data/certs
VOLUME /data
EXPOSE 3000 3443
USER nonroot
ENTRYPOINT ["/app/sql-mcp"]
