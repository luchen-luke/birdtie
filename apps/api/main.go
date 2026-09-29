package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	databaseURL := os.Getenv("BIRDTIE_DATABASE_URL")
	if databaseURL == "" {
		log.Fatal("BIRDTIE_DATABASE_URL is required")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.New(connectCtx, databaseURL)
	if err != nil {
		log.Fatal("configure database: invalid BIRDTIE_DATABASE_URL")
	}
	defer pool.Close()
	if err := pool.Ping(connectCtx); err != nil {
		log.Fatal("connect database: unavailable")
	}

	address := os.Getenv("BIRDTIE_API_ADDR")
	if address == "" {
		address = "127.0.0.1:8080"
	}
	allowedOrigins := strings.Split(os.Getenv("BIRDTIE_ALLOWED_ORIGINS"), ",")
	devSetting := strings.TrimSpace(os.Getenv("BIRDTIE_DEV_PHONE_AUTH"))
	devPhoneEnabled := devSetting == "1" || strings.EqualFold(devSetting, "true")
	if devSetting != "" && devSetting != "0" && !strings.EqualFold(devSetting, "false") && !devPhoneEnabled {
		log.Fatal("BIRDTIE_DEV_PHONE_AUTH must be true or false")
	}
	if devPhoneEnabled {
		host, _, err := net.SplitHostPort(address)
		if err != nil || !isLoopbackHost(host) ||
			!isLoopbackHost(pool.Config().ConnConfig.Host) {
			log.Fatal("development phone auth requires loopback API and database hosts")
		}
		for _, origin := range allowedOrigins {
			origin = strings.TrimSpace(origin)
			if origin == "" {
				continue
			}
			parsed, err := url.Parse(origin)
			if err != nil || !isLoopbackHost(parsed.Hostname()) ||
				(parsed.Scheme != "http" && parsed.Scheme != "https") {
				log.Fatal("development phone auth requires loopback browser origins")
			}
		}
	}
	store := postgres.New(pool, devPhoneEnabled)
	var oidc *oidcauth.Service
	if os.Getenv("BIRDTIE_OIDC_ISSUER") != "" ||
		os.Getenv("BIRDTIE_OIDC_CLIENT_ID") != "" ||
		os.Getenv("BIRDTIE_OIDC_CLIENT_SECRET") != "" ||
		os.Getenv("BIRDTIE_OIDC_REDIRECT_URI") != "" ||
		os.Getenv("BIRDTIE_OIDC_CLIENT_REDIRECT") != "" {
		oidcCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		oidc, err = oidcauth.New(oidcCtx, oidcauth.Config{
			Issuer:         os.Getenv("BIRDTIE_OIDC_ISSUER"),
			ClientID:       os.Getenv("BIRDTIE_OIDC_CLIENT_ID"),
			ClientSecret:   os.Getenv("BIRDTIE_OIDC_CLIENT_SECRET"),
			RedirectURI:    os.Getenv("BIRDTIE_OIDC_REDIRECT_URI"),
			ClientRedirect: os.Getenv("BIRDTIE_OIDC_CLIENT_REDIRECT"),
		}, store)
		if err != nil {
			log.Fatalf("configure OIDC: %v", err)
		}
	}
	server := &http.Server{
		Addr:              address,
		Handler:           httpapi.New(store, store, store, store, store, store, store, store, store, devPhoneEnabled, oidc, pool, allowedOrigins),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown server: %v", err)
		}
	}()

	log.Printf("Birdtie API listening on %s", address)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
