package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"tuqie/internal/server"
	"tuqie/internal/store"
	"tuqie/web"
)

func main() {
	addr := flag.String("addr", envOr("addr", ":7423"), "listen address (TUQIE_ADDR)")
	dataDir := flag.String("data", envOr("data", ""), "directory for uploads (TUQIE_DATA, default: $TMPDIR/tuqie)")
	ttl := flag.String("ttl", envOr("ttl", "60m"), "how long uploads are kept (TUQIE_TTL)")
	password := flag.String("password", envOr("password", ""), "ask every request but the health check for this password (TUQIE_PASSWORD; basic auth, any username); empty leaves the tool open")
	uploads := flag.String("uploads-per-minute", envOr("uploads-per-minute", "30"), "uploads one client may post per minute (TUQIE_UPLOADS_PER_MINUTE), 0 for no limit")
	maxUpload := flag.String("max-upload", envOr("max-upload", defaultMaxUpload), "ceiling on one upload (TUQIE_MAX_UPLOAD): bytes or a size like 50MB, 0 for no ceiling")
	showVersion := flag.Bool("version", false, "print version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("tuqie", version)
		return
	}

	// Read after Parse: a variable nobody overrode should not be able to stop the
	// tool from starting.
	keep, err := time.ParseDuration(*ttl)
	if err != nil {
		log.Fatalf("-ttl: %v", err)
	}
	perMinute, err := strconv.Atoi(*uploads)
	if err != nil {
		log.Fatalf("-uploads-per-minute: %v", err)
	}
	ceiling, err := parseSize(*maxUpload)
	if err != nil {
		log.Fatalf("-max-upload: %v", err)
	}

	cfg := server.Config{Password: *password, UploadsPerMinute: perMinute, MaxUpload: ceiling}
	if err := run(*addr, *dataDir, keep, cfg); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dataDir string, ttl time.Duration, cfg server.Config) error {
	st, err := store.New(dataDir, ttl)
	if err != nil {
		return err
	}
	defer st.Close()

	dist, err := web.Dist()
	if err != nil {
		return err
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           server.New(st, dist, cfg).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      10 * time.Minute,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errCh := make(chan error, 1)
	go func() {
		shown := addr
		if strings.HasPrefix(shown, ":") {
			shown = "localhost" + shown
		}
		log.Printf("tuqie listening on http://%s", shown)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutdownCtx)
	}
}
