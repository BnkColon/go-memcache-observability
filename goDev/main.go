package main

import (
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"bnkmem.com/goDev/cache"
	"bnkmem.com/goDev/observability"
	"bnkmem.com/goDev/store"
)

func main() {
	ctx, cancel := observability.WithStartupContext()
	defer cancel()

	shutdownTracer, err := observability.InitTracer(ctx, "file-memcache-service")
	if err != nil {
		log.Fatalf("failed to init tracer: %v", err)
	}
	defer func() {
		if err := shutdownTracer(ctx); err != nil {
			log.Printf("error shutting down tracer: %v", err)
		}
	}()

	// Config
	listenAddr := ":8080"
	memcacheAddr := "127.0.0.1:11211"
	maxChunkBytes := int64(1 * 1024 * 1024)  // 1 MB
	maxFileBytes := int64(50 * 1024 * 1024)  // 50 MB
	chunkTTL := time.Hour 			// default 1h expiration
	if v := os.Getenv("CHUNK_TTL_SECONDS"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			chunkTTL = time.Duration(secs) * time.Second
		}
	}

	// Set up cache
	c := cache.NewCache(memcacheAddr)
	if err := c.Ping(); err != nil {
		log.Fatalf("memcached not reachable at %s: %v", memcacheAddr, err)
	}

	// Set up server
	srv := store.NewServer(c, maxChunkBytes, maxFileBytes, chunkTTL)

	mux := http.NewServeMux()

	// Fallback handler logs unmatched routes 
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("DEBUG: fallback handler hit: %s %s", r.Method, r.URL.Path)
		http.NotFound(w, r)
	})

	// POST /api/v1/store → StoreHandler
	mux.HandleFunc("/api/v1/store", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("StoreHandler: %s %s", r.Method, r.URL.Path)
		srv.StoreHandler(w, r)
	})

	// GET /api/v1/store/:key → RetrieveHandler
	mux.HandleFunc("/api/v1/store/", func(w http.ResponseWriter, r *http.Request) {
		log.Printf("RetrieveHandler: %s %s", r.Method, r.URL.Path)
		srv.RetrieveHandler(w, r)
	})

	// Wrap mux with observability middleware
	handler := observability.HTTPMiddleware(mux)

	log.Printf("listening on %s (memcached: %s)", listenAddr, memcacheAddr)
	if err := http.ListenAndServe(listenAddr, handler); err != nil {
		log.Fatal(err)
	}
}
