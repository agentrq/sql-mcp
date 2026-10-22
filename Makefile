.PHONY: dev backend frontend build test test-backend test-frontend fmt stop

# Start the backend (:3000) and the Vite dev server (:5173) together.
dev:
	@make -j 2 backend frontend

# The backend needs backend/.env; copy .env.example to start.
backend:
	@cd backend && test -f .env || { echo "backend/.env is missing: cp .env.example backend/.env and fill in the secrets"; exit 1; }
	@cd backend && go run ./cmd/server

frontend:
	@cd frontend && npm install --no-audit --no-fund && npm run dev

# One binary and the web app it serves: backend/bin/sql-mcp + backend/public.
build:
	@cd frontend && npm ci --no-audit --no-fund && npm run build
	@rm -rf backend/public && cp -r frontend/dist backend/public
	@cd backend && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o bin/sql-mcp ./cmd/server
	@echo "built backend/bin/sql-mcp; run it from backend/ (it serves ./public)"

test: test-backend test-frontend

# The Postgres driver's tests run when SQLMCP_TEST_PG_DSN names a database; see README.
test-backend:
	@cd backend && go vet ./... && go test -cover ./...

test-frontend:
	@cd frontend && npm test

fmt:
	@cd backend && gofmt -w .

stop:
	-@lsof -ti:3000,5173 | xargs kill 2>/dev/null || true
