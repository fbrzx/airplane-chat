# Airplane Chat

Local chat interface for interacting with Ollama models, with document uploads that enrich the prompt context and assistant replies saved as Markdown transcripts.

## Prerequisites

- Go 1.21 or newer (tested with Go 1.23)
- Node.js 18+ and npm
- An Ollama instance running locally with the desired models pulled (defaults to `llama3.1:8b`)

## Backend

```bash
# from the repo root
export OLLAMA_MODEL=llama3.1:8b          # optional override
export OLLAMA_HOST=http://localhost:11434
export SERVER_ADDR=127.0.0.1:8080        # optional override
export DATA_DIR=./data                   # optional override

go run ./cmd/server
```

The server accepts HTTP requests on `/api` and persists conversation data under `DATA_DIR`:

- `conversations/<id>/history.json` – chat history
- `conversations/<id>/documents/` – uploaded source files plus extracted text
- `conversations/<id>/transcripts/` – assistant responses as Markdown

## Frontend

```bash
cd frontend
npm install
npm run dev
```

Vite serves the React app on http://localhost:5173 and proxies API calls to the Go server.

## Document Support

The upload panel currently accepts Markdown and plain-text files (`.md`, `.markdown`, `.txt`). Each document is stored in full and its text is appended to the chat system prompt (up to a configurable limit) so the assistant can reference it in responses.

## Useful Commands

- `go build ./...` – compile the backend
- `go test ./...` – execute backend tests (none yet, but ready for future additions)
- `npm run build` (inside `frontend/`) – create a production build of the UI

## Roadmap Ideas

- Streaming responses from Ollama for incremental rendering
- Support for additional document formats (PDF, CSV) via extractors
- Conversation management (naming, listing, resuming existing sessions)
