import React, { useCallback, useEffect, useRef, useState } from 'react';

type Message = {
  role: 'user' | 'assistant';
  content: string;
  timestamp: string;
};

type Document = {
  id: string;
  name: string;
  size: number;
  uploaded_at: string;
};

function formatBytes(bytes: number): string {
  const units = ['B', 'KB', 'MB', 'GB'];
  let size = bytes;
  let unitIndex = 0;

  while (size >= 1024 && unitIndex < units.length - 1) {
    size /= 1024;
    unitIndex += 1;
  }

  const formatted = size >= 10 || unitIndex === 0 ? size.toFixed(0) : size.toFixed(1);
  return `${formatted} ${units[unitIndex]}`;
}

const App: React.FC = () => {
  const [conversationId, setConversationId] = useState<string | null>(null);
  const [messages, setMessages] = useState<Message[]>([]);
  const [documents, setDocuments] = useState<Document[]>([]);
  const [input, setInput] = useState('');
  const [sending, setSending] = useState(false);
  const [uploading, setUploading] = useState(false);
  const messagesEndRef = useRef<HTMLDivElement | null>(null);

  const scrollToBottom = useCallback(() => {
    messagesEndRef.current?.scrollIntoView({ behavior: 'smooth' });
  }, []);

  useEffect(() => {
    const createConversation = async () => {
      const response = await fetch('/api/conversations', { method: 'POST' });
      if (!response.ok) {
        console.error('Failed to create conversation');
        return;
      }
      const data = await response.json();
      setConversationId(data.id);
    };

    createConversation().catch(error => console.error(error));
  }, []);

  useEffect(() => {
    if (!conversationId) {
      return;
    }

    const loadInitialState = async () => {
      const [messagesRes, documentsRes] = await Promise.all([
        fetch(`/api/conversations/${conversationId}/messages`),
        fetch(`/api/conversations/${conversationId}/documents`)
      ]);

      if (messagesRes.ok) {
        const data = await messagesRes.json();
        setMessages(data.messages ?? []);
      }

      if (documentsRes.ok) {
        const data = await documentsRes.json();
        setDocuments(data.documents ?? []);
      }
    };

    loadInitialState().catch(error => console.error(error));
  }, [conversationId]);

  useEffect(() => {
    scrollToBottom();
  }, [messages, scrollToBottom]);

  const sendMessage = async () => {
    if (!conversationId) {
      return;
    }
    const content = input.trim();
    if (!content) {
      return;
    }

    const userMessage: Message = {
      role: 'user',
      content,
      timestamp: new Date().toISOString()
    };

    setMessages(prev => [...prev, userMessage]);
    setInput('');
    setSending(true);
    try {
      const response = await fetch(`/api/conversations/${conversationId}/messages`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ content })
      });

      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || 'Failed to send message');
      }

      const data = await response.json();
      if (data.message) {
        setMessages(prev => [...prev, data.message as Message]);
      }
    } catch (error) {
      console.error(error);
      const errorMessage: Message = {
        role: 'assistant',
        content:
          error instanceof Error
            ? `Error: ${error.message}`
            : 'The server was unable to generate a response.',
        timestamp: new Date().toISOString()
      };
      setMessages(prev => [...prev, errorMessage]);
    } finally {
      setSending(false);
    }
  };

  const handleKeyDown = (event: React.KeyboardEvent<HTMLTextAreaElement>) => {
    if (event.key === 'Enter' && !event.shiftKey) {
      event.preventDefault();
      void sendMessage();
    }
  };

  const onFileChange = async (event: React.ChangeEvent<HTMLInputElement>) => {
    if (!conversationId || !event.target.files || event.target.files.length === 0) {
      return;
    }

    const file = event.target.files[0];
    const formData = new FormData();
    formData.append('file', file);

    setUploading(true);
    try {
      const response = await fetch(`/api/conversations/${conversationId}/documents`, {
        method: 'POST',
        body: formData
      });

      if (!response.ok) {
        const text = await response.text();
        throw new Error(text || 'Failed to upload document');
      }

      const data = await response.json();
      if (data.document) {
        setDocuments(prev => [data.document as Document, ...prev]);
      }
    } catch (error) {
      console.error(error);
    } finally {
      setUploading(false);
      event.target.value = '';
    }
  };

  return (
    <div className="app">
      <aside className="sidebar">
        <h2>Context Documents</h2>
        <p>Upload markdown or text files. Their contents enrich the chat context.</p>
        <div className="upload">
          <label htmlFor="file-input">{uploading ? 'Uploading…' : 'Upload document'}</label>
          <input id="file-input" type="file" accept=".txt,.md,.markdown" onChange={onFileChange} />
        </div>
        <div className="document-list">
          {documents.length === 0 ? (
            <div className="empty-state">No documents uploaded yet.</div>
          ) : (
            documents.map(doc => (
              <div key={doc.id} className="document-card">
                <strong>{doc.name}</strong>
                <span>{formatBytes(doc.size)}</span>
                <br />
                <span>{new Date(doc.uploaded_at).toLocaleString()}</span>
              </div>
            ))
          )}
        </div>
      </aside>
      <section className="chat">
        <header className="chat-header">
          <h1>Airplane Chat</h1>
          {conversationId ? (
            <div className="chat-meta">
              <span>Conversation ID:</span>
              <code>{conversationId}</code>
            </div>
          ) : null}
        </header>
        <div className="messages">
          {messages.length === 0 ? (
            <div className="empty-state">
              <p>Start by asking a question or uploading a document.</p>
            </div>
          ) : (
            messages.map((message, index) => (
              <div key={`${message.timestamp}-${index}`} className={`message ${message.role}`}>
                {message.content}
              </div>
            ))
          )}
          <div ref={messagesEndRef} />
        </div>
        <div className="composer">
          <textarea
            placeholder="Ask a question..."
            value={input}
            onChange={event => setInput(event.target.value)}
            onKeyDown={handleKeyDown}
            disabled={!conversationId || sending}
          />
          <button type="button" onClick={() => void sendMessage()} disabled={sending || !conversationId}>
            {sending ? 'Thinking…' : 'Send'}
          </button>
        </div>
      </section>
    </div>
  );
};

export default App;
