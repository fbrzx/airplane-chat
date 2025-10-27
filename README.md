# Airplane Chat

Local-first chat interface for interacting with Ollama models. Upload documents, have them embedded into a vector database, and let the assistant ground replies in the most relevant snippets. Each assistant turn is archived as a Markdown transcript.

## Prerequisites

- Go 1.21 or newer (tested with Go 1.23)
- Node.js 18+ and npm
- An Ollama instance running locally with the desired models pulled (defaults to `llama3.1:8b`)

## Backend

```bash
# from the repo root
docker compose up -d db                  # start pgvector

export OLLAMA_MODEL=llama3.1:8b          # optional override
export OLLAMA_HOST=http://localhost:11434
export SERVER_ADDR=127.0.0.1:8080        # optional override
export DATA_DIR=./data                   # optional override
export EMBEDDING_MODEL=nomic-embed-text  # optional override
export DATABASE_URL=postgres://airplane:airplane@localhost:5433/airplane_chat?sslmode=disable

go run ./cmd/server
```

The server accepts HTTP requests on `/api` and persists conversation data under `DATA_DIR`:

- `conversations/<id>/history.json` – chat history
- `conversations/<id>/documents/` – uploaded source files plus extracted text
- `conversations/<id>/transcripts/` – assistant responses as Markdown
- pgvector (`docker compose up -d db`) stores chunked document embeddings for retrieval-augmented prompts

## Frontend

```bash
cd frontend
npm install
npm run dev
```

Vite serves the React app on http://localhost:5173 and proxies API calls to the Go server.

## One-liner for local development

```bash
make dev
```

This fetches Go modules, installs frontend deps, ensures the pgvector container is up, and then runs the backend and Vite dev servers together (Ctrl+C stops both).

## Document Support

The upload panel currently accepts Markdown and plain-text files (`.md`, `.markdown`, `.txt`). Uploaded documents are chunked, embedded via Ollama’s embedding API (`nomic-embed-text` by default), and indexed in Postgres + pgvector. Each chat turn embeds the latest user question and pulls the top-matching snippets back into the prompt, keeping context bounded even for large document sets.

## Useful Commands

- `go build ./...` – compile the backend
- `go test ./...` – execute backend tests (none yet, but ready for future additions)
- `npm run build` (inside `frontend/`) – create a production build of the UI
- `docker compose down` – stop the vector database when you are done
- `make reset-vector-db` – wipe the pgvector volume (stops the container; run `make start-vector-db` afterwards)

## Roadmap Ideas

- Streaming responses from Ollama for incremental rendering
- Support for additional document formats (PDF, CSV) via extractors
- Conversation management (naming, listing, resuming existing sessions)
