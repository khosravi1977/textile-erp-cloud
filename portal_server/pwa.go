package main

import (
	"encoding/json"
	"html"
	"net/http"
	"strings"
)

type modulePWAManifest struct {
	ID              string              `json:"id"`
	Name            string              `json:"name"`
	ShortName       string              `json:"short_name"`
	Description     string              `json:"description"`
	Lang            string              `json:"lang"`
	Dir             string              `json:"dir"`
	StartURL        string              `json:"start_url"`
	Scope           string              `json:"scope"`
	Display         string              `json:"display"`
	BackgroundColor string              `json:"background_color"`
	ThemeColor      string              `json:"theme_color"`
	Categories      []string            `json:"categories"`
	Icons           []modulePWAIcon     `json:"icons"`
	Shortcuts       []modulePWAShortcut `json:"shortcuts"`
}

type modulePWAIcon struct {
	Src     string `json:"src"`
	Sizes   string `json:"sizes"`
	Type    string `json:"type"`
	Purpose string `json:"purpose"`
}

type modulePWAShortcut struct {
	Name      string `json:"name"`
	ShortName string `json:"short_name"`
	URL       string `json:"url"`
}

var modulePWAManifests = map[string]modulePWAManifest{
	"financial": {
		ID:              "/pwa/financial",
		Name:            "بخش مالی نساجی",
		ShortName:       "مالی نساجی",
		Description:     "دسترسی امن حسابدار و کاربران مجاز به بخش مالی نساجی",
		Lang:            "fa",
		Dir:             "rtl",
		StartURL:        "/module-login?module=financial&next=%2Ffinancial%2F",
		Scope:           "/",
		Display:         "standalone",
		BackgroundColor: "#07101f",
		ThemeColor:      "#0f4c5c",
		Categories:      []string{"business", "finance", "productivity"},
		Icons: []modulePWAIcon{
			{Src: "/executive/icon-192.png", Sizes: "192x192", Type: "image/png", Purpose: "any"},
			{Src: "/executive/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "any maskable"},
		},
		Shortcuts: []modulePWAShortcut{{Name: "ورود به بخش مالی", ShortName: "بخش مالی", URL: "/module-login?module=financial&next=%2Ffinancial%2F"}},
	},
	"operational": {
		ID:              "/pwa/operational",
		Name:            "بخش عملیاتی نساجی",
		ShortName:       "عملیات نساجی",
		Description:     "دسترسی امن کاربران مجاز به تولید، انبار و عملیات نساجی",
		Lang:            "fa",
		Dir:             "rtl",
		StartURL:        "/module-login?module=operational&next=%2Foperational%2F",
		Scope:           "/",
		Display:         "standalone",
		BackgroundColor: "#061526",
		ThemeColor:      "#176b5b",
		Categories:      []string{"business", "productivity"},
		Icons: []modulePWAIcon{
			{Src: "/executive/icon-192.png", Sizes: "192x192", Type: "image/png", Purpose: "any"},
			{Src: "/executive/icon-512.png", Sizes: "512x512", Type: "image/png", Purpose: "any maskable"},
		},
		Shortcuts: []modulePWAShortcut{{Name: "ورود به بخش عملیاتی", ShortName: "بخش عملیاتی", URL: "/module-login?module=operational&next=%2Foperational%2F"}},
	},
}

const modulePWAServiceWorker = `self.addEventListener("install", () => self.skipWaiting());
self.addEventListener("activate", (event) => event.waitUntil(self.clients.claim()));
// اطلاعات مالی و عملیاتی عمداً در حافظهٔ آفلاین ذخیره نمی‌شوند.
self.addEventListener("fetch", () => {});
`

const modulePWAInstallScript = `(function () {
  "use strict";
  var promptEvent = null;
  var standalone = window.matchMedia("(display-mode: standalone)").matches || window.navigator.standalone === true;
  if (standalone) return;

  function createButton() {
    if (document.getElementById("viora-pwa-install")) return;
    var button = document.createElement("button");
    button.id = "viora-pwa-install";
    button.type = "button";
    button.textContent = "نصب این بخش روی دستگاه";
    button.setAttribute("aria-label", "نصب میانبر امن این بخش روی دستگاه");
    button.style.cssText = "position:fixed;left:18px;bottom:18px;z-index:2147483647;border:1px solid #93c5fd;border-radius:999px;padding:11px 16px;background:#1d4ed8;color:#fff;font:700 13px Tahoma,Arial;box-shadow:0 8px 28px #0006;cursor:pointer";
    button.addEventListener("click", async function () {
      if (promptEvent) {
        promptEvent.prompt();
        await promptEvent.userChoice;
        promptEvent = null;
        return;
      }
      window.alert("از منوی مرورگر، گزینه «نصب برنامه» یا «افزودن به صفحه اصلی» را انتخاب کنید. در آیفون ابتدا دکمه اشتراک‌گذاری و سپس «افزودن به صفحه اصلی» را بزنید.");
    });
    document.body.appendChild(button);
  }

  window.addEventListener("beforeinstallprompt", function (event) {
    event.preventDefault();
    promptEvent = event;
    createButton();
  });
  window.addEventListener("appinstalled", function () {
    var button = document.getElementById("viora-pwa-install");
    if (button) button.remove();
  });
  if (document.readyState === "loading") document.addEventListener("DOMContentLoaded", createButton);
  else createButton();

  if ("serviceWorker" in navigator) {
    window.addEventListener("load", function () {
      navigator.serviceWorker.register("/pwa/sw.js", {scope: "/"}).catch(function () {});
    });
  }
})();
`

func (a *portalApp) modulePWAAsset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "same-origin")
	w.Header().Set("Cache-Control", "no-cache")

	switch r.URL.Path {
	case "/pwa/sw.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		w.Header().Set("Service-Worker-Allowed", "/")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(modulePWAServiceWorker))
		}
		return
	case "/pwa/install.js":
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(modulePWAInstallScript))
		}
		return
	}

	const suffix = "/manifest.webmanifest"
	if !strings.HasPrefix(r.URL.Path, "/pwa/") || !strings.HasSuffix(r.URL.Path, suffix) {
		http.NotFound(w, r)
		return
	}
	module := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/pwa/"), suffix)
	module = strings.Trim(module, "/")
	manifest, ok := modulePWAManifests[module]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json; charset=utf-8")
	if r.Method == http.MethodHead {
		return
	}
	encoder := json.NewEncoder(w)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(manifest)
}

func modulePWAHead(module string) string {
	manifest, ok := modulePWAManifests[module]
	if !ok {
		return ""
	}
	return `<meta name="theme-color" content="` + html.EscapeString(manifest.ThemeColor) + `">` +
		`<meta name="apple-mobile-web-app-capable" content="yes">` +
		`<meta name="apple-mobile-web-app-status-bar-style" content="black-translucent">` +
		`<meta name="apple-mobile-web-app-title" content="` + html.EscapeString(manifest.ShortName) + `">` +
		`<link rel="manifest" href="/pwa/` + html.EscapeString(module) + `/manifest.webmanifest">` +
		`<link rel="apple-touch-icon" href="/executive/icon-192.png">` +
		`<script defer src="/pwa/install.js"></script>`
}
