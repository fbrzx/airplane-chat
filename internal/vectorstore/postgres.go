package vectorstore

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/pgvector/pgvector-go"
)

// Chunk represents a retrieved document snippet along with metadata.
type Chunk struct {
	ID             uuid.UUID
	DocumentID     string
	ConversationID string
	Content        string
	Score          float32
}

// Store persists and retrieves embeddings from Postgres + pgvector.
type Store struct {
	pool      *pgxpool.Pool
	dimension int
}

// NewPostgresStore connects to Postgres and ensures the necessary schema exists.
func NewPostgresStore(ctx context.Context, dsn string, maxConns int, dimension int) (*Store, error) {
	cfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		return nil, fmt.Errorf("parse database URL: %w", err)
	}

	if maxConns > 0 {
		cfg.MaxConns = int32(maxConns)
	}

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	store := &Store{
		pool:      pool,
		dimension: dimension,
	}

	if err := store.ensureSchema(ctx); err != nil {
		pool.Close()
		return nil, err
	}

	return store, nil
}

// Close releases the underlying database resources.
func (s *Store) Close() {
	s.pool.Close()
}

func (s *Store) ensureSchema(ctx context.Context) error {
	const statements = `
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS document_chunks (
	id UUID PRIMARY KEY,
	conversation_id TEXT NOT NULL,
	document_id TEXT NOT NULL,
	chunk_index INT NOT NULL,
	content TEXT NOT NULL,
	embedding vector(%[1]d) NOT NULL,
	created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS document_chunks_conversation_idx
	ON document_chunks (conversation_id);

CREATE INDEX IF NOT EXISTS document_chunks_document_idx
	ON document_chunks (document_id);

-- Create the IVF index if it is missing. This is idempotent because we guard it.
DO $$
BEGIN
	IF NOT EXISTS (
		SELECT 1
		FROM pg_indexes
		WHERE schemaname = current_schema()
			AND indexname = 'document_chunks_embedding_idx'
	) THEN
		EXECUTE 'CREATE INDEX document_chunks_embedding_idx ON document_chunks USING ivfflat (embedding vector_cosine_ops) WITH (lists = 100);';
	END IF;
END
$$;
`

	_, err := s.pool.Exec(ctx, fmt.Sprintf(statements, s.dimension))
	if err != nil && strings.Contains(err.Error(), "ivfflat") {
		// IVF requires an approximate index; if it fails (e.g. insufficient rows),
		// we ignore and continue.
		err = nil
	}
	return err
}

// UpsertDocumentChunks replaces the embeddings for a given document.
func (s *Store) UpsertDocumentChunks(ctx context.Context, conversationID, documentID string, contents []string, vectors [][]float32) error {
	if len(contents) != len(vectors) {
		return fmt.Errorf("contents and vectors length mismatch")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx, `DELETE FROM document_chunks WHERE conversation_id = $1 AND document_id = $2`, conversationID, documentID); err != nil {
		return fmt.Errorf("delete existing chunks: %w", err)
	}

	for idx, content := range contents {
		if len(vectors[idx]) != s.dimension {
			return fmt.Errorf("vector dimension mismatch: expected %d got %d", s.dimension, len(vectors[idx]))
		}

		id := uuid.New()
		if _, err := tx.Exec(
			ctx,
			`INSERT INTO document_chunks (id, conversation_id, document_id, chunk_index, content, embedding, created_at)
				 VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			id,
			conversationID,
			documentID,
			idx,
			content,
			pgvector.NewVector(vectors[idx]),
			time.Now().UTC(),
		); err != nil {
			return fmt.Errorf("insert chunk: %w", err)
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

// QuerySimilar returns the most relevant chunks for the provided embedding.
func (s *Store) QuerySimilar(ctx context.Context, conversationID string, embedding []float32, limit int) ([]Chunk, error) {
	if len(embedding) != s.dimension {
		return nil, fmt.Errorf("embedding dimension mismatch: expected %d got %d", s.dimension, len(embedding))
	}

	rows, err := s.pool.Query(ctx, `
SELECT id, document_id, content, 1 - (embedding <=> $1) AS score
FROM document_chunks
WHERE conversation_id = $2
ORDER BY embedding <=> $1
LIMIT $3`, pgvector.NewVector(embedding), conversationID, limit)
	if err != nil {
		return nil, fmt.Errorf("query similar chunks: %w", err)
	}
	defer rows.Close()

	var chunks []Chunk
	for rows.Next() {
		var chunk Chunk
		chunk.ConversationID = conversationID
		if err := rows.Scan(&chunk.ID, &chunk.DocumentID, &chunk.Content, &chunk.Score); err != nil {
			return nil, fmt.Errorf("scan chunk: %w", err)
		}
		chunks = append(chunks, chunk)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate chunks: %w", err)
	}

	return chunks, nil
}

// DeleteConversation removes all embeddings for the given conversation.
func (s *Store) DeleteConversation(ctx context.Context, conversationID string) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM document_chunks WHERE conversation_id = $1`, conversationID)
	return err
}

// RefreshDocument is a helper that reindexes a single document by running the provided function to generate chunks.
func (s *Store) RefreshDocument(ctx context.Context, conversationID, documentID string, chunkFn func() ([]string, error), embedFn func(context.Context, []string) ([][]float32, error)) error {
	if chunkFn == nil || embedFn == nil {
		return errors.New("chunk function and embed function must be provided")
	}

	contents, err := chunkFn()
	if err != nil {
		return fmt.Errorf("chunk document: %w", err)
	}
	if len(contents) == 0 {
		return s.UpsertDocumentChunks(ctx, conversationID, documentID, []string{}, [][]float32{})
	}

	vectors, err := embedFn(ctx, contents)
	if err != nil {
		return fmt.Errorf("embed document: %w", err)
	}

	return s.UpsertDocumentChunks(ctx, conversationID, documentID, contents, vectors)
}
