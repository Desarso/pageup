package server

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/desarso/pageup/internal/api"
	pageclient "github.com/desarso/pageup/internal/client"
	"github.com/desarso/pageup/internal/protocol"
)

type testEnvironment struct {
	server     *httptest.Server
	privateKey ed25519.PrivateKey
	config     pageclient.Config
	now        time.Time
}

func newTestEnvironment(t *testing.T, maxBytes int64) testEnvironment {
	t.Helper()
	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	bootstrap, err := json.Marshal([]api.Key{{
		Name:      "test admin",
		PublicKey: protocol.EncodePublicKey(publicKey),
		Role:      RoleAdmin,
	}})
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(Config{
		DataDir:       t.TempDir(),
		BootstrapKeys: string(bootstrap),
		MaxPageBytes:  maxBytes,
		Version:       "test",
		Now:           time.Now,
		Logger:        slog.New(slog.NewTextHandler(io.Discard, nil)),
	})
	if err != nil {
		t.Fatal(err)
	}
	httpServer := httptest.NewServer(service.Handler())
	config := pageclient.Config{
		Version:    1,
		Endpoint:   httpServer.URL,
		KeyID:      protocol.KeyID(publicKey),
		PrivateKey: protocol.EncodePrivateKey(privateKey),
		Name:       "test admin",
	}
	return testEnvironment{server: httpServer, privateKey: privateKey, config: config, now: now}
}

func (environment testEnvironment) close() {
	environment.server.Close()
}

func TestUploadRequiresSignatureAndServesImmutablePage(t *testing.T) {
	environment := newTestEnvironment(t, 1<<20)
	defer environment.close()

	unsigned, err := http.Post(environment.server.URL+"/api/pages", "text/html", strings.NewReader("<h1>no</h1>"))
	if err != nil {
		t.Fatal(err)
	}
	unsigned.Body.Close()
	if unsigned.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unsigned status = %d", unsigned.StatusCode)
	}

	client, err := pageclient.New(environment.config, "test")
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Upload(context.Background(), []byte("<!doctype html><h1>hello</h1>"))
	if err != nil {
		t.Fatal(err)
	}
	if !result.Created || !strings.HasPrefix(result.URL, environment.server.URL+"/") {
		t.Fatalf("unexpected upload result: %#v", result)
	}

	response, err := http.Get(result.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	body, _ := io.ReadAll(response.Body)
	if response.StatusCode != http.StatusOK || string(body) != "<!doctype html><h1>hello</h1>" {
		t.Fatalf("page response %d %q", response.StatusCode, body)
	}
	if response.Header.Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected cache header %q", response.Header.Get("Cache-Control"))
	}
}

func TestLandingPublishesSiteFavicon(t *testing.T) {
	environment := newTestEnvironment(t, 1<<20)
	defer environment.close()

	landing, err := http.Get(environment.server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	landingBody, err := io.ReadAll(landing.Body)
	landing.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if landing.StatusCode != http.StatusOK || !strings.Contains(string(landingBody), `href="/favicon.svg"`) {
		t.Fatalf("landing status = %d, favicon link missing", landing.StatusCode)
	}

	favicon, err := http.Get(environment.server.URL + "/favicon.svg")
	if err != nil {
		t.Fatal(err)
	}
	faviconBody, err := io.ReadAll(favicon.Body)
	favicon.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if favicon.StatusCode != http.StatusOK {
		t.Fatalf("favicon status = %d", favicon.StatusCode)
	}
	if favicon.Header.Get("Content-Type") != "image/svg+xml" {
		t.Fatalf("favicon content type = %q", favicon.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(faviconBody), `<svg`) || !strings.Contains(string(faviconBody), `#c9ff3d`) {
		t.Fatal("favicon response does not contain the Pageup SVG")
	}

	head, err := http.Head(environment.server.URL + "/favicon.ico")
	if err != nil {
		t.Fatal(err)
	}
	head.Body.Close()
	if head.StatusCode != http.StatusOK || head.Header.Get("Content-Length") == "" {
		t.Fatalf("favicon fallback HEAD status = %d, length = %q", head.StatusCode, head.Header.Get("Content-Length"))
	}
}

func TestTamperedAndReplayedRequestsAreRejected(t *testing.T) {
	environment := newTestEnvironment(t, 1<<20)
	defer environment.close()
	nonce, _ := protocol.NewUUIDv7(environment.now)
	original := []byte("<p>original</p>")

	tampered, _ := http.NewRequest(http.MethodPost, environment.server.URL+"/api/pages", bytes.NewReader([]byte("<p>tampered</p>")))
	tampered.Header.Set("Content-Type", "text/html")
	protocol.SignRequest(tampered, environment.privateKey, nonce, original, environment.now)
	response, err := http.DefaultClient.Do(tampered)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("tampered status = %d", response.StatusCode)
	}

	expiredNonce, _ := protocol.NewUUIDv7(environment.now.Add(2 * time.Millisecond))
	expired, _ := http.NewRequest(http.MethodPost, environment.server.URL+"/api/pages", bytes.NewReader(original))
	expired.Header.Set("Content-Type", "text/html")
	protocol.SignRequest(expired, environment.privateKey, expiredNonce, original, environment.now.Add(-6*time.Minute))
	response, err = http.DefaultClient.Do(expired)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired status = %d", response.StatusCode)
	}

	nonce, _ = protocol.NewUUIDv7(environment.now.Add(time.Millisecond))
	request, _ := http.NewRequest(http.MethodPost, environment.server.URL+"/api/pages", bytes.NewReader(original))
	request.Header.Set("Content-Type", "text/html")
	protocol.SignRequest(request, environment.privateKey, nonce, original, environment.now)
	headers := request.Header.Clone()
	response, err = http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusCreated {
		t.Fatalf("first status = %d", response.StatusCode)
	}

	replay, _ := http.NewRequest(http.MethodPost, environment.server.URL+"/api/pages", bytes.NewReader(original))
	replay.Header = headers
	response, err = http.DefaultClient.Do(replay)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized {
		t.Fatalf("replay status = %d", response.StatusCode)
	}
}

