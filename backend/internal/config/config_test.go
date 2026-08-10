package config

import (
	"testing"
	"time"
)

func TestLoadAPILocalDevelopmentConfiguration(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local-placeholder")
	t.Setenv("APP_ORIGIN", "http://localhost:3000")
	t.Setenv("SESSION_COOKIE_SECURE", "false")
	t.Setenv("SESSION_DURATION", "2h")
	t.Setenv("HOST", "127.0.0.2")
	t.Setenv("PORT", "9090")

	configuration, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if configuration.CookieSecure || configuration.SessionDuration != 2*time.Hour {
		t.Fatalf("auth configuration = %+v", configuration)
	}
	if configuration.Host != "127.0.0.2" || configuration.Port != "9090" {
		t.Fatalf("server address = %s:%s", configuration.Host, configuration.Port)
	}
}

func TestLoadAPIDefaultsToSecureCookie(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://local-placeholder")
	t.Setenv("APP_ORIGIN", "https://runway.example")
	t.Setenv("SESSION_COOKIE_SECURE", "")
	t.Setenv("SESSION_DURATION", "")

	configuration, err := LoadAPI()
	if err != nil {
		t.Fatalf("LoadAPI() error = %v", err)
	}
	if !configuration.CookieSecure || configuration.SessionDuration != 24*time.Hour {
		t.Fatalf("defaults = %+v", configuration)
	}
}

func TestLoadAPIRejectsUnsafeAuthenticationConfiguration(t *testing.T) {
	tests := []struct {
		name     string
		origin   string
		secure   string
		duration string
	}{
		{name: "missing origin", secure: "true"},
		{name: "credentials in origin", origin: "https://user@runway.example", secure: "true"},
		{name: "insecure production origin", origin: "http://runway.example", secure: "false"},
		{name: "secure cookie over HTTP", origin: "http://localhost:3000", secure: "true"},
		{name: "excessive duration", origin: "https://runway.example", secure: "true", duration: "721h"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("DATABASE_URL", "postgres://local-placeholder")
			t.Setenv("APP_ORIGIN", test.origin)
			t.Setenv("SESSION_COOKIE_SECURE", test.secure)
			t.Setenv("SESSION_DURATION", test.duration)
			if _, err := LoadAPI(); err == nil {
				t.Fatal("LoadAPI() error = nil, want validation error")
			}
		})
	}
}
