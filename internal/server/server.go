package server

import (
	"crypto/ed25519"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/desarso/pageup/internal/api"
	"github.com/desarso/pageup/internal/protocol"
)

const defaultMaxPageBytes int64 = 5 << 20

var uuidV7Pattern = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

type Config struct {
	DataDir       string
	PublicURL     string
	DownloadsDir  string
	BootstrapKeys string
	MaxPageBytes  int64
	Version       string
	Logger        *slog.Logger
	Now           func() time.Time
}

type Server struct {
	config Config
	keys   *KeyStore
	nonces struct {
		sync.Mutex
		used map[string]time.Time
	}
}

func New(config Config) (*Server, error) {
	if config.DataDir == "" {
		config.DataDir = "./data"
	}
	if config.MaxPageBytes <= 0 {
		config.MaxPageBytes = defaultMaxPageBytes
	}
	if config.Logger == nil {
		config.Logger = slog.Default()
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	config.PublicURL = strings.TrimRight(config.PublicURL, "/")
	if config.PublicURL != "" {
		parsed, err := url.Parse(config.PublicURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.Path != "" {
			return nil, errors.New("PAGEUP_PUBLIC_URL must be an origin such as https://pages.example.com")
		}
	}
	pagesDir := filepath.Join(config.DataDir, "pages")
	if err := os.MkdirAll(pagesDir, 0o700); err != nil {
		return nil, fmt.Errorf("create pages directory: %w", err)
	}
	keys, err := NewKeyStore(filepath.Join(config.DataDir, "keys.json"), config.BootstrapKeys, config.Now())
	if err != nil {
		return nil, err
	}
	server := &Server{config: config, keys: keys}
	server.nonces.used = make(map[string]time.Time)
	return server, nil
}

func (server *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.handleHealth)
	mux.HandleFunc("/robots.txt", server.handleRobots)
	mux.HandleFunc("/install.sh", server.handleInstallShell)
	mux.HandleFunc("/install.ps1", server.handleInstallPowerShell)
	mux.HandleFunc("/downloads/", server.handleDownload)
	mux.HandleFunc("/api/pages", server.handleUpload)
	mux.HandleFunc("/api/keys", server.handleKeys)
	mux.HandleFunc("/api/keys/", server.handleKey)
	mux.HandleFunc("/api/whoami", server.handleWhoAmI)
	mux.HandleFunc("/", server.handlePage)
	return server.securityHeaders(server.accessLog(mux))
}

func (server *Server) handleHealth(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writeJSON(writer, http.StatusOK, map[string]string{"status": "ok", "version": server.config.Version})
}

func (server *Server) handleRobots(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (server *Server) handleUpload(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodPost {
		methodNotAllowed(writer, http.MethodPost)
		return
	}
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || (mediaType != "text/html" && mediaType != "application/xhtml+xml") {
		writeError(writer, http.StatusUnsupportedMediaType, "Content-Type must be text/html")
		return
	}
	body, ok := readBody(writer, request, server.config.MaxPageBytes)
	if !ok {
		return
	}
	_, nonce, ok := server.authorize(writer, request, body, false)
	if !ok {
		return
	}
	if !uuidV7Pattern.MatchString(nonce) {
		writeError(writer, http.StatusUnauthorized, "authentication failed")
		return
	}
	if len(body) == 0 {
		writeError(writer, http.StatusBadRequest, "HTML page cannot be empty")
		return
	}
	path := filepath.Join(server.config.DataDir, "pages", nonce+".html")
	created, err := writeImmutable(path, body)
	if err != nil {
		if errors.Is(err, errContentConflict) {
			writeError(writer, http.StatusConflict, "page id already exists with different content")
			return
		}
		server.config.Logger.Error("write page", "error", err)
		writeError(writer, http.StatusInternalServerError, "could not store page")
		return
	}
	status := http.StatusCreated
	if !created {
		status = http.StatusOK
	}
	writeJSON(writer, status, api.UploadResponse{
		ID:      nonce,
		URL:     server.publicURL(request) + "/" + nonce,
		Created: created,
	})
}

func (server *Server) handleWhoAmI(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	key, _, ok := server.authorize(writer, request, nil, false)
	if !ok {
		return
	}
	writeJSON(writer, http.StatusOK, api.WhoAmIResponse{Key: key})
}

func (server *Server) handleKeys(writer http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodGet:
		_, _, ok := server.authorize(writer, request, nil, true)
		if !ok {
			return
		}
		writeJSON(writer, http.StatusOK, api.KeyListResponse{Keys: server.keys.List()})
	case http.MethodPost:
		body, ok := readBody(writer, request, 64<<10)
		if !ok {
			return
		}
		_, _, ok = server.authorize(writer, request, body, true)
		if !ok {
			return
		}
		var input api.AddKeyRequest
		if err := json.Unmarshal(body, &input); err != nil {
			writeError(writer, http.StatusBadRequest, "invalid JSON request")
			return
		}
		publicKey, err := protocol.DecodePublicKey(input.PublicKey)
		if err != nil {
			writeError(writer, http.StatusBadRequest, "invalid public key")
			return
		}
		if input.Role == "" {
			input.Role = RoleUpload
		}
		key, created, err := server.keys.Add(input.Name, publicKey, input.Role, server.config.Now())
		if err != nil {
			writeError(writer, http.StatusBadRequest, err.Error())
			return
		}
		status := http.StatusCreated
		if !created {
			status = http.StatusOK
		}
		writeJSON(writer, status, key)
	default:
		writer.Header().Set("Allow", "GET, POST")
		writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
	}
}

func (server *Server) handleKey(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodDelete {
		methodNotAllowed(writer, http.MethodDelete)
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/api/keys/")
	if id == "" || strings.Contains(id, "/") {
		http.NotFound(writer, request)
		return
	}
	_, _, ok := server.authorize(writer, request, nil, true)
	if !ok {
		return
	}
	key, err := server.keys.Remove(id)
	if errors.Is(err, os.ErrNotExist) {
		writeError(writer, http.StatusNotFound, "key not found")
		return
	}
	if err != nil {
		writeError(writer, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(writer, http.StatusOK, key)
}

func (server *Server) handlePage(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet && request.Method != http.MethodHead {
		methodNotAllowed(writer, http.MethodGet)
		return
	}
	if request.URL.Path == "/" {
		server.handleLanding(writer)
		return
	}
	id := strings.TrimPrefix(request.URL.Path, "/")
	if !uuidV7Pattern.MatchString(id) {
		http.NotFound(writer, request)
		return
	}
	path := filepath.Join(server.config.DataDir, "pages", id+".html")
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		http.NotFound(writer, request)
		return
	}
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "could not read page")
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "could not read page")
		return
	}
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	writer.Header().Set("Content-Disposition", "inline")
	http.ServeContent(writer, request, id+".html", info.ModTime(), file)
}

