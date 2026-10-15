package main

import (
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/birdtie/birdtie/apps/api/internal/agentcognitive"
	"github.com/birdtie/birdtie/apps/api/internal/agentfeature"
	"github.com/birdtie/birdtie/apps/api/internal/agenttool"
	"github.com/birdtie/birdtie/apps/api/internal/devauth"
	"github.com/birdtie/birdtie/apps/api/internal/httpapi"
	lr "github.com/birdtie/birdtie/apps/api/internal/locationreference"
	"github.com/birdtie/birdtie/apps/api/internal/media"
	"github.com/birdtie/birdtie/apps/api/internal/momentmedia"
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
	maintenanceEnabled, err := loadBackgroundMaintenance(os.LookupEnv)
	if err != nil {
		log.Fatal("后台维护配置无效")
	}
	poolConfig, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		log.Fatal("configure database: invalid BIRDTIE_DATABASE_URL")
	}
	expectedDatabase, err := loadNativeDatabaseName(os.LookupEnv, poolConfig.ConnConfig.Database)
	if err != nil {
		log.Fatal("原生数据库目标配置无效")
	}
	// Domain queries use the native public schema, never role-specific shadows.
	if poolConfig.ConnConfig.RuntimeParams == nil {
		poolConfig.ConnConfig.RuntimeParams = map[string]string{}
	}
	poolConfig.ConnConfig.RuntimeParams["search_path"] = "public"

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	connectCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	pool, err := pgxpool.NewWithConfig(connectCtx, poolConfig)
	if err != nil {
		log.Fatal("configure database: invalid BIRDTIE_DATABASE_URL")
	}
	defer pool.Close()
	readyDatabase, err := newNativeDatabasePinger(connectCtx, pool, expectedDatabase)
	if err != nil {
		log.Fatal("原生数据库身份或必要结构不可用")
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
	publicTestOrigin := os.Getenv("BIRDTIE_DEV_PHONE_PUBLIC_TEST_ORIGIN")
	if err := devauth.ValidateTestChannel(devauth.TestChannel{
		Enabled: devPhoneEnabled, APIAddress: address, DatabaseHost: pool.Config().ConnConfig.Host,
		AllowedOrigins: allowedOrigins, PublicTestOrigin: publicTestOrigin,
	}); err != nil {
		log.Fatal("固定码测试登录通道配置无效")
	}
	if publicTestOrigin != "" {
		log.Print("固定码公网测试登录已启用；未进行短信或生产手机号所有权验证")
	}
	store := postgres.New(pool, devPhoneEnabled)
	if endpoint := os.Getenv("BIRDTIE_LOCATION_SEARCH_URL"); endpoint != "" {
		provider, err := lr.NewPelias(endpoint)
		if err != nil {
			log.Fatal("location provider configuration invalid")
		}
		store.ConfigureLocationSearch(provider)
	}
	publicVideoSetting := strings.TrimSpace(os.Getenv("BIRDTIE_PUBLIC_MOMENT_VIDEO_ENABLED"))
	publicVideoEnabled := publicVideoSetting == "1" || strings.EqualFold(publicVideoSetting, "true")
	if publicVideoSetting != "" && publicVideoSetting != "0" && !strings.EqualFold(publicVideoSetting, "false") && !publicVideoEnabled {
		log.Fatal("public Moment video configuration invalid")
	}
	if publicVideoEnabled {
		codec := &momentmedia.FFmpegVideoNormalizer{
			FFmpegPath:  os.Getenv("BIRDTIE_PUBLIC_MOMENT_VIDEO_FFMPEG_PATH"),
			FFprobePath: os.Getenv("BIRDTIE_PUBLIC_MOMENT_VIDEO_FFPROBE_PATH"),
			PrlimitPath: os.Getenv("BIRDTIE_PUBLIC_MOMENT_VIDEO_PRLIMIT_PATH"),
			TempRoot:    os.Getenv("BIRDTIE_PUBLIC_MOMENT_VIDEO_TEMP_ROOT"),
		}
		if err := store.ConfigureMomentMediaVideo(codec); err != nil {
			log.Fatal("public Moment video runtime unavailable")
		}
	}

	videoRuntime, err := media.VideoRuntimeFromEnvironment(os.Getenv)
	if err != nil {
		log.Fatal("视频材料配置不完整或未通过可信边界检查")
	}
	videoMaterials := media.NewVideoMaterialService(store, videoRuntime)
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
	activityJoin, err := loadAgentActivityJoinStartup(os.LookupEnv, store, featureController)
	if err != nil {
		log.Fatal("智能体活动报名配置无效")
	}
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
	var openPlaces httpapi.OpenPlaceAcquirer
	systemPush, err := loadSystemPushStartup(store, os.LookupEnv)
	if err != nil {
		log.Fatal("系统通知配置不完整或未通过可信边界检查")
	}
	agentVisual, err := loadAgentVisualStartup(os.Getenv("BIRDTIE_AGENT_VISUAL_CONFIG"), address, devPhoneEnabled, store, featureController)
	if err != nil {
		log.Fatal("图片分析配置不完整或未通过可信边界检查")
	}
	if os.Getenv("BIRDTIE_OPEN_PLACES_ENABLED") == "1" {
		openPlaces, err = agenttool.NewOpenStreetMapPlaces(nil)
		if err != nil {
			log.Fatal("开放地点来源配置无效")
		}
	}
	baseHandler := httpapi.New(store, store, store, store, store, store, store, store, store, store, store, store, devPhoneEnabled, oidc, readyDatabase, allowedOrigins, httpapi.WithMemoryCandidates(candidates), httpapi.WithCandidatePipeline(candidatePipeline), httpapi.WithMultiCandidates(multiCandidates), httpapi.WithHumanActiveIntents(humanActiveIntents), httpapi.WithHumanIntentConversions(humanIntentConversions), httpapi.WithAgentRuns(agentRuns), httpapi.WithAgentActivityJoin(activityJoin), httpapi.WithNowContextSelection(nowContextSelection), httpapi.WithSandboxRecovery(sandboxRecovery), httpapi.WithNowLiveAnswers(liveAnswers), httpapi.WithOpenPlaceAcquirer(openPlaces), httpapi.WithPublicOnlinePlaceCache(store), httpapi.WithVideoMaterials(videoMaterials), httpapi.WithAgentVisualGateway(agentVisual), httpapi.WithSystemPush(systemPush))
	handler, err := newAgentFeatureBoundaryWithController(featureController, store, baseHandler)
	if err != nil {
		log.Fatal("Agent 功能配置无效或边界不可用")
	}
	handler = withNowLiveDeadline(liveAnswers, store, handler)
	// Image requests include bounded base64/raw bodies. Permit upload time while
	// keeping domain/model-specific deadlines and exact media limits unchanged.
	writeTimeout := 65 * time.Second
	if os.Getenv("BIRDTIE_AGENT_VISUAL_CONFIG") != "" {
		writeTimeout = 125 * time.Second
	}
	server := &http.Server{
		Addr:              address,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       60 * time.Second,
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

	// Only start maintenance after every startup dependency passed validation.
	startBackgroundMaintenance(ctx, maintenanceEnabled, store)
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
