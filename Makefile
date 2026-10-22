.PHONY: dev backend frontend build test test-backend test-frontend check-case fmt stop

# The backend, then the Vite dev server on :5173 once the backend is up.
# A missing backend/.env is created with development secrets (scripts/dev-env.sh).
dev:
	@./scripts/dev.sh

backend:
	@./scripts/dev-env.sh
	@cd backend && go run ./cmd/server

frontend:
	@cd frontend && npm install --no-audit --no-fund && npm run dev

# One binary and the web app it serves: backend/bin/sql-mcp + backend/public.
build:
	@cd frontend && npm ci --no-audit --no-fund && npm run build
	@rm -rf backend/public && cp -r frontend/dist backend/public
	@cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/sql-mcp ./cmd/server
	@echo "built backend/bin/sql-mcp; run it from backend/ (it serves ./public)"

test: check-case test-backend test-frontend

# Paths that differ only by case break checkouts on macOS and Windows.
check-case:
	@./scripts/check-case.sh

# The Postgres driver's tests run when SQLMCP_TEST_PG_DSN names a database; see README.
test-backend:
	@cd backend && go vet ./... && go test -cover ./...

test-frontend:
	@cd frontend && npm test

fmt:
	@cd backend && gofmt -w .

stop:
	-@lsof -ti:3000,5173 | xargs kill 2>/dev/null || true
