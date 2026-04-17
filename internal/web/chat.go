package web

import (
	"context"
	"fmt"
	"net/http"
)

func (s *Server) handleChatPage(w http.ResponseWriter, r *http.Request) {
	data := map[string]interface{}{
		"Title": "Chat with Jor-El",
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	s.tmpls["chat.html"].ExecuteTemplate(w, "chat.html", data)
}

func (s *Server) handleChatStream(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query().Get("q")
	if query == "" {
		http.Error(w, "Missing query", http.StatusBadRequest)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming not supported", http.StatusInternalServerError)
		return
	}

	ctx := context.Background()

	ch, err := s.pipeline.Answer(ctx, query)
	if err != nil {
		fmt.Fprintf(w, "data: Error: %s\n\n", err.Error())
		flusher.Flush()
		fmt.Fprintf(w, "data: [DONE]\n\n")
		flusher.Flush()
		return
	}

	for token := range ch {
		fmt.Fprintf(w, "data: %s\n\n", token)
		flusher.Flush()
	}

	fmt.Fprintf(w, "data: [DONE]\n\n")
	flusher.Flush()
}
