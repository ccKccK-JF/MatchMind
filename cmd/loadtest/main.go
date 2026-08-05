package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"sort"
	"sync"
	"sync/atomic"
	"time"
)

type configuration struct {
	baseURL     string
	rate        int
	duration    time.Duration
	maxTickets  int
	concurrency int
}

type recorder struct {
	mu        sync.Mutex
	latencies []time.Duration
	attempted atomic.Int64
	success   atomic.Int64
	failed    atomic.Int64
}

type summary struct {
	DurationSeconds float64 `json:"duration_seconds"`
	Attempted       int64   `json:"attempted"`
	Successful      int64   `json:"successful"`
	Failed          int64   `json:"failed"`
	Throughput      float64 `json:"tickets_per_second"`
	P95Milliseconds float64 `json:"p95_ms"`
	P99Milliseconds float64 `json:"p99_ms"`
	GeneratorHeapMB float64 `json:"generator_heap_mb"`
	GeneratorGo     int     `json:"generator_goroutines"`
}

func main() {
	config := parseFlags()
	if err := run(config); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func parseFlags() configuration {
	var config configuration
	flag.StringVar(&config.baseURL, "base-url", "http://localhost:8080", "MatchMind public API base URL")
	flag.IntVar(&config.rate, "rate", 500, "target Ticket creations per second")
	flag.DurationVar(&config.duration, "duration", 10*time.Minute, "test duration")
	flag.IntVar(&config.maxTickets, "max-tickets", 100000, "maximum Ticket attempts; zero means duration-only")
	flag.IntVar(&config.concurrency, "concurrency", 256, "maximum in-flight player and Ticket pairs")
	flag.Parse()
	return config
}

func run(config configuration) error {
	if config.rate <= 0 || config.duration <= 0 || config.concurrency <= 0 || config.maxTickets < 0 {
		return fmt.Errorf("rate, duration, and concurrency must be positive; max-tickets cannot be negative")
	}
	client := &http.Client{Timeout: 5 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), config.duration)
	defer cancel()

	startedAt := time.Now()
	interval := time.Second / time.Duration(config.rate)
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	semaphore := make(chan struct{}, config.concurrency)
	var waitGroup sync.WaitGroup
	var sequence atomic.Int64
	results := &recorder{}

schedule:
	for {
		select {
		case <-ctx.Done():
			break schedule
		case <-ticker.C:
			index := int(sequence.Add(1) - 1)
			if config.maxTickets > 0 && index >= config.maxTickets {
				break schedule
			}
			semaphore <- struct{}{}
			waitGroup.Add(1)
			go func() {
				defer waitGroup.Done()
				defer func() { <-semaphore }()
				createPair(ctx, client, config.baseURL, index, results)
			}()
		}
	}
	waitGroup.Wait()
	elapsed := time.Since(startedAt)

	result := results.summarize(elapsed)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(result)
}

func createPair(ctx context.Context, client *http.Client, baseURL string, index int, results *recorder) {
	results.attempted.Add(1)
	playerID := fmt.Sprintf("load-player-%08d", index)
	roles := []string{"vanguard", "roamer", "core", "ranged", "support"}
	role := roles[index%len(roles)]
	player := map[string]any{
		"id": playerID, "name": playerID, "initial_rating": 1500 + index%20,
		"preferred_roles": []string{role}, "home_region": "hongkong",
		"region_latency": map[string]int{"hongkong": 30}, "behavior_score": 95,
	}
	if err := postJSON(ctx, client, baseURL+"/api/v1/players", player, nil); err != nil {
		results.failed.Add(1)
		return
	}
	ticket := map[string]any{
		"player_id": playerID, "mode": "ranked_5v5", "client_version": "load-1",
		"preferred_roles": []string{role}, "region_latency": map[string]int{"hongkong": 30},
	}
	startedAt := time.Now()
	err := postJSON(ctx, client, baseURL+"/api/v1/tickets", ticket, map[string]string{
		"Idempotency-Key": fmt.Sprintf("load-create-%08d", index),
	})
	results.record(time.Since(startedAt), err)
}

func postJSON(ctx context.Context, client *http.Client, url string, body any, headers map[string]string) error {
	data, err := json.Marshal(body)
	if err != nil {
		return err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("POST %s returned %s", url, response.Status)
	}
	return nil
}

func (r *recorder) record(latency time.Duration, err error) {
	if err != nil {
		r.failed.Add(1)
		return
	}
	r.success.Add(1)
	r.mu.Lock()
	r.latencies = append(r.latencies, latency)
	r.mu.Unlock()
}

func (r *recorder) summarize(elapsed time.Duration) summary {
	r.mu.Lock()
	latencies := append([]time.Duration(nil), r.latencies...)
	r.mu.Unlock()
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	successful := r.success.Load()
	return summary{
		DurationSeconds: elapsed.Seconds(),
		Attempted:       r.attempted.Load(),
		Successful:      successful,
		Failed:          r.failed.Load(),
		Throughput:      float64(successful) / elapsed.Seconds(),
		P95Milliseconds: percentileMilliseconds(latencies, 0.95),
		P99Milliseconds: percentileMilliseconds(latencies, 0.99),
		GeneratorHeapMB: float64(memory.HeapAlloc) / (1024 * 1024),
		GeneratorGo:     runtime.NumGoroutine(),
	}
}

func percentileMilliseconds(values []time.Duration, percentile float64) float64 {
	if len(values) == 0 {
		return 0
	}
	index := int(float64(len(values)-1) * percentile)
	return float64(values[index]) / float64(time.Millisecond)
}
