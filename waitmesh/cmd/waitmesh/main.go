package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/waitmesh/waitmesh/internal/api"
	"github.com/waitmesh/waitmesh/internal/clock"
	"github.com/waitmesh/waitmesh/internal/config"
	"github.com/waitmesh/waitmesh/internal/domain"
	"github.com/waitmesh/waitmesh/internal/engine"
	"github.com/waitmesh/waitmesh/internal/scheduler"
	"github.com/waitmesh/waitmesh/internal/storage/sqlite"
	"github.com/waitmesh/waitmesh/pkg/client"
)

var (
	version = "0.1.0"
	baseURL string
	token   string
)

func main() {
	root := &cobra.Command{
		Use:   "waitmesh",
		Short: "Waitmesh — know when something didn't happen",
	}
	root.PersistentFlags().StringVar(&baseURL, "url", "http://127.0.0.1:7777", "API base URL")
	root.PersistentFlags().StringVar(&token, "token", os.Getenv("WAITMESH_TOKEN"), "Bearer token")

	root.AddCommand(serveCmd(), publishCmd(), rulesCmd(), eventsCmd(), pendingCmd(), benchmarkCmd(), versionCmd())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func serveCmd() *cobra.Command {
	var listen, dbPath string
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the Waitmesh server",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := config.FromEnv()
			if listen != "" {
				cfg.ListenAddr = listen
			}
			if dbPath != "" {
				cfg.DBPath = dbPath
			}
			if err := cfg.Validate(); err != nil {
				return err
			}
			log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
			db, err := sqlite.Open(cfg.DBPath)
			if err != nil {
				return err
			}
			defer db.Close()

			eng := engine.New(db, clock.System{}, log, cfg.WriteQueue)
			defer eng.Close()

			sched := scheduler.New(eng, clock.System{}, log)
			ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
			defer cancel()
			sched.Start(ctx)
			defer sched.Stop()

			srv := api.New(eng, cfg, log)
			httpSrv := &http.Server{
				Addr:         cfg.ListenAddr,
				Handler:      srv.Handler(),
				ReadTimeout:  cfg.ReadTimeout,
				WriteTimeout: cfg.WriteTimeout,
				IdleTimeout:  cfg.IdleTimeout,
			}
			go func() {
				<-ctx.Done()
				shCtx, c := context.WithTimeout(context.Background(), 5*time.Second)
				defer c()
				_ = httpSrv.Shutdown(shCtx)
			}()
			log.Info("waitmesh listening", "addr", cfg.ListenAddr, "db", cfg.DBPath)
			err = httpSrv.ListenAndServe()
			if err == http.ErrServerClosed {
				return nil
			}
			return err
		},
	}
	cmd.Flags().StringVar(&listen, "listen", "", "listen address (default 127.0.0.1:7777)")
	cmd.Flags().StringVar(&dbPath, "db", "", "sqlite path")
	return cmd
}

func cli() *client.Client {
	opts := []client.Option{client.WithTimeout(15 * time.Second)}
	if token != "" {
		opts = append(opts, client.WithToken(token))
	}
	return client.New(baseURL, opts...)
}

func publishCmd() *cobra.Command {
	var typ, key, id, data string
	cmd := &cobra.Command{
		Use:   "publish",
		Short: "Publish an event",
		RunE: func(cmd *cobra.Command, args []string) error {
			if id == "" {
				id = fmt.Sprintf("evt_%d", time.Now().UnixNano())
			}
			in := domain.EventInput{ID: id, Type: typ, Key: key}
			if data != "" {
				in.Data = json.RawMessage(data)
			}
			out, err := cli().Publish(context.Background(), in)
			if err != nil {
				return err
			}
			return printJSON(out)
		},
	}
	cmd.Flags().StringVar(&typ, "type", "", "event type (required)")
	cmd.Flags().StringVar(&key, "key", "", "correlation key (required)")
	cmd.Flags().StringVar(&id, "id", "", "event id")
	cmd.Flags().StringVar(&data, "data", "", "JSON data")
	_ = cmd.MarkFlagRequired("type")
	_ = cmd.MarkFlagRequired("key")
	return cmd
}

func rulesCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rules", Short: "Manage rules"}
	cmd.AddCommand(rulesAddCmd(), rulesListCmd(), rulesEnableCmd(), rulesDisableCmd())
	return cmd
}

