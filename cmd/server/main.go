package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/fabfab/airplane-chat/internal/config"
	"github.com/fabfab/airplane-chat/internal/embeddings"
	"github.com/fabfab/airplane-chat/internal/ollama"
	"github.com/fabfab/airplane-chat/internal/server"
	"github.com/fabfab/airplane-chat/internal/storage"
	"github.com/fabfab/airplane-chat/internal/vectorstore"
)

func main() {
	log.SetFlags(log.LstdFlags | log.Lshortfile)

	var showVersion bool
	flag.BoolVar(&showVersion, "version", false, "print version information and exit")
	flag.Parse()

	if showVersion {
		fmt.Println("airplane-chat dev build")
		return
	}

	cfg, err := config.FromEnv()
	if err != nil {
		log.Fatalf("failed to load configuration: %v", err)
	}

	store, err := storage.NewManager(cfg.DataDir)
	if err != nil {
		log.Fatalf("failed to set up storage: %v", err)
	}

	embedder := embeddings.NewOllamaEmbedder(cfg.Ollama.Host, cfg.Embed.Model, cfg.Embed.Dimension, 90*time.Second)

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	vectorStore, err := vectorstore.NewPostgresStore(ctx, cfg.Database.URL, cfg.Database.MaxConnections, cfg.Embed.Dimension)
	if err != nil {
		log.Fatalf("failed to connect vector store: %v", err)
	}
	defer vectorStore.Close()

	llmClient := ollama.NewClient(cfg.Ollama.Host, cfg.Ollama.Model)
	srv := server.New(cfg, store, llmClient, embedder, vectorStore)

	httpServer := &http.Server{
		Addr:    cfg.Address,
		Handler: srv,
	}

	log.Printf("starting server on %s (data dir: %s, model: %s)", cfg.Address, cfg.DataDir, cfg.Ollama.Model)

	go func() {
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server error: %v", err)
		}
	}()

	waitForShutdown(httpServer)
}

func waitForShutdown(srv *http.Server) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
		if err := srv.Close(); err != nil {
			log.Printf("forced close failed: %v", err)
		}
	}

	log.Println("server stopped")
}
