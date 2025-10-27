package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Message represents a single conversation turn stored in history.json.
type Message struct {
	Role      string    `json:"role"`
	Content   string    `json:"content"`
	Timestamp time.Time `json:"timestamp"`
}

// Document holds metadata about an uploaded document that can be reused for
// subsequent chat requests.
type Document struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	StoredPath   string    `json:"stored_path"`
	TextPath     string    `json:"text_path"`
	Size         int64     `json:"size"`
	UploadedAt   time.Time `json:"uploaded_at"`
	ContentCache string    `json:"-"` // populated on load to avoid repeat disk reads
}

// Manager provides a thin abstraction over the filesystem layout that stores
// conversations, associated documents, and markdown transcripts.
type Manager struct {
	root string

	mu    sync.Mutex
	locks map[string]*sync.Mutex
}

// ErrUnsupportedFileType is returned when a document with an unsupported
// extension is uploaded.
var ErrUnsupportedFileType = errors.New("unsupported file type")

// NewManager initialises a Manager rooted at the provided directory.
func NewManager(root string) (*Manager, error) {
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, fmt.Errorf("create data directory: %w", err)
	}
	return &Manager{
		root:  root,
		locks: make(map[string]*sync.Mutex),
	}, nil
}

// EnsureConversation prepares the directory structure for the requested
// conversation ID if it does not already exist.
func (m *Manager) EnsureConversation(conversationID string) error {
	dir := m.conversationDir(conversationID)
	subdirs := []string{
		dir,
		filepath.Join(dir, "documents"),
		filepath.Join(dir, "transcripts"),
	}

	for _, subdir := range subdirs {
		if err := os.MkdirAll(subdir, 0o755); err != nil {
			return fmt.Errorf("create conversation directory %q: %w", subdir, err)
		}
	}

	return nil
}

// AppendMessage adds a message to the conversation history.
func (m *Manager) AppendMessage(conversationID string, message Message) error {
	if err := m.EnsureConversation(conversationID); err != nil {
		return err
	}

	lock := m.lockFor(conversationID)
	lock.Lock()
	defer lock.Unlock()

	history, err := m.LoadHistory(conversationID)
	if err != nil {
		return err
	}

	history = append(history, message)

	data, err := json.MarshalIndent(history, "", "  ")
	if err != nil {
		return fmt.Errorf("encode history: %w", err)
	}

	if err := os.WriteFile(m.historyPath(conversationID), data, 0o644); err != nil {
		return fmt.Errorf("write history: %w", err)
	}

	return nil
}

