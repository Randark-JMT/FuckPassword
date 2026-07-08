package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"fuckpassword/internal/api"
	"fuckpassword/internal/config"
	"fuckpassword/internal/db"
	"fuckpassword/internal/ingest"
	"fuckpassword/internal/logstream"
	"fuckpassword/internal/queue"
	"fuckpassword/internal/tasklock"
)

func main() {
	cfg := config.Load()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	database, err := db.Open(ctx, cfg.DSN)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer database.Pool.Close()
	log.Println("database connected and schema applied")

	database.ResetStuckJobs(ctx)
	database.StartReaper(ctx, cfg.TTLDays)

	tasks := tasklock.New()
	logs := logstream.New(500)
	logs.Publish("system", "info", "Server started", nil)

	ing := ingest.New(database, tasks, logs, cfg.UploadDir, cfg.IngestBatch, cfg.MaxLineBytes)
	ing.SweepOrphans()
	ing.Start(ctx)

	worker := queue.New(database, tasks, logs, cfg.StatementTimeout)
	go worker.Run(ctx)

	apiObj := &api.API{DB: database, Ingest: ing, Tasks: tasks, Logs: logs, MaxQueue: cfg.MaxQueue}
	srv := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           api.NewRouter(apiObj),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("listening on %s", cfg.ListenAddr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("shutting down")
	shut, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := srv.Shutdown(shut); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
