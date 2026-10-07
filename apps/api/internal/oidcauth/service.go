package oidcauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/birdtie/birdtie/apps/api/internal/identity"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

var ErrInvalidFlow = errors.New("invalid or expired OIDC flow")

const codePrefix = "btc1_"

var verifierPattern = regexp.MustCompile(`^[A-Za-z0-9._~-]{43,128}$`)

type Config struct {
	Issuer         string
	ClientID       string
	ClientSecret   string
	RedirectURI    string
	ClientRedirect string
}

type Flow struct {
	Nonce            string
	ProviderVerifier string
	ClientChallenge  string
}

type Store interface {
	SaveOIDCFlow(context.Context, [32]byte, Flow) error
	TakeOIDCFlow(context.Context, [32]byte) (Flow, error)
	SaveOIDCExchangeCode(context.Context, string, string, string, [32]byte, string) error
	RedeemOIDCExchangeCode(context.Context, [32]byte, string, [32]byte) error
}

type Service struct {
	config         oauth2.Config
	verifier       *oidc.IDTokenVerifier
	httpClient     *http.Client
	clientID       string
	clientRedirect string
	store          Store
}

func New(ctx context.Context, cfg Config, store Store) (*Service, error) {
	if cfg.ClientID == "" || cfg.Issuer == "" {
		return nil, errors.New("OIDC issuer and client ID are required")
	}
	issuer, err := url.Parse(cfg.Issuer)
	if err != nil || issuer.Scheme != "https" || issuer.Host == "" {
		return nil, errors.New("OIDC issuer must be HTTPS")
	}
	callback, err := url.Parse(cfg.RedirectURI)
	if err != nil || !validApplicationURL(callback) ||
		callback.Path != "/v1/auth/oidc/callback" ||
		callback.RawQuery != "" || callback.Fragment != "" {
		return nil, errors.New("invalid OIDC callback URL")
	}
	client, err := url.Parse(cfg.ClientRedirect)
	if err != nil || !validClientRedirect(client) ||
		client.RawQuery != "" || client.Fragment != "" {
		return nil, errors.New("invalid OIDC client redirect URL")
	}
	secureClient := &http.Client{
		Timeout: 10 * time.Second,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error {
			return errors.New("OIDC endpoint redirects are not allowed")
		},
	}
	provider, err := oidc.NewProvider(oidc.ClientContext(ctx, secureClient), cfg.Issuer)
	if err != nil {
		return nil, errors.New("OIDC discovery unavailable")
	}
	endpoint := provider.Endpoint()
	var metadata struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := provider.Claims(&metadata); err != nil {
		return nil, errors.New("invalid OIDC discovery document")
	}
	jwksURL, jwksErr := url.Parse(metadata.JWKSURI)
	authURL, authErr := url.Parse(endpoint.AuthURL)
	tokenURL, tokenErr := url.Parse(endpoint.TokenURL)
	if authErr != nil || tokenErr != nil || jwksErr != nil ||
		jwksURL.Scheme != "https" || jwksURL.Host == "" ||
		authURL.Scheme != "https" || authURL.Host == "" ||
		tokenURL.Scheme != "https" || tokenURL.Host == "" {
		return nil, errors.New("OIDC endpoints must be HTTPS")
	}
	return &Service{
		config: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURI,
			Endpoint:     endpoint,
			Scopes:       []string{oidc.ScopeOpenID, "profile"},
		},
		verifier: provider.VerifierContext(
			oidc.ClientContext(context.Background(), secureClient),
			&oidc.Config{ClientID: cfg.ClientID}),
		httpClient:     secureClient,
		clientID:       cfg.ClientID,
		clientRedirect: cfg.ClientRedirect,
		store:          store,
	}, nil
}

func validApplicationURL(u *url.URL) bool {
	if u == nil || u.Host == "" || u.User != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" {
		return false
	}
	host := u.Hostname()
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func validClientRedirect(u *url.URL) bool {
	if u != nil && u.Scheme == "birdtie-auth" {
		return u.Host == "callback" && u.Path == "" && u.User == nil &&
			u.Opaque == "" && u.RawQuery == "" && u.Fragment == ""
	}
	return validApplicationURL(u)
}

func ValidChallenge(challenge string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(challenge)
	return err == nil && len(raw) == 32 &&
		base64.RawURLEncoding.EncodeToString(raw) == challenge
}

