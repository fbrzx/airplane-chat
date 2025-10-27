package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/google/uuid"

	"github.com/fabfab/airplane-chat/internal/config"
	"github.com/fabfab/airplane-chat/internal/ollama"
	"github.com/fabfab/airplane-chat/internal/storage"
)

// Server wires HTTP handlers to the underlying chat and storage services.
type Server struct {
	cfg     config.Config
	router  http.Handler
	storage *storage.Manager
	llm     ollama.Client
}

// New constructs a Server with the provided dependencies.
func New(cfg config.Config, store *storage.Manager, llmClient ollama.Client) *Server {
	mux := chi.NewRouter()
	mux.Use(middleware.RequestID)
	mux.Use(middleware.RealIP)
	mux.Use(middleware.Logger)
	mux.Use(middleware.Recoverer)
	mux.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:5173", "http://127.0.0.1:5173"},
		AllowedMethods:   []string{http.MethodGet, http.MethodPost, http.MethodOptions},
		AllowedHeaders:   []string{"Accept", "Content-Type", "X-CSRF-Token"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	s := &Server{
		cfg:     cfg,
		router:  mux,
		storage: store,
		llm:     llmClient,
	}

	mux.Get("/api/health", s.handleHealth)
	mux.Post("/api/conversations", s.handleCreateConversation)
	mux.Get("/api/conversations/{id}/messages", s.handleGetMessages)
	mux.Post("/api/conversations/{id}/messages", s.handlePostMessage)
	mux.Get("/api/conversations/{id}/documents", s.handleListDocuments)
	mux.Post("/api/conversations/{id}/documents", s.handleUploadDocument)

	return s
}

// ServeHTTP exposes the router so Server satisfies http.Handler.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleCreateConversation(w http.ResponseWriter, r *http.Request) {
	id := uuid.NewString()
	if err := s.storage.EnsureConversation(id); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("prepare conversation: %w", err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"id": id})
}

func (s *Server) handleGetMessages(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing conversation id"))
		return
	}

	history, err := s.storage.LoadHistory(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load history: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{"messages": history})
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing conversation id"))
		return
	}

	var payload struct {
		Content string `json:"content"`
	}

	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("decode request: %w", err))
		return
	}

	payload.Content = strings.TrimSpace(payload.Content)
	if payload.Content == "" {
		writeError(w, http.StatusBadRequest, errors.New("content must not be empty"))
		return
	}

	userMessage := storage.Message{
		Role:      "user",
		Content:   payload.Content,
		Timestamp: time.Now().UTC(),
	}

	if err := s.storage.AppendMessage(id, userMessage); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("store user message: %w", err))
		return
	}

	history, err := s.storage.LoadHistory(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("load history: %w", err))
		return
	}

	ollamaMessages := buildPrompt(history, s.storage, id)
	response, err := s.llm.Generate(r.Context(), ollamaMessages)
	if err != nil {
		writeError(w, http.StatusBadGateway, fmt.Errorf("generate response: %w", err))
		return
	}

	assistantMessage := storage.Message{
		Role:      "assistant",
		Content:   response,
		Timestamp: time.Now().UTC(),
	}

	if err := s.storage.AppendMessage(id, assistantMessage); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("store assistant message: %w", err))
		return
	}

	if _, err := s.storage.SaveTranscript(id, response, assistantMessage.Timestamp); err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("save transcript: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"message": assistantMessage,
	})
}

func (s *Server) handleListDocuments(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing conversation id"))
		return
	}

	documents, err := s.storage.ListDocuments(id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("list documents: %w", err))
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"documents": documents,
	})
}

func (s *Server) handleUploadDocument(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if id == "" {
		writeError(w, http.StatusBadRequest, errors.New("missing conversation id"))
		return
	}

	if err := r.ParseMultipartForm(10 << 20); err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("parse form: %w", err))
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, fmt.Errorf("read file: %w", err))
		return
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		writeError(w, http.StatusInternalServerError, fmt.Errorf("read upload: %w", err))
		return
	}

	document, err := s.storage.SaveDocument(id, header.Filename, data)
	if err != nil {
		if errors.Is(err, storage.ErrUnsupportedFileType) {
			writeError(w, http.StatusBadRequest, err)
			return
		}
		writeError(w, http.StatusInternalServerError, fmt.Errorf("store document: %w", err))
		return
	}

	writeJSON(w, http.StatusCreated, map[string]any{
		"document": document,
	})
}

func buildPrompt(history []storage.Message, store *storage.Manager, conversationID string) []ollama.Message {
	const (
		maxDocCharacters = 8000
		maxCombinedDocs  = 24000
	)

	docMessages := []string{}
	if store != nil {
		if texts, err := store.LoadDocumentTexts(conversationID); err == nil {
			total := 0
			for _, text := range texts {
				trimmed := trimToLimit(text, maxDocCharacters)
				if trimmed == "" {
					continue
				}
				if total+len(trimmed) > maxCombinedDocs {
					break
				}
				docMessages = append(docMessages, trimmed)
				total += len(trimmed)
			}
		}
	}

	var messages []ollama.Message

	systemContent := "You are a helpful assistant. Answer the user's question using the conversation history"
	if len(docMessages) > 0 {
		systemContent += " and the following reference documents.\n\n"
		for i, doc := range docMessages {
			systemContent += fmt.Sprintf("Document %d:\n%s\n\n", i+1, doc)
		}
	}
	messages = append(messages, ollama.Message{
		Role:    "system",
		Content: systemContent,
	})

	for _, msg := range history {
		messages = append(messages, ollama.Message{
			Role:    msg.Role,
			Content: msg.Content,
		})
	}

	return messages
}

func trimToLimit(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	return text[:limit]
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(payload); err != nil {
		fmt.Printf("failed to write JSON response: %v\n", err)
	}
}

func writeError(w http.ResponseWriter, status int, err error) {
	writeJSON(w, status, map[string]any{
		"error": err.Error(),
	})
}
