package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"time"
)

const (
	defaultSessionDuration = 24 * time.Hour
	maximumSessionDuration = 30 * 24 * time.Hour
)

type API struct {
	Host              string
	Port              string
	DatabaseURL       string
	SessionDuration   time.Duration
	CookieSecure      bool
	ApplicationOrigin string
}

func LoadAPI() (API, error) {
	databaseURL, err := RequireDatabaseURL()
	if err != nil {
		return API{}, err
	}
	duration := defaultSessionDuration
	if value := os.Getenv("SESSION_DURATION"); value != "" {
		duration, err = time.ParseDuration(value)
		if err != nil || duration < time.Minute || duration > maximumSessionDuration {
			return API{}, errors.New("SESSION_DURATION must be between 1m and 720h")
		}
	}
	cookieSecure := true
	if value := os.Getenv("SESSION_COOKIE_SECURE"); value != "" {
		cookieSecure, err = strconv.ParseBool(value)
		if err != nil {
			return API{}, errors.New("SESSION_COOKIE_SECURE must be true or false")
		}
	}
	origin := os.Getenv("APP_ORIGIN")
	if err := validateOrigin(origin, cookieSecure); err != nil {
		return API{}, err
	}
	return API{
		Host:              envOrDefault("HOST", "127.0.0.1"),
		Port:              envOrDefault("PORT", "8080"),
		DatabaseURL:       databaseURL,
		SessionDuration:   duration,
		CookieSecure:      cookieSecure,
		ApplicationOrigin: origin,
	}, nil
}

func RequireDatabaseURL() (string, error) {
	value := os.Getenv("DATABASE_URL")
	if value == "" {
		return "", errors.New("DATABASE_URL is required")
	}
	return value, nil
}

func validateOrigin(value string, requireHTTPS bool) error {
	parsed, err := url.Parse(value)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" || parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return errors.New("APP_ORIGIN must be an origin such as https://runway.example")
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return errors.New("APP_ORIGIN must use http or https")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("APP_ORIGIN must use https when SESSION_COOKIE_SECURE is true")
	}
	if !requireHTTPS && !isLoopbackHost(parsed.Hostname()) {
		return errors.New("SESSION_COOKIE_SECURE may be false only for a loopback APP_ORIGIN")
	}
	return nil
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	address := net.ParseIP(host)
	return address != nil && address.IsLoopback()
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
