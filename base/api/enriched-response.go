package api

import (
	"bufio"
	"errors"
	"net"
	"net/http"

	"github.com/safing/portmaster/base/log"
)

// LoggingResponseWriter is a wrapper for http.ResponseWriter for better request logging.
type LoggingResponseWriter struct {
	ResponseWriter http.ResponseWriter
	Request        *http.Request
	Status         int
}

// NewLoggingResponseWriter wraps a http.ResponseWriter.
func NewLoggingResponseWriter(w http.ResponseWriter, r *http.Request) *LoggingResponseWriter {
	return &LoggingResponseWriter{
		ResponseWriter: w,
		Request:        r,
	}
}

// Header wraps the original Header method.
func (lrw *LoggingResponseWriter) Header() http.Header {
	return lrw.ResponseWriter.Header()
}

// Write wraps the original Write method.
func (lrw *LoggingResponseWriter) Write(b []byte) (int, error) {
	return lrw.ResponseWriter.Write(b)
}

// WriteHeader wraps the original WriteHeader method to extract information.
func (lrw *LoggingResponseWriter) WriteHeader(code int) {
	lrw.Status = code
	lrw.ResponseWriter.WriteHeader(code)
}

// Hijack wraps the original Hijack method, if available.
func (lrw *LoggingResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hijacker, ok := lrw.ResponseWriter.(http.Hijacker)
	if ok {
		c, b, err := hijacker.Hijack()
		if err != nil {
			return nil, nil, err
		}
		log.Tracer(lrw.Request.Context()).Infof("api request: %s HIJ %s", lrw.Request.RemoteAddr, lrw.Request.RequestURI)
		return c, b, nil
	}
	return nil, nil, errors.New("response does not implement http.Hijacker")
}

// RequestLogger is a logging middleware.
func RequestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		log.Tracer(r.Context()).Tracef("api request: %s ___ %s", r.RemoteAddr, r.RequestURI)
		lrw := NewLoggingResponseWriter(w, r)
		next.ServeHTTP(lrw, r)
		if lrw.Status != 0 {
			// request may have been hijacked
			log.Tracer(r.Context()).Infof("api request: %s %d %s", lrw.Request.RemoteAddr, lrw.Status, lrw.Request.RequestURI)
		}
	})
}
