package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"spark/internal/compat/proxyutil"
)

type compatProxyServer struct {
	server      *http.Server
	listener    net.Listener
	mux         *http.ServeMux
	baseURL     string
	client      *http.Client
	quietStderr bool
	logFile     io.WriteCloser
	logMu       sync.Mutex
	logPath     string
	logPrefix   string
	closeOnce   sync.Once
	closeErr    error
	restore     func()
}

func newCompatProxyServer(openLogFile func() (io.WriteCloser, string, error), logPrefix string, quietStderr bool) (*compatProxyServer, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	logFile, logPath, err := openLogFile()
	if err != nil {
		_ = ln.Close()
		return nil, err
	}
	return &compatProxyServer{
		listener:    ln,
		mux:         http.NewServeMux(),
		baseURL:     "http://" + ln.Addr().String(),
		client:      proxyutil.NewStreamingHTTPClient(),
		quietStderr: quietStderr,
		logFile:     logFile,
		logPath:     logPath,
		logPrefix:   logPrefix,
	}, nil
}

func (s *compatProxyServer) handleFunc(pattern string, handler func(http.ResponseWriter, *http.Request)) {
	s.mux.HandleFunc(pattern, handler)
}

func (s *compatProxyServer) start() {
	s.server = &http.Server{Handler: s.mux}
	go func() {
		_ = s.server.Serve(s.listener)
	}()
}

func (s *compatProxyServer) BaseURL() string {
	return s.baseURL
}

func (s *compatProxyServer) LogPath() string {
	return s.logPath
}

func (s *compatProxyServer) Close() error {
	if s == nil {
		return nil
	}
	s.closeOnce.Do(func() {
		if s.restore != nil {
			s.restore()
			s.restore = nil
		}
		if s.server != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			s.closeErr = s.server.Shutdown(ctx)
			cancel()
		} else if s.listener != nil {
			s.closeErr = s.listener.Close()
		}
		s.logMu.Lock()
		if s.logFile != nil {
			_ = s.logFile.Close()
			s.logFile = nil
		}
		s.logMu.Unlock()
	})
	return s.closeErr
}

func (s *compatProxyServer) logf(format string, args ...any) {
	line := fmt.Sprintf("["+s.logPrefix+"] "+format, args...)
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if s.logFile != nil {
		_, _ = fmt.Fprintf(s.logFile, "%s %s\n", time.Now().Format(time.RFC3339), line)
	}
}

func (s *compatProxyServer) warnf(summary string) {
	if s.quietStderr {
		return
	}
	fmt.Fprintf(os.Stderr, "[%s] %s (details: %s)\n", s.logPrefix, summary, s.logPath)
}
