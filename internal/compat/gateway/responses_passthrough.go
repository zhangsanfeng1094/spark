package gateway

import (
	"bufio"
	"io"
	"net/http"
	"strings"
)

func ForwardResponsesPassthrough(w http.ResponseWriter, upResp *http.Response, logf func(format string, args ...any)) {
	copyResponseHeaders(w.Header(), upResp.Header)
	w.WriteHeader(upResp.StatusCode)
	flusher, _ := w.(http.Flusher)
	contentType := strings.ToLower(upResp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/event-stream") {
		reader := bufio.NewReader(upResp.Body)
		for {
			line, err := reader.ReadBytes('\n')
			if len(line) > 0 {
				_, _ = w.Write(line)
				if flusher != nil {
					flusher.Flush()
				}
			}
			if err != nil {
				if err == io.EOF {
					return
				}
				if logf != nil {
					logf("responses passthrough stream read failed: %v", err)
				}
				return
			}
		}
	}
	if _, err := io.Copy(w, upResp.Body); err != nil && logf != nil {
		logf("responses passthrough copy failed: %v", err)
	}
}

func copyResponseHeaders(dst, src http.Header) {
	for k, values := range src {
		for _, v := range values {
			dst.Add(k, v)
		}
	}
}
