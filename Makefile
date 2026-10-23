.PHONY: dev backend frontend build test test-backend test-frontend check-case cover lint fmt stop

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

# The drivers' tests run when SQLMCP_TEST_PG_DSN, SQLMCP_TEST_MYSQL_DSN and
# SQLMCP_TEST_MARIADB_DSN name databases; see README.
test-backend:
	@cd backend && go vet ./... && go test -cover ./...

test-frontend:
	@cd frontend && npm test

# What CI enforces: the backend coverage floor (and, with BASE=<ref>, 100% of
# lines added since it), then the frontend thresholds in vitest.config.js.
# CI's floor (backend.yml) is higher because it runs the drivers' tests too.
COVER_MIN ?= 92.5
cover:
	@cd backend && go test -coverpkg=$$(go list ./... | grep -v -e /testutil -e /cmd/ | paste -sd, -) -coverprofile=coverage.out ./...
	@./scripts/coverage.sh backend/coverage.out $(COVER_MIN) $(BASE)
	@cd frontend && npm run test:coverage

lint:
	@cd backend && golangci-lint run ./...
	@cd frontend && npm run lint

fmt:
	@cd backend && gofmt -w .

stop:
	-@lsof -ti:3000,5173 | xargs kill 2>/dev/null || true
