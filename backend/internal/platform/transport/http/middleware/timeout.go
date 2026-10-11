package middleware

import (
	"bytes"
	"context"
	"net/http"
	"sync"
	"time"

	"example.com/nexora/backend/internal/platform/apperr"
)

type timeoutWriter struct {
	underlying  http.ResponseWriter
	h           http.Header
	buf         bytes.Buffer
	code        int
	mu          sync.Mutex
	timedOut    bool
	wroteHeader bool
}

func newTimeoutWriter(w http.ResponseWriter) *timeoutWriter {
	h := w.Header().Clone()
	if h == nil {
		h = make(http.Header)
	}
	return &timeoutWriter{
		underlying: w,
		h:          h,
	}
}

func (tw *timeoutWriter) Header() http.Header {
	return tw.h
}

func (tw *timeoutWriter) WriteHeader(code int) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut || tw.wroteHeader {
		return
	}
	tw.wroteHeader = true
	tw.code = code
}

func (tw *timeoutWriter) Write(b []byte) (int, error) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	if tw.timedOut {
		return 0, http.ErrHandlerTimeout
	}
	if !tw.wroteHeader {
		tw.wroteHeader = true
		tw.code = http.StatusOK
	}
	return tw.buf.Write(b)
}

// Timeout wraps the request in a bounded context timeout, returning canonical 504 on deadline expiry.
func Timeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if timeout <= 0 {
				next.ServeHTTP(w, r)
				return
			}

			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()

			tw := newTimeoutWriter(w)
			r = r.WithContext(ctx)

			panicChan := make(chan any, 1)
			done := make(chan struct{})
			go func() {
				defer func() {
					if p := recover(); p != nil {
						panicChan <- p
					}
					close(done)
				}()
				next.ServeHTTP(tw, r)
			}()

			select {
			case p := <-panicChan:
				panic(p)
			case <-done:
				select {
				case p := <-panicChan:
					panic(p)
				default:
				}
				tw.mu.Lock()
				defer tw.mu.Unlock()
				if !tw.timedOut {
					dst := tw.underlying.Header()
					for k, vv := range tw.h {
						dst[k] = vv
					}
					code := tw.code
					if code == 0 {
						code = http.StatusOK
					}
					tw.underlying.WriteHeader(code)
					_, _ = tw.underlying.Write(tw.buf.Bytes())
				}
			case <-ctx.Done():
				tw.mu.Lock()
				tw.timedOut = true
				tw.mu.Unlock()

				reqID := GetRequestID(r.Context())
				apperr.RespondError(w, reqID, apperr.NewTimeoutError("The request processing exceeded timeout deadline"))
			}
		})
	}
}
