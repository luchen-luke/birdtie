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

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	"github.com/birdtie/birdtie/apps/api/internal/oidcauth"
	"github.com/birdtie/birdtie/apps/api/internal/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	featureConfig, err := agentfeature.LoadConfig(os.LookupEnv)
	if err != nil {
		log.Fatal("Agent 功能配置无效或边界不可用")
	}
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
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()
		for {
			runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			started := time.Now()
			count, err := store.EnqueueStartsSoonReminders(runCtx)
			if err != nil && ctx.Err() == nil {
				log.Printf("activity_reminder_run status=failed source=api stage=enqueue duration_ms=%d error=%q", time.Since(started).Milliseconds(), err)
			} else if err == nil {
				log.Printf("activity_reminder_run status=ok source=api inserted=%d duration_ms=%d", count, time.Since(started).Milliseconds())
			}
			cancel()
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
			}
		}
	}()
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
	featureController, err := agentfeature.NewController(featureConfig)
	if err != nil {
		log.Fatal("Agent 功能配置无效或边界不可用")
	}
	candidates := postgres.NewMemoryCandidateHumanGateway(store, featureController)
	candidatePipeline := postgres.NewCandidatePipeline(store, featureController)
	multiCandidates := postgres.NewMultiCandidatePipeline(store, featureController)
	agentRuns := postgres.NewAgentRuns(store, featureController)
	sandboxRecovery := postgres.NewHumanSandboxRecovery(store, featureController)
	humanActiveIntents, err := postgres.NewHumanActiveIntents(store)
	if err != nil {
		log.Fatal("当前意图管理边界初始化失败")
	}
	humanIntentConversions, err := postgres.NewHumanIntentConversions(store)
	if err != nil {
		log.Fatal("意图转活动边界初始化失败")
	}
	nowContextSelection, err := postgres.NewNowContextSelection(store)
	if err != nil {
		log.Fatal("情境选择边界初始化失败")
	}
	liveAnswers, err := loadNowLiveStartup(os.Getenv("BIRDTIE_NOW_LIVE_CONFIG"), address, devPhoneEnabled, store, featureController)
	if err != nil {
		log.Fatal("Now 真实检索与回答配置无效")
	}
	baseHandler := httpapi.New(store, store, store, store, store, store, store, store, store, store, store, store, devPhoneEnabled, oidc, pool, allowedOrigins, httpapi.WithMemoryCandidates(candidates), httpapi.WithCandidatePipeline(candidatePipeline), httpapi.WithMultiCandidates(multiCandidates), httpapi.WithHumanActiveIntents(humanActiveIntents), httpapi.WithHumanIntentConversions(humanIntentConversions), httpapi.WithAgentRuns(agentRuns), httpapi.WithNowContextSelection(nowContextSelection), httpapi.WithSandboxRecovery(sandboxRecovery), httpapi.WithNowLiveAnswers(liveAnswers))
	handler, err := newAgentFeatureBoundaryWithController(featureController, store, baseHandler)
	if err != nil {
		log.Fatal("Agent 功能配置无效或边界不可用")
	}
	handler = withNowLiveDeadline(liveAnswers, store, handler)
	writeTimeout := 15 * time.Second
	if liveAnswers != nil {
		writeTimeout = 35 * time.Second
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      writeTimeout,
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

// This composes the configured server adapter into the real request handler;
// it creates no enrichment/model HTTP route and changes no direct human ACL.
func newAgentFeatureBoundary(config agentfeature.Config, store agentcognitive.CurrentDomainStore, next http.Handler) (http.Handler, error) {
	controller, err := agentfeature.NewController(config)
	if err != nil {
		return nil, err
	}
	return newAgentFeatureBoundaryWithController(controller, store, next)
}

// Cognitive, manual-candidate and purpose-bound candidate gates share the same
// configured controller. HTTP input cannot replace that startup dependency.
func newAgentFeatureBoundaryWithController(controller *agentfeature.Controller, store agentcognitive.CurrentDomainStore, next http.Handler) (http.Handler, error) {
	domains, err := agentcognitive.NewFeatureGatedDomains(controller, store)
	if err != nil {
		return nil, err
	}
	return agentcognitive.FeatureBoundary(next, domains), nil
}

func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
