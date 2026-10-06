// Command musicclub runs the music club server and its small admin subcommands.
//
//	musicclub                                    run the server
//	musicclub club add --slug S --name N         create a club
//	musicclub healthcheck                        GET /healthz (for Docker HEALTHCHECK)
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"musiclub/internal/config"
	"musiclub/internal/db"
	"musiclub/internal/store"
	"musiclub/internal/web"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	if len(args) == 0 {
		return serve(cfg)
	}
	switch args[0] {
	case "healthcheck":
		return healthcheck(cfg)
	case "club":
		if len(args) >= 2 && args[1] == "add" {
			return clubAdd(cfg, args[2:])
		}
		return errors.New("usage: musicclub club add --slug SLUG --name NAME")
	default:
		return fmt.Errorf("unknown command %q (try: club add, healthcheck)", args[0])
	}
}

func serve(cfg config.Config) error {
	if err := cfg.RequireSecret(); err != nil {
		return err
	}
	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           web.NewServer(database).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	slog.Info("listening", "addr", srv.Addr, "db", cfg.DBPath)

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutdownCtx)
}

func clubAdd(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("club add", flag.ContinueOnError)
	slug := fs.String("slug", "", "URL slug, e.g. friday-friends")
	name := fs.String("name", "", "display name")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	club, err := store.NewClubs(database).Create(context.Background(), *slug, *name)
	if err != nil {
		return err
	}
	fmt.Printf("created club %q at /c/%s\n", club.Name, club.Slug)
	return nil
}

func healthcheck(cfg config.Config) error {
	client := http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort("127.0.0.1", cfg.Port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz returned %s", resp.Status)
	}
	return nil
}
