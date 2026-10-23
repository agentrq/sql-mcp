// Package live streams a database's queries to the browser as server-sent
// events. It sits on the standard mux, not Fiber: Fiber reaches net/http
// through an adaptor that buffers the whole response, so nothing would be
// sent until the stream ended.
package live

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/data/view"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
)

// accessCookie is the session cookie the REST API sets.
const accessCookie = "at"

// Watcher streams a database's queries.
type Watcher interface {
	WatchQueries(ctx context.Context, userID, id int64) (<-chan entity.QueryLog, func(), error)
}

// Params is what the handler needs.
type Params struct {
	Mux     *http.ServeMux
	Watcher Watcher
	Tokens  *auth.TokenService
	// Heartbeat is how often an idle stream sends a comment, so proxies do
	// not close it. Zero is 25 seconds.
	Heartbeat time.Duration
}

type handler struct {
	watcher   Watcher
	tokens    *auth.TokenService
	heartbeat time.Duration
}

// Register mounts the stream. The pattern is more specific than the API's
// `/api/v1/`, so the mux sends it here rather than to Fiber.
func Register(p Params) {
	h := &handler{watcher: p.Watcher, tokens: p.Tokens, heartbeat: p.Heartbeat}
	if h.heartbeat == 0 {
		h.heartbeat = 25 * time.Second
	}
	p.Mux.HandleFunc("GET /api/v1/databases/{id}/queries/stream", h.queries)
}

func fail(w http.ResponseWriter, status int, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(view.Error{Error: view.ErrorBody{Message: message, Code: status}})
}

// queries sends `event: query` with a query log entry as each one is
// recorded. It ends when the session expires, so a stream never outlives the
// cookie that opened it, and when the database is deleted; the browser
// reconnects, and is refused, either way.
func (h *handler) queries(w http.ResponseWriter, r *http.Request) {
	var userID int64
	var expires time.Time
	cookie, err := r.Cookie(accessCookie)
	if err == nil {
		if claims, err := h.tokens.ValidateAccessToken(cookie.Value); err == nil {
			userID, expires = crud.ParseID(claims.Subject), claims.ExpiresAt.Time
		}
	}
	if userID == 0 {
		fail(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	queries, stop, err := h.watcher.WatchQueries(r.Context(), userID, crud.ParseID(r.PathValue("id")))
	if errors.Is(err, entity.ErrNotFound) {
		fail(w, http.StatusNotFound, "not found")
		return
	}
	if err != nil {
		log.Error().Err(err).Str("path", r.URL.Path).Msg("request failed")
		fail(w, http.StatusInternalServerError, "internal server error")
		return
	}
	defer stop()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no") // nginx would hold events back
	w.WriteHeader(http.StatusOK)
	rc := http.NewResponseController(w)
	// The comment opens the stream at once, so the browser reports it open
	// before the first query arrives.
	if !send(w, rc, ": connected\n\n") {
		return
	}

	session := time.NewTimer(time.Until(expires))
	defer session.Stop()
	beat := time.NewTicker(h.heartbeat)
	defer beat.Stop()
	for {
		var msg string
		select {
		case <-r.Context().Done():
			return
		case <-session.C:
			return
		case q, open := <-queries:
			if !open {
				return
			}
			data, _ := json.Marshal(view.FromQueryLog(q))
			msg = fmt.Sprintf("id: %s\nevent: query\ndata: %s\n\n", q.ID, data)
		case <-beat.C:
			msg = ": ping\n\n"
		}
		if !send(w, rc, msg) {
			return
		}
	}
}

// send writes msg and flushes it, reporting whether the client is still
// there.
func send(w http.ResponseWriter, rc *http.ResponseController, msg string) bool {
	if _, err := w.Write([]byte(msg)); err != nil {
		return false
	}
	return rc.Flush() == nil
}
