package main

import (
	"bufio"
	"context"
	"crypto/subtle"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"golang.org/x/term"

	"runway/backend/internal/auth"
	"runway/backend/internal/auth/password"
	"runway/backend/internal/config"
	"runway/backend/internal/postgres"
)

func main() {
	if err := run(context.Background(), os.Args[1:], os.Stdin, os.Stderr); err != nil {
		log.Fatal(err)
	}
}

func run(ctx context.Context, arguments []string, stdin io.Reader, stderr io.Writer) error {
	flags := flag.NewFlagSet("bootstrap", flag.ContinueOnError)
	flags.SetOutput(stderr)
	email := flags.String("email", "", "owner email address")
	passwordStdin := flags.Bool("password-stdin", false, "read one password line from standard input")
	if err := flags.Parse(arguments); err != nil {
		return err
	}
	if *email == "" {
		return errors.New("-email is required")
	}
	passwordValue, err := readPassword(stdin, stderr, *passwordStdin)
	if err != nil {
		return err
	}

	databaseURL, err := config.RequireDatabaseURL()
	if err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, databaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	hasher, err := password.NewArgon2id(password.DefaultParameters())
	if err != nil {
		return err
	}
	service, err := auth.NewService(postgres.NewAuthRepository(pool), hasher, auth.ServiceOptions{
		SessionDuration: 24 * time.Hour,
	})
	if err != nil {
		return err
	}
	owner, err := service.Bootstrap(ctx, *email, passwordValue)
	if errors.Is(err, auth.ErrOwnerExists) {
		return errors.New("owner bootstrap refused: an owner already exists")
	}
	if err != nil {
		return fmt.Errorf("bootstrap owner: %w", err)
	}
	log.Printf("owner %s created", owner.Email())
	return nil
}

func readPassword(stdin io.Reader, stderr io.Writer, fromStdin bool) (string, error) {
	if fromStdin {
		value, err := bufio.NewReader(io.LimitReader(stdin, 2049)).ReadString('\n')
		if err != nil && !errors.Is(err, io.EOF) {
			return "", errors.New("read password")
		}
		return strings.TrimSuffix(strings.TrimSuffix(value, "\n"), "\r"), nil
	}
	file, ok := stdin.(*os.File)
	if !ok || !term.IsTerminal(int(file.Fd())) {
		return "", errors.New("standard input is not a terminal; use -password-stdin for automation")
	}
	_, _ = fmt.Fprint(stderr, "Owner password: ")
	value, err := term.ReadPassword(int(file.Fd()))
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return "", errors.New("read password")
	}
	_, _ = fmt.Fprint(stderr, "Confirm owner password: ")
	confirmation, err := term.ReadPassword(int(file.Fd()))
	_, _ = fmt.Fprintln(stderr)
	if err != nil {
		return "", errors.New("read password confirmation")
	}
	if subtle.ConstantTimeCompare(value, confirmation) != 1 {
		return "", errors.New("passwords do not match")
	}
	return string(value), nil
}
