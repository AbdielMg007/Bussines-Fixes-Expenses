package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"runway/backend/internal/auth"
	"runway/backend/internal/auth/password"
	"runway/backend/internal/config"
	"runway/backend/internal/httpapi"
	"runway/backend/internal/ledger"
	"runway/backend/internal/postgres"
)

func main() {
	if err := run(); err != nil {
		log.Fatal(err)
	}
}

func run() error {
	configuration, err := config.LoadAPI()
	if err != nil {
		return err
	}
	startupContext, cancelStartup := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelStartup()
	pool, err := postgres.Open(startupContext, configuration.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	hasher, err := password.NewArgon2id(password.DefaultParameters())
	if err != nil {
		return errors.New("initialize password hashing")
	}
	authentication, err := auth.NewService(
		postgres.NewAuthRepository(pool),
		hasher,
		auth.ServiceOptions{SessionDuration: configuration.SessionDuration},
	)
	if err != nil {
		return err
	}
	financialLedger, err := ledger.NewService(postgres.NewLedgerRepository(pool), ledger.ServiceOptions{})
	if err != nil {
		return err
	}

	address := net.JoinHostPort(configuration.Host, configuration.Port)
	server := &http.Server{
		Addr: address,
		Handler: httpapi.NewHandler(authentication, financialLedger, httpapi.AuthConfig{
			AllowedOrigin:        configuration.ApplicationOrigin,
			CookieSecure:         configuration.CookieSecure,
			SessionMaxAgeSeconds: int(configuration.SessionDuration / time.Second),
		}),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       10 * time.Second,
		WriteTimeout:      10 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	shutdownSignal, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverErrors := make(chan error, 1)
	log.Printf("Runway API listening on http://%s", address)
	go func() {
		serverErrors <- server.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("serve HTTP: %w", err)
		}
		return nil
	case <-shutdownSignal.Done():
		log.Printf("shutdown signal received")
	}

	shutdownContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := server.Shutdown(shutdownContext); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}

	if err := <-serverErrors; err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("serve HTTP during shutdown: %w", err)
	}

	log.Printf("Runway API stopped")
	return nil
}