// LoadHistory retrieves the stored conversation history. Missing history files
// are treated as an empty conversation.
func (m *Manager) LoadHistory(conversationID string) ([]Message, error) {
	path := m.historyPath(conversationID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Message{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read history: %w", err)
	}

	var history []Message
	if err := json.Unmarshal(data, &history); err != nil {
		return nil, fmt.Errorf("decode history: %w", err)
	}

	return history, nil
}

// SaveTranscript writes the assistant's response to a markdown file for later
// reference.
func (m *Manager) SaveTranscript(conversationID, content string, timestamp time.Time) (string, error) {
	if err := m.EnsureConversation(conversationID); err != nil {
		return "", err
	}

	filename := fmt.Sprintf("%s.md", timestamp.UTC().Format("20060102T150405Z"))
	path := filepath.Join(m.conversationDir(conversationID), "transcripts", filename)

	body := strings.Builder{}
	body.WriteString("---\n")
	body.WriteString(fmt.Sprintf("conversation_id: %s\n", conversationID))
	body.WriteString(fmt.Sprintf("timestamp: %s\n", timestamp.Format(time.RFC3339)))
	body.WriteString("---\n\n")
	body.WriteString(content)
	body.WriteString("\n")

	if err := os.WriteFile(path, []byte(body.String()), 0o644); err != nil {
		return "", fmt.Errorf("write transcript: %w", err)
	}

	return path, nil
}

// SaveDocument stores an uploaded file and its extracted text representation.
func (m *Manager) SaveDocument(conversationID, originalName string, data []byte) (Document, error) {
	if err := m.EnsureConversation(conversationID); err != nil {
		return Document{}, err
	}

	ext := strings.ToLower(filepath.Ext(originalName))
	if ext == "" {
		ext = ".txt"
	}
	if !isSupportedExtension(ext) {
		return Document{}, ErrUnsupportedFileType
	}

	docID := uuid.NewString()
	now := time.Now().UTC()

	// We always store the exact bytes that were uploaded so the user can
	// download them later if desired.
	storedName := fmt.Sprintf("%s%s", docID, ext)
	storedPath := filepath.Join(m.conversationDir(conversationID), "documents", storedName)
	if err := os.WriteFile(storedPath, data, 0o644); err != nil {
		return Document{}, fmt.Errorf("write document: %w", err)
	}

	text := extractText(ext, data)
	textPath := filepath.Join(m.conversationDir(conversationID), "documents", docID+".txt")
	if err := os.WriteFile(textPath, []byte(text), 0o644); err != nil {
		return Document{}, fmt.Errorf("write extracted text: %w", err)
	}

	document := Document{
		ID:           docID,
		Name:         originalName,
		StoredPath:   storedPath,
		TextPath:     textPath,
		Size:         int64(len(data)),
		UploadedAt:   now,
		ContentCache: text,
	}

	lock := m.lockFor(conversationID)
	lock.Lock()
	defer lock.Unlock()

	documents, err := m.loadDocuments(conversationID)
	if err != nil {
		return Document{}, err
	}

	documents = append(documents, document)
	if err := m.saveDocuments(conversationID, documents); err != nil {
		return Document{}, err
	}

	return document, nil
}

// ListDocuments returns metadata for all documents associated with the
// conversation.
func (m *Manager) ListDocuments(conversationID string) ([]Document, error) {
	if err := m.EnsureConversation(conversationID); err != nil {
		return nil, err
	}
	docs, err := m.loadDocuments(conversationID)
	if err != nil {
		return nil, err
	}

	for i := range docs {
		if docs[i].ContentCache == "" {
			if data, err := os.ReadFile(docs[i].TextPath); err == nil {
				docs[i].ContentCache = string(data)
			}
		}
	}

	return docs, nil
}

// LoadDocumentTexts returns the extracted textual content of all documents for
// use when assembling a chat prompt.
func (m *Manager) LoadDocumentTexts(conversationID string) ([]string, error) {
	docs, err := m.ListDocuments(conversationID)
	if err != nil {
		return nil, err
	}

	var texts []string
	for _, doc := range docs {
		data, err := os.ReadFile(doc.TextPath)
		if err != nil {
			return nil, fmt.Errorf("read document text: %w", err)
		}
		texts = append(texts, string(data))
	}
	return texts, nil
}

// DocumentText returns the extracted text for a specific document.
func (m *Manager) DocumentText(doc Document) (string, error) {
	if doc.ContentCache != "" {
		return doc.ContentCache, nil
	}

	data, err := os.ReadFile(doc.TextPath)
	if err != nil {
		return "", fmt.Errorf("read document text: %w", err)
	}
	return string(data), nil
}

func (m *Manager) loadDocuments(conversationID string) ([]Document, error) {
	path := m.documentsPath(conversationID)
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return []Document{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read documents: %w", err)
	}

	var documents []Document
	if err := json.Unmarshal(data, &documents); err != nil {
		return nil, fmt.Errorf("decode documents: %w", err)
	}
	return documents, nil
}

func (m *Manager) saveDocuments(conversationID string, documents []Document) error {
	data, err := json.MarshalIndent(documents, "", "  ")
	if err != nil {
		return fmt.Errorf("encode documents: %w", err)
	}
	if err := os.WriteFile(m.documentsPath(conversationID), data, 0o644); err != nil {
		return fmt.Errorf("write documents: %w", err)
	}
	return nil
}

func (m *Manager) lockFor(conversationID string) *sync.Mutex {
	m.mu.Lock()
	defer m.mu.Unlock()

	if lock, ok := m.locks[conversationID]; ok {
		return lock
	}

	lock := &sync.Mutex{}
	m.locks[conversationID] = lock
	return lock
}

func (m *Manager) conversationDir(conversationID string) string {
	return filepath.Join(m.root, "conversations", conversationID)
}

func (m *Manager) historyPath(conversationID string) string {
	return filepath.Join(m.conversationDir(conversationID), "history.json")
}

func (m *Manager) documentsPath(conversationID string) string {
	return filepath.Join(m.conversationDir(conversationID), "documents.json")
}

func isSupportedExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".txt", ".md", ".markdown":
		return true
	default:
		return false
	}
}

func extractText(ext string, data []byte) string {
	switch strings.ToLower(ext) {
	case ".md", ".markdown":
		return string(data)
	default:
		return string(data)
	}
}
