package live_test

import (
	"bufio"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/agentrq/sql-mcp/backend/internal/controller/crud"
	"github.com/agentrq/sql-mcp/backend/internal/data/entity"
	"github.com/agentrq/sql-mcp/backend/internal/handler/live"
	"github.com/agentrq/sql-mcp/backend/internal/service/auth"
	"github.com/agentrq/sql-mcp/backend/internal/testutil"
)

const (
	userID = int64(1_000_001)
	dbID   = int64(1_000_002)
)

// watcher hands out ch to userID for dbID, and fails otherwise.
type watcher struct {
	ch      chan entity.QueryLog
	err     error
	stopped chan struct{}
}

func (w *watcher) WatchQueries(_ context.Context, uid, id int64) (<-chan entity.QueryLog, func(), error) {
	if w.err != nil {
		return nil, nil, w.err
	}
	if uid != userID || id != dbID {
		return nil, nil, entity.ErrNotFound
	}
	return w.ch, func() { close(w.stopped) }, nil
}

func newWatcher() *watcher {
	return &watcher{ch: make(chan entity.QueryLog, 4), stopped: make(chan struct{})}
}

func tokens(t *testing.T) *auth.TokenService {
	t.Helper()
	s, err := auth.NewTokenService(testutil.Secret)
	require.NoError(t, err)
	return s
}

func session(t *testing.T) string {
	t.Helper()
	tok, err := tokens(t).CreateAccessToken(crud.FormatID(userID), "a@example.com", "A", "")
	require.NoError(t, err)
	return tok
}

func server(t *testing.T, w live.Watcher, heartbeat time.Duration) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	live.Register(live.Params{Mux: mux, Watcher: w, Tokens: tokens(t), Heartbeat: heartbeat})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func open(t *testing.T, srv *httptest.Server, id int64, token string) *http.Response {
	t.Helper()
	req, err := http.NewRequest("GET", srv.URL+"/api/v1/databases/"+crud.FormatID(id)+"/queries/stream", nil)
	require.NoError(t, err)
	if token != "" {
		req.AddCookie(&http.Cookie{Name: "at", Value: token})
	}
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { _ = res.Body.Close() })
	return res
}

// next reads one event (up to its blank line) from the stream.
func next(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	var b strings.Builder
	for {
		line, err := r.ReadString('\n')
		require.NoError(t, err)
		if line == "\n" {
			return b.String()
		}
		b.WriteString(line)
	}
}

func TestStreamRefusesWithoutAValidSessionOrDatabase(t *testing.T) {
	srv := server(t, newWatcher(), 0)
	assert.Equal(t, http.StatusUnauthorized, open(t, srv, dbID, "").StatusCode)
	assert.Equal(t, http.StatusUnauthorized, open(t, srv, dbID, "not-a-jwt").StatusCode)
	admin, _, err := tokens(t).CreateAdminToken()
	require.NoError(t, err)
	assert.Equal(t, http.StatusUnauthorized, open(t, srv, dbID, admin).StatusCode, "an admin token is not a session")

	res := open(t, srv, dbID+1, session(t))
	assert.Equal(t, http.StatusNotFound, res.StatusCode)
	assert.Equal(t, "application/json", res.Header.Get("Content-Type"))

	broken := newWatcher()
	broken.err = errors.New("db down")
	res = open(t, server(t, broken, 0), dbID, session(t))
	assert.Equal(t, http.StatusInternalServerError, res.StatusCode)
}

func TestStreamSendsQueriesAndHeartbeats(t *testing.T) {
	w := newWatcher()
	res := open(t, server(t, w, 20*time.Millisecond), dbID, session(t))
	require.Equal(t, http.StatusOK, res.StatusCode)
	assert.Equal(t, "text/event-stream", res.Header.Get("Content-Type"))
	r := bufio.NewReader(res.Body)
	assert.Equal(t, ": connected\n", next(t, r))

	w.ch <- entity.QueryLog{ID: "q1", SQL: "select 1", Status: "ok"}
	ev := next(t, r)
	for ev == ": ping\n" {
		ev = next(t, r)
	}
	assert.True(t, strings.HasPrefix(ev, "id: q1\nevent: query\ndata: {"), ev)
	assert.Contains(t, ev, `"sql":"select 1"`)
	assert.Equal(t, ": ping\n", next(t, r))

	close(w.ch) // the database was deleted
	_, err := r.ReadString('\n')
	assert.Error(t, err)
	<-w.stopped
}

func TestStreamEndsWithTheSession(t *testing.T) {
	w := newWatcher()
	claims := auth.Claims{RegisteredClaims: jwt.RegisteredClaims{
		Subject:   crud.FormatID(userID),
		Audience:  jwt.ClaimStrings{auth.AccessAudience},
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(2 * time.Second)),
	}}
	short, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(testutil.Secret))
	require.NoError(t, err)

	res := open(t, server(t, w, time.Hour), dbID, short)
	require.Equal(t, http.StatusOK, res.StatusCode)
	r := bufio.NewReader(res.Body)
	next(t, r)
	_, err = r.ReadString('\n')
	assert.Error(t, err)
	<-w.stopped
}

func TestStreamStopsWhenTheClientLeaves(t *testing.T) {
	w := newWatcher()
	srv := server(t, w, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/v1/databases/"+crud.FormatID(dbID)+"/queries/stream", nil)
	req.AddCookie(&http.Cookie{Name: "at", Value: session(t)})
	res, err := srv.Client().Do(req)
	require.NoError(t, err)
	defer res.Body.Close()
	next(t, bufio.NewReader(res.Body))
	cancel()
	<-w.stopped
}

// unflushable is a writer the stream cannot flush, like a proxy that buffers.
type unflushable struct{ header http.Header }

func (u *unflushable) Header() http.Header         { return u.header }
func (u *unflushable) Write(b []byte) (int, error) { return len(b), nil }
func (u *unflushable) WriteHeader(int)             {}

// failing accepts n writes, then fails, like a client that went away.
type failing struct {
	*httptest.ResponseRecorder
	n int
}

func (f *failing) Write(b []byte) (int, error) {
	if f.n == 0 {
		return 0, errors.New("broken pipe")
	}
	f.n--
	return f.ResponseRecorder.Write(b)
}

func TestStreamStopsWhenItCannotWrite(t *testing.T) {
	w := newWatcher()
	mux := http.NewServeMux()
	live.Register(live.Params{Mux: mux, Watcher: w, Tokens: tokens(t), Heartbeat: time.Millisecond})
	req := httptest.NewRequest("GET", "/api/v1/databases/"+crud.FormatID(dbID)+"/queries/stream", nil)
	req.AddCookie(&http.Cookie{Name: "at", Value: session(t)})

	mux.ServeHTTP(&unflushable{header: http.Header{}}, req)
	<-w.stopped

	for _, n := range []int{0, 1} { // the opening comment, then a heartbeat
		w = newWatcher()
		mux = http.NewServeMux()
		live.Register(live.Params{Mux: mux, Watcher: w, Tokens: tokens(t), Heartbeat: time.Millisecond})
		mux.ServeHTTP(&failing{ResponseRecorder: httptest.NewRecorder(), n: n}, req)
		<-w.stopped
	}
}

func TestStreamDefaultsItsHeartbeat(t *testing.T) {
	w := newWatcher()
	res := open(t, server(t, w, 0), dbID, session(t))
	require.Equal(t, http.StatusOK, res.StatusCode)
}