func (server *Server) authorize(writer http.ResponseWriter, request *http.Request, body []byte, adminOnly bool) (api.Key, string, bool) {
	fail := func(reason string) (api.Key, string, bool) {
		server.config.Logger.Warn("authentication rejected", "reason", reason, "remote", request.RemoteAddr, "path", request.URL.Path)
		writeError(writer, http.StatusUnauthorized, "authentication failed")
		return api.Key{}, "", false
	}
	keyID := request.Header.Get(protocol.HeaderKeyID)
	nonce := request.Header.Get(protocol.HeaderNonce)
	timestampValue := request.Header.Get(protocol.HeaderTimestamp)
	signatureValue := request.Header.Get(protocol.HeaderSignature)
	if keyID == "" || nonce == "" || timestampValue == "" || signatureValue == "" {
		return fail("missing headers")
	}
	if !uuidV7Pattern.MatchString(nonce) {
		return fail("invalid nonce")
	}
	timestamp, err := strconv.ParseInt(timestampValue, 10, 64)
	if err != nil {
		return fail("invalid timestamp")
	}
	now := server.config.Now()
	requestTime := time.Unix(timestamp, 0)
	if requestTime.Before(now.Add(-5*time.Minute)) || requestTime.After(now.Add(5*time.Minute)) {
		return fail("timestamp outside allowed window")
	}
	publicKey, key, found := server.keys.PublicKey(keyID)
	if !found {
		return fail("unknown key")
	}
	if adminOnly && key.Role != RoleAdmin {
		return fail("admin key required")
	}
	signature, err := protocol.DecodeSignature(signatureValue)
	if err != nil {
		return fail("invalid signature encoding")
	}
	canonical := protocol.Canonical(request.Method, request.URL.EscapedPath(), timestamp, nonce, body)
	if !ed25519.Verify(publicKey, canonical, signature) {
		return fail("invalid signature")
	}
	if !server.useNonce(keyID, nonce, now) {
		return fail("replayed nonce")
	}
	return key, nonce, true
}