func rulesAddCmd() *cobra.Command {
	var name, typ, start, unless, then, event, within, emit string
	var count int
	cmd := &cobra.Command{
		Use:   "add",
		Short: "Add a rule",
		RunE: func(cmd *cobra.Command, args []string) error {
			in := domain.RuleInput{
				Name: name, Type: domain.RuleType(typ), Start: start, Unless: unless,
				Then: then, Event: event, Count: count, Within: within, Emit: emit,
			}
			rule, err := cli().AddRule(context.Background(), in)
			if err != nil {
				return err
			}
			return printJSON(rule)
		},
	}
	cmd.Flags().StringVar(&name, "name", "", "rule name")
	cmd.Flags().StringVar(&typ, "type", "absence", "absence|sequence|count")
	cmd.Flags().StringVar(&start, "start", "", "start event type")
	cmd.Flags().StringVar(&unless, "unless", "", "unless event type (absence)")
	cmd.Flags().StringVar(&then, "then", "", "then event type (sequence)")
	cmd.Flags().StringVar(&event, "event", "", "event type (count)")
	cmd.Flags().IntVar(&count, "count", 0, "threshold (count)")
	cmd.Flags().StringVar(&within, "within", "5m", "duration window")
	cmd.Flags().StringVar(&emit, "emit", "", "emit event type")
	_ = cmd.MarkFlagRequired("name")
	_ = cmd.MarkFlagRequired("emit")
	return cmd
}

func rulesListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "List rules",
		RunE: func(cmd *cobra.Command, args []string) error {
			rules, err := cli().ListRules(context.Background())
			if err != nil {
				return err
			}
			return printJSON(rules)
		},
	}
}

func rulesEnableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "enable [id]",
		Short: "Enable a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli().EnableRule(context.Background(), args[0])
		},
	}
}

func rulesDisableCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "disable [id]",
		Short: "Disable a rule",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cli().DisableRule(context.Background(), args[0])
		},
	}
}

func eventsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "events", Short: "Event commands"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List events",
		RunE: func(cmd *cobra.Command, args []string) error {
			evs, err := cli().ListEvents(context.Background())
			if err != nil {
				return err
			}
			return printJSON(evs)
		},
	})
	return cmd
}

func pendingCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "pending", Short: "Pending match commands"}
	cmd.AddCommand(&cobra.Command{
		Use:   "list",
		Short: "List pending matches",
		RunE: func(cmd *cobra.Command, args []string) error {
			items, err := cli().ListPending(context.Background())
			if err != nil {
				return err
			}
			return printJSON(items)
		},
	})
	return cmd
}

func benchmarkCmd() *cobra.Command {
	var n, rules int
	cmd := &cobra.Command{
		Use:   "benchmark",
		Short: "Local ingest micro-benchmark against a temp DB",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := os.MkdirTemp("", "waitmesh-bench-*")
			if err != nil {
				return err
			}
			defer os.RemoveAll(dir)
			db, err := sqlite.Open(dir + "/bench.db")
			if err != nil {
				return err
			}
			defer db.Close()
			log := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
			eng := engine.New(db, clock.System{}, log, 4096)
			defer eng.Close()
			ctx := context.Background()
			for i := 0; i < rules; i++ {
				_, err := eng.CreateRule(ctx, domain.RuleInput{
					Name: fmt.Sprintf("r-%d", i), Type: domain.RuleAbsence,
					Start: "a", Unless: "b", Within: "1h", Emit: "c",
				})
				if err != nil {
					return err
				}
			}
			start := time.Now()
			for i := 0; i < n; i++ {
				_, err := eng.Ingest(ctx, domain.EventInput{
					ID: fmt.Sprintf("e-%d", i), Type: "x", Key: "k",
				})
				if err != nil {
					return err
				}
			}
			elapsed := time.Since(start)
			fmt.Printf("events=%d rules=%d elapsed=%s throughput=%.0f/s\n",
				n, rules, elapsed, float64(n)/elapsed.Seconds())
			return nil
		},
	}
	cmd.Flags().IntVar(&n, "n", 1000, "number of events")
	cmd.Flags().IntVar(&rules, "rules", 0, "number of absence rules")
	return cmd
}

func versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	}
}

func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}
