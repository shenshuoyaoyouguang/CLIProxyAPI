package management

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/router-for-me/CLIProxyAPI/v7/internal/config"
)

// TestOAuthConfigMapsConcurrentReadWrite hammers the oauth-excluded-models and
// oauth-model-alias handlers with concurrent GET clones (management UI polling)
// and PUT/PATCH/DELETE mutations on the same maps. Run with -race: before the
// map accesses were serialized under h.mu this surfaced a fatal "concurrent map
// read and map write".
func TestOAuthConfigMapsConcurrentReadWrite(t *testing.T) {
	t.Setenv("MANAGEMENT_PASSWORD", "")

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.yaml")
	if err := os.WriteFile(cfgPath, []byte("port: 8080\n"), 0o644); err != nil {
		t.Fatalf("write initial config: %v", err)
	}

	h := NewHandler(&config.Config{
		AuthDir: dir,
		OAuthExcludedModels: map[string][]string{
			"provider-a": {"model-a", "model-b"},
			"provider-b": {"model-c"},
		},
		OAuthModelAlias: map[string][]config.OAuthModelAlias{
			"channel-a": {{Name: "upstream-a", Alias: "client-a"}},
			"channel-b": {{Name: "upstream-b", Alias: "client-b"}},
		},
	}, cfgPath, nil)

	start := make(chan struct{})
	var wg sync.WaitGroup
	rounds := 20
	opsPerRound := 10
	for i := 0; i < rounds*opsPerRound; i++ {
		op := i % opsPerRound
		wg.Add(1)
		go func(op int) {
			defer wg.Done()
			<-start
			runOAuthConcurrentOp(h, op)
		}(op)
	}
	close(start)
	wg.Wait()

	// The maps must still be readable and serializable after the stress.
	assertOAuthConfigReadable(t, h.GetOAuthExcludedModels)
	assertOAuthConfigReadable(t, h.GetOAuthModelAlias)
}

func runOAuthConcurrentOp(h *Handler, op int) {
	switch op {
	case 0:
		ctx, _ := oauthTestContext(http.MethodGet, "/", "")
		h.GetOAuthExcludedModels(ctx)
	case 1:
		ctx, _ := oauthTestContext(http.MethodGet, "/", "")
		h.GetOAuthModelAlias(ctx)
	case 2:
		ctx, _ := oauthTestContext(http.MethodPatch, "/", `{"provider":"provider-a","models":["m1","m2"]}`)
		h.PatchOAuthExcludedModels(ctx)
	case 3:
		ctx, _ := oauthTestContext(http.MethodPatch, "/", `{"provider":"provider-b","models":[]}`)
		h.PatchOAuthExcludedModels(ctx)
	case 4:
		ctx, _ := oauthTestContext(http.MethodDelete, "/?provider=provider-a", "")
		h.DeleteOAuthExcludedModels(ctx)
	case 5:
		ctx, _ := oauthTestContext(http.MethodPut, "/", `{"provider-c":["m3"],"provider-d":["m4"]}`)
		h.PutOAuthExcludedModels(ctx)
	case 6:
		ctx, _ := oauthTestContext(http.MethodPatch, "/", `{"channel":"channel-a","aliases":[{"name":"ua","alias":"ca"}]}`)
		h.PatchOAuthModelAlias(ctx)
	case 7:
		ctx, _ := oauthTestContext(http.MethodPatch, "/", `{"channel":"channel-b","aliases":[]}`)
		h.PatchOAuthModelAlias(ctx)
	case 8:
		ctx, _ := oauthTestContext(http.MethodDelete, "/?channel=channel-a", "")
		h.DeleteOAuthModelAlias(ctx)
	case 9:
		ctx, _ := oauthTestContext(http.MethodPut, "/", `{"channel-c":[{"name":"uc","alias":"cc"}]}`)
		h.PutOAuthModelAlias(ctx)
	}
}

func oauthTestContext(method, target, body string) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	req := httptest.NewRequest(method, target, strings.NewReader(body))
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	ctx.Request = req
	return ctx, rec
}

func assertOAuthConfigReadable(t *testing.T, fn func(*gin.Context)) {
	t.Helper()
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)
	ctx.Request = httptest.NewRequest(http.MethodGet, "/", nil)
	fn(ctx)
	if rec.Code != http.StatusOK {
		t.Fatalf("final GET status = %d, body %s", rec.Code, rec.Body.String())
	}
}
