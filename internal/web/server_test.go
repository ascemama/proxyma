package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func TestWithAssetsKeepsAPIRoutesOnAPIHandler(t *testing.T) {
	handler := WithAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/health" {
			t.Fatalf("unexpected API path %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusUnauthorized)
	}), testAssets())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/health", nil))

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusUnauthorized)
	}
}

func TestWithAssetsServesIndexForRootAndSPARoutes(t *testing.T) {
	handler := WithAssets(notFoundAPI(), testAssets())

	for _, requestPath := range []string{"/", "/settings/users"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, requestPath, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status = %d, want %d", requestPath, rec.Code, http.StatusOK)
		}
		if rec.Body.String() != "<html>Proxyma</html>" {
			t.Fatalf("%s body = %q", requestPath, rec.Body.String())
		}
	}
}

func TestWithAssetsServesStaticFiles(t *testing.T) {
	handler := WithAssets(notFoundAPI(), testAssets())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusOK)
	}
	if rec.Body.String() != "console.log('Proxyma');" {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestWithAssetsReturnsNotFoundForMissingStaticAsset(t *testing.T) {
	handler := WithAssets(notFoundAPI(), testAssets())

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/assets/missing.js", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func TestWithAssetsFallsBackToAPIWhenNoAssetsArePresent(t *testing.T) {
	handler := WithAssets(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}), nil)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusNotFound)
	}
}

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html":    {Data: []byte("<html>Proxyma</html>")},
		"assets/app.js": {Data: []byte("console.log('Proxyma');")},
	}
}

func notFoundAPI() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
}