func TestPerDeviceRolesAndKeyManagement(t *testing.T) {
	environment := newTestEnvironment(t, 1<<20)
	defer environment.close()
	adminClient, err := pageclient.New(environment.config, "test")
	if err != nil {
		t.Fatal(err)
	}
	uploadPublic, uploadPrivate, _ := ed25519.GenerateKey(rand.Reader)
	added, err := adminClient.AddKey(context.Background(), api.AddKeyRequest{
		Name:      "upload laptop",
		PublicKey: protocol.EncodePublicKey(uploadPublic),
		Role:      RoleUpload,
	})
	if err != nil {
		t.Fatal(err)
	}
	if added.Role != RoleUpload {
		t.Fatalf("role = %s", added.Role)
	}
	uploadConfig := pageclient.Config{
		Version:    1,
		Endpoint:   environment.server.URL,
		KeyID:      protocol.KeyID(uploadPublic),
		PrivateKey: protocol.EncodePrivateKey(uploadPrivate),
	}
	uploadClient, err := pageclient.New(uploadConfig, "test")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := uploadClient.Upload(context.Background(), []byte("<p>allowed</p>")); err != nil {
		t.Fatalf("upload key could not upload: %v", err)
	}
	if _, err := uploadClient.ListKeys(context.Background()); err == nil {
		t.Fatal("upload key unexpectedly listed keys")
	}
	if _, err := adminClient.RevokeKey(context.Background(), environment.config.KeyID); err == nil {
		t.Fatal("last admin was unexpectedly revoked")
	}
	keys, err := adminClient.ListKeys(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(keys) != 2 {
		t.Fatalf("key count = %d", len(keys))
	}
}

func TestUploadSizeLimit(t *testing.T) {
	environment := newTestEnvironment(t, 16)
	defer environment.close()
	nonce, _ := protocol.NewUUIDv7(environment.now)
	body := bytes.Repeat([]byte("x"), 17)
	request, _ := http.NewRequest(http.MethodPost, environment.server.URL+"/api/pages", bytes.NewReader(body))
	request.Header.Set("Content-Type", "text/html")
	protocol.SignRequest(request, environment.privateKey, nonce, body, environment.now)
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d", response.StatusCode)
	}
}

func TestKeyStorePersists(t *testing.T) {
	directory := t.TempDir()
	publicKey, _, _ := ed25519.GenerateKey(rand.Reader)
	bootstrap, _ := json.Marshal([]api.Key{{Name: "admin", PublicKey: protocol.EncodePublicKey(publicKey), Role: RoleAdmin}})
	now := time.Now().UTC()
	store, err := NewKeyStore(directory+"/keys.json", string(bootstrap), now)
	if err != nil {
		t.Fatal(err)
	}
	secondPublic, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, _, err := store.Add("second", secondPublic, RoleUpload, now); err != nil {
		t.Fatal(err)
	}
	reloaded, err := NewKeyStore(directory+"/keys.json", "", now)
	if err != nil {
		t.Fatal(err)
	}
	if len(reloaded.List()) != 2 {
		t.Fatalf("persisted key count = %d", len(reloaded.List()))
	}
	info, err := os.Stat(directory + "/keys.json")
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("key store mode = %o", info.Mode().Perm())
	}
}
