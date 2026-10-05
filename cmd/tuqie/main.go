package main

import (
	"context"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"tuqie/internal/server"
	"tuqie/internal/store"
	"tuqie/web"
)

func main() {
	addr := flag.String("addr", ":7423", "listen address")
	dataDir := flag.String("data", "", "directory for uploads (default: $TMPDIR/tuqie)")
	ttl := flag.Duration("ttl", 60*time.Minute, "how long uploads are kept")
	flag.Parse()

	if err := run(*addr, *dataDir, *ttl); err != nil {
		log.Fatal(err)
	}
}

func run(addr, dataDir string, ttl time.Duration) error {
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
		Handler:           server.New(st, dist).Handler(),
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
