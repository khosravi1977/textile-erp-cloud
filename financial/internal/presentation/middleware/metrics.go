package middleware

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type requestMetric struct {
	Count           uint64
	TotalDurationMS uint64
}

var (
	metricStartedAt = time.Now()
	metricMu        sync.RWMutex
	metricRequests  = make(map[string]*requestMetric)
)

// Metrics records lightweight HTTP request counters in Prometheus text format.
func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		recordMetric(r.Method, routeLabel(r.URL.Path), time.Since(start))
	})
}

func MetricsHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	fmt.Fprintln(w, "# HELP textile_erp_uptime_seconds Service uptime in seconds.")
	fmt.Fprintln(w, "# TYPE textile_erp_uptime_seconds gauge")
	fmt.Fprintf(w, "textile_erp_uptime_seconds %.0f\n", time.Since(metricStartedAt).Seconds())
	fmt.Fprintln(w, "# HELP textile_erp_http_requests_total Total HTTP requests.")
	fmt.Fprintln(w, "# TYPE textile_erp_http_requests_total counter")
	fmt.Fprintln(w, "# HELP textile_erp_http_request_duration_ms_total Total HTTP request duration in milliseconds.")
	fmt.Fprintln(w, "# TYPE textile_erp_http_request_duration_ms_total counter")

	metricMu.RLock()
	keys := make([]string, 0, len(metricRequests))
	for key := range metricRequests {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		metric := metricRequests[key]
		parts := strings.SplitN(key, " ", 2)
		method, route := parts[0], parts[1]
		fmt.Fprintf(w, "textile_erp_http_requests_total{method=%q,route=%q} %d\n", method, route, atomic.LoadUint64(&metric.Count))
		fmt.Fprintf(w, "textile_erp_http_request_duration_ms_total{method=%q,route=%q} %d\n", method, route, atomic.LoadUint64(&metric.TotalDurationMS))
	}
	metricMu.RUnlock()
}

func recordMetric(method, route string, duration time.Duration) {
	key := method + " " + route
	metricMu.RLock()
	metric := metricRequests[key]
	metricMu.RUnlock()
	if metric == nil {
		metricMu.Lock()
		metric = metricRequests[key]
		if metric == nil {
			metric = &requestMetric{}
			metricRequests[key] = metric
		}
		metricMu.Unlock()
	}
	atomic.AddUint64(&metric.Count, 1)
	atomic.AddUint64(&metric.TotalDurationMS, uint64(duration.Milliseconds()))
}

func routeLabel(path string) string {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for i, part := range parts {
		if _, err := fmt.Sscanf(part, "%d", new(int64)); err == nil {
			parts[i] = ":id"
		}
	}
	if len(parts) == 1 && parts[0] == "" {
		return "/"
	}
	return "/" + strings.Join(parts, "/")
}
