package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestModulePWAManifestsAreIndependentAndInstallable(t *testing.T) {
	t.Parallel()
	app := &portalApp{}
	seenIDs := map[string]bool{}
	for _, module := range []string{"financial", "operational"} {
		req := httptest.NewRequest(http.MethodGet, "/pwa/"+module+"/manifest.webmanifest", nil)
		rec := httptest.NewRecorder()
		app.modulePWAAsset(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s manifest returned %d", module, rec.Code)
		}
		var manifest modulePWAManifest
		if err := json.Unmarshal(rec.Body.Bytes(), &manifest); err != nil {
			t.Fatal(err)
		}
		if manifest.ID == "" || seenIDs[manifest.ID] {
			t.Fatalf("%s manifest does not have an independent id: %q", module, manifest.ID)
		}
		seenIDs[manifest.ID] = true
		if !strings.Contains(manifest.StartURL, "module="+module) || manifest.Scope != "/" || manifest.Display != "standalone" {
			t.Fatalf("unexpected %s manifest: %#v", module, manifest)
		}
	}
}

func TestModulePWAServiceWorkerCanControlModuleRoutesWithoutCachingData(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/pwa/sw.js", nil)
	rec := httptest.NewRecorder()
	(&portalApp{}).modulePWAAsset(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if rec.Header().Get("Service-Worker-Allowed") != "/" {
		t.Fatalf("unexpected service worker scope: %q", rec.Header().Get("Service-Worker-Allowed"))
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"caches.open", "cache.put", "localStorage"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("service worker must not persist business data; found %q", forbidden)
		}
	}
}

func TestModulePWAInstallScriptProvidesPersianInstallAction(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/pwa/install.js", nil)
	rec := httptest.NewRecorder()
	(&portalApp{}).modulePWAAsset(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	for _, expected := range []string{"beforeinstallprompt", "نصب این بخش روی دستگاه", "/pwa/sw.js"} {
		if !strings.Contains(rec.Body.String(), expected) {
			t.Fatalf("install helper is missing %q", expected)
		}
	}
}
