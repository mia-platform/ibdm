// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	IncomingRequestMessage  = "incoming request"
	RequestCompletedMessage = "request completed"

	// requestIDKey is the log field carrying the request id.
	requestIDKey = "requestId"

	// headerRequestID carries the request id chosen by the caller or by a proxy in front of IBDM.
	headerRequestID = "X-Request-Id"
	// headerForwardedHost carries the host requested by the client when IBDM is behind a proxy.
	headerForwardedHost = "X-Forwarded-Host"
	// headerForwardedFor carries the address of the client when IBDM is behind a proxy.
	headerForwardedFor = "X-Forwarded-For"
	// headerUserAgent carries the user agent of the client.
	headerUserAgent = "User-Agent"
)

// httpCall is the struct of the log formatter.
type httpCall struct {
	Request  *request  `json:"request,omitempty"`
	Response *response `json:"response,omitempty"`
}

type userAgent struct {
	Original string `json:"original,omitempty"`
}

// request contains the items of request info log.
type request struct {
	Method    string    `json:"method,omitempty"`
	UserAgent userAgent `json:"userAgent"`
}

type responseBody struct {
	Bytes int `json:"bytes,omitempty"`
}

// response contains the items of response info log.
type response struct {
	StatusCode int          `json:"statusCode,omitempty"`
	Body       responseBody `json:"body"`
}

// host has the host information.
type host struct {
	Hostname      string `json:"hostname,omitempty"`
	ForwardedHost string `json:"forwardedHost,omitempty"`
	IP            string `json:"ip,omitempty"`
}

// url info
type url struct {
	Path string `json:"path,omitempty"`
}

// responseRecorder wraps an http.ResponseWriter to record the status code and the number of
// body bytes sent to the client.
type responseRecorder struct {
	http.ResponseWriter

	status      int
	bytes       int
	wroteHeader bool
}

// WriteHeader records the final status code and forwards it. Informational (1xx) responses are
// forwarded without being recorded, because the final status is still to come.
func (r *responseRecorder) WriteHeader(code int) {
	if !r.wroteHeader && code >= http.StatusOK {
		r.status = code
		r.wroteHeader = true
	}
	r.ResponseWriter.WriteHeader(code)
}

// Write forwards b and counts the bytes actually written. A body written without an explicit
// status code is sent with 200, as net/http does.
func (r *responseRecorder) Write(b []byte) (int, error) {
	if !r.wroteHeader {
		r.WriteHeader(http.StatusOK)
	}
	n, err := r.ResponseWriter.Write(b)
	r.bytes += n
	return n, err
}

// Unwrap returns the wrapped writer, so that http.ResponseController reaches its optional
// features (flush, deadlines) through the recorder.
func (r *responseRecorder) Unwrap() http.ResponseWriter {
	return r.ResponseWriter
}

// statusCode returns the status code sent to the client: 200 when the handler wrote nothing.
func (r *responseRecorder) statusCode() int {
	if !r.wroteHeader {
		return http.StatusOK
	}
	return r.status
}

func removePort(host string) string {
	return strings.Split(host, ":")[0]
}

func getReqID(r *http.Request) string {
	if requestID := r.Header.Get(headerRequestID); requestID != "" {
		return requestID
	}

	requestID, err := uuid.NewRandom()
	if err != nil {
		panic(fmt.Errorf("error generating request id: %w", err))
	}
	return requestID.String()
}

// requestPath returns the path of the request as received, without its query string: a query can
// carry credentials, such as a token passed as a parameter, and must never reach the logs.
func requestPath(r *http.Request) string {
	return r.URL.EscapedPath()
}

func requestHost(r *http.Request) host {
	return host{
		ForwardedHost: r.Header.Get(headerForwardedHost),
		Hostname:      removePort(r.Host),
		IP:            r.Header.Get(headerForwardedFor),
	}
}

func logIncomingRequest(r *http.Request, logger Logger) {
	logger.
		Trace(IncomingRequestMessage,
			"http", httpCall{
				Request: &request{
					Method: r.Method,
					UserAgent: userAgent{
						Original: r.Header.Get(headerUserAgent),
					},
				},
			},
			"url", url{Path: requestPath(r)},
			"host", requestHost(r),
		)
}

func logRequestCompleted(r *http.Request, recorder *responseRecorder, logger Logger, startTime time.Time) {
	logger.
		Info(RequestCompletedMessage,
			"http", httpCall{
				Request: &request{
					Method: r.Method,
					UserAgent: userAgent{
						Original: r.Header.Get(headerUserAgent),
					},
				},
				Response: &response{
					StatusCode: recorder.statusCode(),
					Body: responseBody{
						Bytes: recorder.bytes,
					},
				},
			},
			"url", url{Path: requestPath(r)},
			"host", requestHost(r),
			"responseTime", float64(time.Since(startTime).Milliseconds()),
		)
}

// RequestMiddlewareLogger returns an HTTP middleware that logs every request: the incoming request
// at Trace level and its completion, with status code, response size and latency, at Info level.
// Requests whose path starts with one of excludedPrefix are served without being logged.
// The logger of the request is stored in the request context, where handlers find it with
// FromContext; excluded requests carry logger itself. It is named after the request id and adds it
// to every entry as the requestId field, which is kept when handlers rename the logger.
func RequestMiddlewareLogger(logger Logger, excludedPrefix []string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, prefix := range excludedPrefix {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r.WithContext(WithContext(r.Context(), logger)))
					return
				}
			}

			start := time.Now()

			requestID := getReqID(r)
			loggerWithReqID := logger.With(requestIDKey, requestID).WithName("server:request:" + requestID)
			r = r.WithContext(WithContext(r.Context(), loggerWithReqID))
			logIncomingRequest(r, loggerWithReqID)

			recorder := &responseRecorder{ResponseWriter: w}
			next.ServeHTTP(recorder, r)

			logRequestCompleted(r, recorder, loggerWithReqID, start)
		})
	}
}
