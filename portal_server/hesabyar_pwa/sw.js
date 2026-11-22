"use strict";
const root = self.registration.scope;
const prefix = "hesabyar-pilot:";
const key = prefix + root + ":6";
const assets = ["./", "./index.html", "./app.js", "./style.css", "./manifest.webmanifest", "./icon-192.png", "./icon-512.png"];
const allowed = new Set(assets.map(asset => new URL(asset, root).href));
self.addEventListener("install", event => {
  event.waitUntil(caches.open(key).then(cache => cache.addAll(assets)).then(() => self.skipWaiting()));
});
self.addEventListener("activate", event => {
  event.waitUntil(caches.keys().then(keys => Promise.all(keys.filter(old =>
    old.startsWith(prefix + root + ":") && old !== key
  ).map(old => caches.delete(old)))).then(() => self.clients.claim()));
});
self.addEventListener("fetch", event => {
  if (event.request.method !== "GET" || !allowed.has(event.request.url)) return;
  event.respondWith(fetch(event.request).catch(() => caches.open(key).then(cache => cache.match(event.request))));
});
