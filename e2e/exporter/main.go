// Command exporter emits a controlled classic Prometheus histogram in old,
// dual-write, or new mode. It exists solely for the isolated TMR E2E harness.
package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
)

type mode string

const (
	modeOld  mode = "old"
	modeDual mode = "dual"
	modeNew  mode = "new"
)

var scrapes atomic.Uint64

func main() {
	selected, err := parseMode(os.Getenv("TMR_EXPORT_MODE"))
	if err != nil {
		log.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusOK)
		_, _ = writer.Write([]byte("ok\n"))
	})
	mux.HandleFunc("/metrics", func(writer http.ResponseWriter, _ *http.Request) {
		writer.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = writer.Write([]byte(render(selected, scrapes.Add(1))))
	})
	log.Printf("TMR test exporter listening on :8080 in %s mode", selected)
	log.Fatal(http.ListenAndServe(":8080", mux))
}

func parseMode(value string) (mode, error) {
	switch mode(strings.ToLower(strings.TrimSpace(value))) {
	case "", modeDual:
		return modeDual, nil
	case modeOld:
		return modeOld, nil
	case modeNew:
		return modeNew, nil
	default:
		return "", fmt.Errorf("TMR_EXPORT_MODE must be old, dual, or new")
	}
}

func render(selected mode, scrape uint64) string {
	var output strings.Builder
	if selected == modeOld || selected == modeDual {
		writeHistogram(&output, "checkout_request_duration_seconds", "http_method", scrape)
	}
	if selected == modeNew || selected == modeDual {
		writeHistogram(&output, "checkout_server_request_duration_seconds", "http_request_method", scrape)
	}
	return output.String()
}

func writeHistogram(output *strings.Builder, metric, methodLabel string, scrape uint64) {
	fmt.Fprintf(output, "# HELP %s Controlled checkout request latency.\n", metric)
	fmt.Fprintf(output, "# TYPE %s histogram\n", metric)
	writeStatusHistogram(output, metric, methodLabel, "200", scrape*100, float64(scrape)*24)
	writeStatusHistogram(output, metric, methodLabel, "500", scrape*2, float64(scrape)*1.2)
}

func writeStatusHistogram(output *strings.Builder, metric, methodLabel, status string, count uint64, sum float64) {
	buckets := []struct {
		le    string
		count uint64
	}{
		{le: "0.1", count: count * 3 / 10},
		{le: "0.5", count: count * 8 / 10},
		{le: "1", count: count * 95 / 100},
		{le: "+Inf", count: count},
	}
	for _, bucket := range buckets {
		fmt.Fprintf(output, "%s_bucket{%s=\"GET\",status=\"%s\",le=\"%s\"} %d\n", metric, methodLabel, status, bucket.le, bucket.count)
	}
	fmt.Fprintf(output, "%s_sum{%s=\"GET\",status=\"%s\"} %.3f\n", metric, methodLabel, status, sum)
	fmt.Fprintf(output, "%s_count{%s=\"GET\",status=\"%s\"} %d\n", metric, methodLabel, status, count)
}
