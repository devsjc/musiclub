// Command musicclub runs the music club server and its small admin subcommands.
//
//	musicclub                                    run the server
//	musicclub club add --slug S --name N         create a club
//	musicclub player add --club S --name N [--admin]
//	                                             add a player (the first admin has to come from here)
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
	"musiclub/internal/rounds"
	"musiclub/internal/scheduler"
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
	case "player":
		if len(args) >= 2 && args[1] == "add" {
			return playerAdd(cfg, args[2:])
		}
		return errors.New("usage: musicclub player add --club SLUG --name NAME [--admin]")
	default:
		return fmt.Errorf("unknown command %q (try: club add, player add, healthcheck)", args[0])
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
		Handler:           web.NewServer(database, cfg.CookieSecret, cfg.Location).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	svc := rounds.New(database)
	go scheduler.Run(ctx, svc, 10*time.Minute)

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

func playerAdd(cfg config.Config, args []string) error {
	fs := flag.NewFlagSet("player add", flag.ContinueOnError)
	slug := fs.String("club", "", "slug of the club to add the player to")
	name := fs.String("name", "", "player name")
	admin := fs.Bool("admin", false, "make the player an admin")
	if err := fs.Parse(args); err != nil {
		return err
	}

	database, err := db.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer database.Close()

	ctx := context.Background()
	club, err := store.NewClubs(database).BySlug(ctx, *slug)
	if err != nil {
		return fmt.Errorf("club %q: %w", *slug, err)
	}
	p, err := store.NewPlayers(database).Create(ctx, club.ID, *name, *admin)
	if err != nil {
		return err
	}
	fmt.Printf("added %q to %q (admin: %t)\n", p.Name, club.Name, p.IsAdmin)
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
