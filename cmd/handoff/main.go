// Command handoff runs the Handoff server and its admin tools.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/joalavedra/handoff/internal/beads"
	"github.com/joalavedra/handoff/internal/config"
	"github.com/joalavedra/handoff/internal/core"
	"github.com/joalavedra/handoff/internal/db"
	"github.com/joalavedra/handoff/internal/mail"
	"github.com/joalavedra/handoff/internal/server"
	"github.com/joalavedra/handoff/web"
)

var version = "dev"

const usage = `handoff — one task list for a company's people and agents

  handoff serve                                  run the server (default)
  handoff create-org -slug acme -email a@acme.com [-name Acme]
  handoff bd import -org acme -as a@acme.com [-project Inbox] < .beads/issues.jsonl
  handoff bd export -org acme -as a@acme.com [-project Inbox] > issues.jsonl

Configuration is read from the environment; see README.md.
`

func main() {
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, nil)))
	args := os.Args[1:]
	cmd := "serve"
	if len(args) > 0 {
		cmd, args = args[0], args[1:]
	}
	var err error
	switch cmd {
	case "serve":
		err = serve()
	case "create-org":
		err = createOrg(args)
	case "bd":
		err = bd(args)
	case "version":
		fmt.Println(version)
	default:
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "handoff:", err)
		os.Exit(1)
	}
}

func open(ctx context.Context) (*config.Config, *core.Service, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, nil, err
	}
	d, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return nil, nil, err
	}
	return cfg, core.New(d, cfg), nil
}

func serve() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	cfg, svc, err := open(ctx)
	if err != nil {
		return err
	}
	defer svc.DB.Close()
	if err := svc.EnsureSingleOrg(ctx, os.Getenv("HANDOFF_ADMIN_EMAIL")); err != nil {
		return err
	}
	static, err := fs.Sub(web.Dist, "dist")
	if err != nil {
		return err
	}
	go svc.RunTriage(ctx)
	go svc.RunWebhooks(ctx)
	srv := server.New(cfg, svc, &mail.Mailer{URL: cfg.SMTPURL, From: cfg.MailFrom}, static, version)
	hs := &http.Server{Addr: cfg.Addr, Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = hs.Shutdown(sctx)
	}()
	where := cfg.BaseURL.String()
	if cfg.SingleOrg == "" {
		where = cfg.OrgURL("<workspace>")
	}
	slog.Info("handoff listening", "addr", cfg.Addr, "url", where, "version", version)
	if err := hs.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func createOrg(args []string) error {
	fl := flag.NewFlagSet("create-org", flag.ExitOnError)
	slug := fl.String("slug", "", "workspace address, e.g. acme")
	name := fl.String("name", "", "display name")
	email := fl.String("email", "", "first admin's email")
	_ = fl.Parse(args)
	ctx := context.Background()
	cfg, svc, err := open(ctx)
	if err != nil {
		return err
	}
	defer svc.DB.Close()
	org, _, err := svc.CreateOrg(ctx, core.NewOrg{Slug: *slug, Name: *name, AdminEmail: *email})
	if err != nil {
		return err
	}
	fmt.Printf("created %s: sign in at %s with %s\n", org.Name, cfg.OrgURL(org.Slug), *email)
	return nil
}

func bd(args []string) error {
	if len(args) == 0 || (args[0] != "import" && args[0] != "export") {
		return errors.New("usage: handoff bd import|export -org SLUG -as EMAIL [-project NAME]")
	}
	fl := flag.NewFlagSet("bd "+args[0], flag.ExitOnError)
	slug := fl.String("org", "", "workspace slug")
	as := fl.String("as", "", "email of the person the changes are recorded under")
	project := fl.String("project", "", "project name or id (import default: Inbox; export default: all)")
	_ = fl.Parse(args[1:])
	ctx := context.Background()
	_, svc, err := open(ctx)
	if err != nil {
		return err
	}
	defer svc.DB.Close()
	org, err := svc.OrgBySlug(ctx, *slug)
	if err != nil {
		return fmt.Errorf("workspace %q: %w", *slug, err)
	}
	m, err := svc.MemberForLogin(ctx, org, *as, "")
	if err != nil {
		return err
	}
	p, err := svc.Principal(ctx, org, m.ID, "api")
	if err != nil {
		return err
	}
	if args[0] == "export" {
		n, err := beads.Export(ctx, svc, p, *project, os.Stdout)
		fmt.Fprintf(os.Stderr, "exported %d tasks\n", n)
		return err
	}
	res, err := beads.Import(ctx, svc, p, *project, os.Stdin)
	fmt.Fprintf(os.Stderr, "created %d, already there %d, linked %d, closed %d\n", res.Created, res.Existed, res.Linked, res.Closed)
	return err
}
