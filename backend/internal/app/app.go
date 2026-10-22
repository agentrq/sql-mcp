// Package app wires the server together: the REST API on Fiber, the MCP
// servers and their OAuth endpoints on the standard mux, and the web app.
package app

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/adaptor"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/mustafaturan/monoflake"
	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/config"
	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/controller/mcpserver"
	"github.com/agentrq/sql-mcp/backend/internal/handler/admin"
	"github.com/agentrq/sql-mcp/backend/internal/handler/api"
	mcphandler "github.com/agentrq/sql-mcp/backend/internal/handler/mcp"
	"github.com/agentrq/sql-mcp/backend/internal/repository"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/service/dbdriver"
	"github.com/agentrq/sql-mcp/backend/internal/service/security"
	"github.com/agentrq/sql-mcp/backend/internal/service/tmphost"
)

// Version is the server's version, set at build time.
var Version = "0.1.0"

// Deps are the parts tests replace; New fills whatever is nil.
type Deps struct {
	Drivers *dbdriver.Registry
	Tmphost *tmphost.Client
	Google  auth.Provider
}

// New builds the server's handler from cfg.
func New(cfg config.Config, deps Deps) (http.Handler, error) {
	ids, err := monoflake.New(1)
	if err != nil {
		return nil, err
	}
	if cfg.AppDBDriver == "sqlite" {
		if err := os.MkdirAll(filepath.Dir(cfg.AppDBDSN), 0o700); err != nil {
			return nil, err
		}
	}
	repo, err := repository.Open(cfg.AppDBDriver, cfg.AppDBDSN, func() int64 { return ids.Next().Int64() })
	if err != nil {
		return nil, err
	}
	sealer, err := security.NewSealer(cfg.EncryptionKey)
	if err != nil {
		return nil, err
	}
	allowlist, err := auth.NewAllowlist(cfg.AllowedEmails)
	if err != nil {
		return nil, err
	}
	if allowlist.Empty() {
		log.Warn().Msg("ALLOWED_EMAILS is empty: nobody can sign in until an admin saves a configuration")
	}
	tokens, err := auth.NewTokenService(cfg.JWTSecret)
	if err != nil {
		return nil, err
	}
	if deps.Drivers == nil {
		deps.Drivers = dbdriver.NewRegistry(dbdriver.NewPostgres(), dbdriver.NewMySQL())
	}
	if deps.Tmphost == nil {
		deps.Tmphost = tmphost.New(cfg.TmphostAPIURL, nil)
	}
	if deps.Google == nil && cfg.GoogleClientID != "" {
		deps.Google = auth.NewGoogle(cfg.GoogleClientID, cfg.GoogleClientSecret, cfg.BaseURL+api.BasePath+"/auth/google/callback")
	}

	ctrl := crud.New(crud.Params{
		Repo: repo, Drivers: deps.Drivers, Sealer: sealer, Allowlist: allowlist, Tmphost: deps.Tmphost,
		Config: crud.Config{BaseURL: cfg.BaseURL, MaxRows: cfg.QueryMaxRows, InlineRows: cfg.QueryInlineRows, QueryTimeout: cfg.QueryTimeout},
	})
	if err := ctrl.LoadSetup(context.Background()); err != nil {
		return nil, err
	}

	fiberApp := fiber.New(fiber.Config{
		AppName:      "sql-mcp",
		BodyLimit:    4 << 20,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: cfg.QueryTimeout + 30*time.Second,
	})
	fiberApp.Use(recover.New())
	api.Register(api.Params{
		Router:       fiberApp.Group(api.BasePath),
		Crud:         ctrl,
		Tokens:       tokens,
		Google:       deps.Google,
		BaseURL:      cfg.BaseURL,
		CookieSecure: cfg.CookieSecure(),
		RootLogin:    cfg.RootLoginEnabled,
		RootToken:    cfg.RootToken,
		RootEmail:    cfg.RootEmail,
	})

	admin.Register(admin.Params{
		Router:       fiberApp.Group(admin.BasePath),
		Crud:         ctrl,
		Tokens:       tokens,
		RootToken:    cfg.RootToken,
		CookieSecure: cfg.CookieSecure(),
		Server: admin.Server{
			BaseURL:            cfg.BaseURL,
			SSLEnabled:         cfg.SSLEnabled,
			Domain:             cfg.Domain,
			HTTPSPort:          cfg.HTTPSPort,
			GoogleLoginEnabled: deps.Google != nil,
			RootLoginEnabled:   cfg.RootLoginEnabled,
			AppDBDriver:        cfg.AppDBDriver,
			TmphostAPIURL:      cfg.TmphostAPIURL,
		},
	})

	mux := http.NewServeMux()
	mcphandler.Register(mcphandler.Params{
		Mux:     mux,
		Owners:  ctrl,
		Tokens:  tokens,
		MCP:     mcpserver.NewHandler(ctrl, Version),
		BaseURL: cfg.BaseURL,
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	fiberHandler := adaptor.FiberApp(fiberApp)
	mux.Handle(api.BasePath+"/", fiberHandler)
	mux.Handle(admin.BasePath+"/", fiberHandler)
	mux.Handle("/", spa(cfg.PublicDir))
	return securityHeaders(mux), nil
}

// securityHeaders are sent on every response.
func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if !strings.HasPrefix(r.URL.Path, "/mcp/") {
			h.Set("X-Frame-Options", "DENY")
		}
		next.ServeHTTP(w, r)
	})
}

// spa serves the built web app: a file that exists as itself, anything else
// as index.html, so the router's paths survive a reload. index.html gets the
// base path the app addresses the API under.
func spa(dir string) http.Handler {
	files := http.FileServer(http.Dir(dir))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		clean := filepath.Clean("/" + r.URL.Path)
		if clean != "/" && clean != "/index.html" {
			if info, err := os.Stat(filepath.Join(dir, clean)); err == nil && !info.IsDir() {
				if strings.HasPrefix(clean, "/assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
				}
				files.ServeHTTP(w, r)
				return
			}
			// A missing asset is a 404, not the app: a stale tab asking for
			// an old bundle must not get HTML back as JavaScript.
			if strings.HasPrefix(clean, "/assets/") || filepath.Ext(clean) != "" {
				http.NotFound(w, r)
				return
			}
		}
		index, err := os.ReadFile(filepath.Join(dir, "index.html"))
		if errors.Is(err, os.ErrNotExist) {
			http.Error(w, "the web app is not built: run `make build` (frontend/dist → PUBLIC_DIR)", http.StatusNotFound)
			return
		}
		if err != nil {
			http.Error(w, "internal server error", http.StatusInternalServerError)
			return
		}
		// The build's URLs are relative (base ''), which breaks on a nested
		// route such as /databases/x, so they are made absolute here.
		index = bytes.ReplaceAll(index, []byte(`src="./`), []byte(`src="/`))
		index = bytes.ReplaceAll(index, []byte(`href="./`), []byte(`href="/`))
		index = bytes.ReplaceAll(index, []byte(`href="favicon.svg"`), []byte(`href="/favicon.svg"`))
		index = bytes.Replace(index, []byte("<head>"), []byte(`<head><script>window.__SQLMCP_BASE_PATH__="";</script>`), 1)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(index)
	})
}