func (server *Server) useNonce(keyID, nonce string, now time.Time) bool {
	server.nonces.Lock()
	defer server.nonces.Unlock()
	cutoff := now.Add(-10 * time.Minute)
	for existing, usedAt := range server.nonces.used {
		if usedAt.Before(cutoff) {
			delete(server.nonces.used, existing)
		}
	}
	key := keyID + ":" + nonce
	if _, exists := server.nonces.used[key]; exists {
		return false
	}
	server.nonces.used[key] = now
	return true
}

func (server *Server) publicURL(request *http.Request) string {
	if server.config.PublicURL != "" {
		return server.config.PublicURL
	}
	scheme := request.Header.Get("X-Forwarded-Proto")
	if scheme != "http" && scheme != "https" {
		scheme = "http"
	}
	return scheme + "://" + request.Host
}

var errContentConflict = errors.New("content conflict")

func writeImmutable(path string, body []byte) (bool, error) {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if errors.Is(err, os.ErrExist) {
		existing, readErr := os.ReadFile(path)
		if readErr != nil {
			return false, readErr
		}
		if string(existing) != string(body) {
			return false, errContentConflict
		}
		return false, nil
	}
	if err != nil {
		return false, err
	}
	name := file.Name()
	ok := false
	defer func() {
		file.Close()
		if !ok {
			os.Remove(name)
		}
	}()
	if _, err := file.Write(body); err != nil {
		return false, err
	}
	if err := file.Sync(); err != nil {
		return false, err
	}
	if err := file.Close(); err != nil {
		return false, err
	}
	ok = true
	return true, nil
}

func readBody(writer http.ResponseWriter, request *http.Request, maxBytes int64) ([]byte, bool) {
	request.Body = http.MaxBytesReader(writer, request.Body, maxBytes)
	body, err := io.ReadAll(request.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(writer, http.StatusRequestEntityTooLarge, "request body is too large")
		} else {
			writeError(writer, http.StatusBadRequest, "could not read request body")
		}
		return nil, false
	}
	return body, true
}

func methodNotAllowed(writer http.ResponseWriter, allowed string) {
	writer.Header().Set("Allow", allowed)
	writeError(writer, http.StatusMethodNotAllowed, "method not allowed")
}

func writeError(writer http.ResponseWriter, status int, message string) {
	writeJSON(writer, status, api.ErrorResponse{Error: message})
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	writer.Header().Set("Content-Type", "application/json; charset=utf-8")
	writer.WriteHeader(status)
	json.NewEncoder(writer).Encode(value)
}

func (server *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		writer.Header().Set("Referrer-Policy", "no-referrer")
		writer.Header().Set("X-Frame-Options", "SAMEORIGIN")
		next.ServeHTTP(writer, request)
	})
}

func (server *Server) accessLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		next.ServeHTTP(writer, request)
		server.config.Logger.Info("request", "method", request.Method, "path", request.URL.Path, "duration", time.Since(started), "remote", request.RemoteAddr)
	})
}

const landingHTML = `<!doctype html>
<html lang="en">
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>Pageup</title>
<style>
  :root { color-scheme: dark; font-family: ui-monospace, SFMono-Regular, Menlo, Consolas, monospace; }
  body { margin: 0; min-height: 100vh; display: grid; place-items: center; background: #0c0d0e; color: #ede7d9; }
  main { width: min(42rem, calc(100% - 3rem)); }
  h1 { font-size: clamp(2.8rem, 10vw, 6rem); letter-spacing: -.08em; margin: 0 0 1rem; }
  p { color: #a9a49a; line-height: 1.65; }
  code { display: block; overflow-x: auto; padding: 1rem; border: 1px solid #34322e; background: #151616; color: #b9f5a8; }
  .dot { color: #f7a65a; }
</style>
<main>
  <h1>pageup<span class="dot">.</span></h1>
  <p>Private uploads. Shareable, unlisted HTML pages.</p>
  <code>curl -fsSL {{.URL}}/install.sh | sh</code>
</main>
</html>`

func (server *Server) handleLanding(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/html; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	template.Must(template.New("landing").Parse(landingHTML)).Execute(writer, map[string]string{"URL": server.config.PublicURL})
}
