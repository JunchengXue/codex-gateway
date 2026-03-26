package server

import (
	"context"
	"io"
	"log/slog"
	"net/http"
)

type UpstreamClient interface {
	Do(ctx context.Context, method, path string, body []byte, contentType string, headers map[string]string) (*http.Response, error)
}

type Dependencies struct {
	FixedAPIKey    string
	ResponsesPath  string
	Originator     string
	Logger         *slog.Logger
	UpstreamClient UpstreamClient
}

type Server struct {
	deps   Dependencies
	logger *slog.Logger
}

func New(deps Dependencies) http.Handler {
	if deps.ResponsesPath == "" {
		deps.ResponsesPath = "/backend-api/codex/responses"
	}
	if deps.Originator == "" {
		deps.Originator = "opencode"
	}

	mux := http.NewServeMux()
	srv := &Server{deps: deps, logger: deps.Logger}

	mux.HandleFunc("/healthz", srv.handleHealthz)

	auth := AuthMiddleware(deps.FixedAPIKey)
	mux.Handle("/v1/models", auth(http.HandlerFunc(srv.handleModels)))
	mux.Handle("/v1/chat/completions", auth(http.HandlerFunc(srv.handleChatCompletions)))
	mux.Handle("/v1/responses", auth(http.HandlerFunc(srv.handleResponses)))

	return mux
}

func relayUpstreamResponse(w http.ResponseWriter, resp *http.Response) {
	defer resp.Body.Close()

	for k, vals := range resp.Header {
		for _, v := range vals {
			w.Header().Add(k, v)
		}
	}

	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}
