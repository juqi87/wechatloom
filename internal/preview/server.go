package preview

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

type StartRequest struct {
	BuildPath string `json:"build_path"`
	Port      int    `json:"port,omitempty"`
}

type Session interface {
	URL() string
	Close(context.Context) error
}

type localSession struct {
	url    string
	server *http.Server
	once   sync.Once
	err    error
	done   chan struct{}
}

func Start(ctx context.Context, request StartRequest) (Session, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if request.Port < 0 || request.Port > 65535 {
		return nil, fmt.Errorf("preview port must be between 0 and 65535")
	}
	buildPath, err := filepath.Abs(request.BuildPath)
	if err != nil {
		return nil, fmt.Errorf("resolve build path: %w", err)
	}
	manifestPath := filepath.Join(buildPath, "manifest.json")
	if info, err := os.Stat(manifestPath); err != nil || !info.Mode().IsRegular() {
		return nil, fmt.Errorf("completed build manifest is required: %s", manifestPath)
	}
	previewPath := filepath.Join(buildPath, "preview.html")
	previewHTML, err := os.ReadFile(previewPath)
	if err != nil {
		return nil, fmt.Errorf("read completed preview: %w", err)
	}
	type assetFile struct {
		content   []byte
		mediaType string
	}
	assets := map[string]assetFile{}
	assetEntries, err := os.ReadDir(filepath.Join(buildPath, "assets"))
	if err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("read preview assets: %w", err)
	}
	for _, entry := range assetEntries {
		if !entry.Type().IsRegular() {
			continue
		}
		content, err := os.ReadFile(filepath.Join(buildPath, "assets", entry.Name()))
		if err != nil {
			return nil, fmt.Errorf("read preview asset %q: %w", entry.Name(), err)
		}
		assets[entry.Name()] = assetFile{content: content, mediaType: http.DetectContentType(content)}
	}

	handler := http.HandlerFunc(func(writer http.ResponseWriter, incoming *http.Request) {
		writer.Header().Set("Cache-Control", "no-store")
		writer.Header().Set("X-Content-Type-Options", "nosniff")
		if incoming.Method != http.MethodGet && incoming.Method != http.MethodHead {
			writer.Header().Set("Allow", "GET, HEAD")
			writer.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if incoming.URL.Path == "/" {
			writer.Header().Set("Content-Type", "text/html; charset=utf-8")
			writer.WriteHeader(http.StatusOK)
			if incoming.Method == http.MethodGet {
				_, _ = writer.Write(previewHTML)
			}
			return
		}
		const assetPrefix = "/assets/"
		if strings.HasPrefix(incoming.URL.Path, assetPrefix) {
			name := strings.TrimPrefix(incoming.URL.Path, assetPrefix)
			if name == "" || filepath.Base(name) != name {
				http.NotFound(writer, incoming)
				return
			}
			asset, exists := assets[name]
			if !exists {
				http.NotFound(writer, incoming)
				return
			}
			writer.Header().Set("Content-Type", asset.mediaType)
			writer.WriteHeader(http.StatusOK)
			if incoming.Method == http.MethodGet {
				_, _ = writer.Write(asset.content)
			}
			return
		}
		http.NotFound(writer, incoming)
		return
	})
	listener, err := net.Listen("tcp4", "127.0.0.1:"+strconv.Itoa(request.Port))
	if err != nil {
		return nil, fmt.Errorf("listen for local preview: %w", err)
	}
	server := &http.Server{Handler: handler, ErrorLog: log.New(io.Discard, "", 0)}
	session := &localSession{url: "http://" + listener.Addr().String() + "/", server: server, done: make(chan struct{})}
	go func() {
		_ = server.Serve(listener)
	}()
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close(context.Background())
		case <-session.done:
		}
	}()
	return session, nil
}

func (session *localSession) URL() string {
	return session.url
}

func (session *localSession) Close(ctx context.Context) error {
	session.once.Do(func() {
		session.err = session.server.Shutdown(ctx)
		close(session.done)
	})
	return session.err
}