func (s *Service) Start(ctx context.Context, clientChallenge string) (string, error) {
	if !ValidChallenge(clientChallenge) {
		return "", ErrInvalidFlow
	}
	state, err := randomString()
	if err != nil {
		return "", err
	}
	nonce, err := randomString()
	if err != nil {
		return "", err
	}
	providerVerifier := oauth2.GenerateVerifier()
	if err := s.store.SaveOIDCFlow(ctx, sha256.Sum256([]byte(state)), Flow{
		Nonce: nonce, ProviderVerifier: providerVerifier, ClientChallenge: clientChallenge,
	}); err != nil {
		return "", err
	}
	return s.config.AuthCodeURL(state, oauth2.S256ChallengeOption(providerVerifier),
		oauth2.SetAuthURLParam("nonce", nonce)), nil
}

func (s *Service) Complete(ctx context.Context, state, providerCode string) (string, error) {
	if !validRandomString(state) || providerCode == "" {
		return "", ErrInvalidFlow
	}
	flow, err := s.store.TakeOIDCFlow(ctx, sha256.Sum256([]byte(state)))
	if err != nil {
		return "", err
	}
	providerToken, err := s.config.Exchange(oidc.ClientContext(ctx, s.httpClient), providerCode,
		oauth2.VerifierOption(flow.ProviderVerifier))
	if err != nil {
		return "", ErrInvalidFlow
	}
	rawIDToken, ok := providerToken.Extra("id_token").(string)
	if !ok || rawIDToken == "" {
		return "", ErrInvalidFlow
	}
	idToken, err := s.verifier.Verify(ctx, rawIDToken)
	if err != nil || idToken.Subject == "" || len(idToken.Subject) > 255 ||
		strings.ContainsRune(idToken.Subject, 0) || idToken.Issuer == "" ||
		subtle.ConstantTimeCompare([]byte(idToken.Nonce), []byte(flow.Nonce)) != 1 {
		return "", ErrInvalidFlow
	}
	var claims struct {
		Name              string `json:"name"`
		PreferredUsername string `json:"preferred_username"`
		AuthorizedParty   string `json:"azp"`
	}
	if err := idToken.Claims(&claims); err != nil {
		return "", ErrInvalidFlow
	}
	if (claims.AuthorizedParty != "" && claims.AuthorizedParty != s.clientID) ||
		(len(idToken.Audience) > 1 && claims.AuthorizedParty == "") {
		return "", ErrInvalidFlow
	}
	for _, audience := range idToken.Audience {
		if audience != s.clientID {
			return "", ErrInvalidFlow
		}
	}
	displayName := cleanDisplayName(claims.Name)
	if displayName == "" {
		displayName = cleanDisplayName(claims.PreferredUsername)
	}
	if displayName == "" {
		displayName = "Birdtie member"
	}
	if len([]rune(displayName)) > 80 {
		displayName = string([]rune(displayName)[:80])
	}
	exchangeCode, err := randomString()
	if err != nil {
		return "", err
	}
	exchangeCode = codePrefix + exchangeCode
	if err := s.store.SaveOIDCExchangeCode(ctx, idToken.Issuer, idToken.Subject,
		displayName, sha256.Sum256([]byte(exchangeCode)),
		flow.ClientChallenge); err != nil {
		return "", err
	}
	redirect, _ := url.Parse(s.clientRedirect)
	params := redirect.Query()
	params.Set("code", exchangeCode)
	params.Set("challenge", flow.ClientChallenge)
	redirect.RawQuery = params.Encode()
	return redirect.String(), nil
}

func (s *Service) FailureRedirect() string {
	redirect, _ := url.Parse(s.clientRedirect)
	params := redirect.Query()
	params.Set("error", "oidc_login_failed")
	redirect.RawQuery = params.Encode()
	return redirect.String()
}

func cleanDisplayName(value string) string {
	return strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return -1
		}
		return r
	}, value))
}

func (s *Service) Exchange(ctx context.Context, code, clientVerifier string) (string, error) {
	if !strings.HasPrefix(code, codePrefix) ||
		!validRandomString(strings.TrimPrefix(code, codePrefix)) ||
		!verifierPattern.MatchString(clientVerifier) {
		return "", ErrInvalidFlow
	}
	token, digest, err := identity.NewToken()
	if err != nil {
		return "", err
	}
	challenge := oauth2.S256ChallengeFromVerifier(clientVerifier)
	err = s.store.RedeemOIDCExchangeCode(ctx, sha256.Sum256([]byte(code)),
		challenge, digest)
	if err != nil {
		return "", err
	}
	return token, nil
}

func randomString() (string, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validRandomString(value string) bool {
	raw, err := base64.RawURLEncoding.DecodeString(value)
	return err == nil && len(raw) == 32 &&
		base64.RawURLEncoding.EncodeToString(raw) == value
}
