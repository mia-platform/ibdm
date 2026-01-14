// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	forwardedHostHeaderKey = "x-forwarded-host"
	forwardedForHeaderKey  = "x-forwarded-for"
	requestIDHeaderName    = "x-request-id"

	IncomingRequestMessage  = "incoming request"
	RequestCompletedMessage = "request completed"
)

type fiberLoggingContext struct {
	c          *fiber.Ctx
	handlerErr error
}

type loggingContext interface {
	Request() requestLoggingContext
	Response() responseLoggingContext
}

type requestLoggingContext interface {
	GetHeader(string) string
	URI() string
	Host() string
	Method() string
}

type responseLoggingContext interface {
	BodySize() int
	StatusCode() int
}

// http is the struct of the log formatter.
type http struct {
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

func removePort(host string) string {
	return strings.Split(host, ":")[0]
}

func GetReqID(ctx loggingContext) string {
	if requestID := ctx.Request().GetHeader(requestIDHeaderName); requestID != "" {
		return requestID
	}
	// Generate a random uuid string. e.g. 16c9c1f2-c001-40d3-bbfe-48857367e7b5
	requestID, err := uuid.NewRandom()
	if err != nil {
		panic(fmt.Errorf("error generating request id: %w", err))
	}
	return requestID.String()
}

func logIncomingRequest[T any](ctx loggingContext, logger Logger) {
	logger.
		WithName("incoming_request").
		Trace(IncomingRequestMessage,
			"http", http{
				Request: &request{
					Method: ctx.Request().Method(),
					UserAgent: userAgent{
						Original: ctx.Request().GetHeader("user-agent"),
					},
				},
			},
			"url", url{Path: ctx.Request().URI()},
			"host", host{
				ForwardedHost: ctx.Request().GetHeader(forwardedHostHeaderKey),
				Hostname:      removePort(ctx.Request().Host()),
				IP:            ctx.Request().GetHeader(forwardedForHeaderKey),
			},
		)
}

func logRequestCompleted(ctx loggingContext, logger Logger, startTime time.Time) {
	logger.
		WithName("request_completed").
		Info(RequestCompletedMessage,
			"http", http{
				Request: &request{
					Method: ctx.Request().Method(),
					UserAgent: userAgent{
						Original: ctx.Request().GetHeader("user-agent"),
					},
				},
				Response: &response{
					StatusCode: ctx.Response().StatusCode(),
					Body: responseBody{
						Bytes: ctx.Response().BodySize(),
					},
				},
			},
			"url", url{Path: ctx.Request().URI()},
			"host", host{
				ForwardedHost: ctx.Request().GetHeader(forwardedHostHeaderKey),
				Hostname:      removePort(ctx.Request().Host()),
				IP:            ctx.Request().GetHeader(forwardedForHeaderKey),
			},
			"responseTime", float64(time.Since(startTime).Milliseconds()),
		)
}

func (flc *fiberLoggingContext) Request() requestLoggingContext {
	return flc
}

func (flc *fiberLoggingContext) Response() responseLoggingContext {
	return flc
}

func (flc *fiberLoggingContext) GetHeader(key string) string {
	return flc.c.Get(key, "")
}

func (flc *fiberLoggingContext) URI() string {
	return string(flc.c.Request().URI().RequestURI())
}

func (flc *fiberLoggingContext) Host() string {
	return string(flc.c.Request().Host())
}

func (flc *fiberLoggingContext) Method() string {
	return flc.c.Method()
}

func (flc fiberLoggingContext) getFiberError() *fiber.Error {
	if fiberErr, ok := flc.handlerErr.(*fiber.Error); flc.handlerErr != nil && ok {
		return fiberErr
	}
	return nil
}

func (flc *fiberLoggingContext) setError(err error) {
	flc.handlerErr = err
}

func (flc *fiberLoggingContext) BodySize() int {
	if fiberErr := flc.getFiberError(); fiberErr != nil {
		return len(fiberErr.Error())
	}

	if content := flc.c.GetRespHeader("Content-Length"); content != "" {
		if length, err := strconv.Atoi(content); err == nil {
			return length
		}
	}
	return len(flc.c.Response().Body())
}

func (flc *fiberLoggingContext) StatusCode() int {
	if fiberErr := flc.getFiberError(); fiberErr != nil {
		return fiberErr.Code
	}

	return flc.c.Response().StatusCode()
}

// RequestMiddlewareLogger is a fiber middleware to log all requests
// It logs the incoming request and when request is completed, adding latency of the request
func RequestMiddlewareLogger(logger Logger, excludedPrefix []string) func(*fiber.Ctx) error {
	return func(fiberCtx *fiber.Ctx) error {
		fiberLoggingContext := &fiberLoggingContext{c: fiberCtx}

		for _, prefix := range excludedPrefix {
			if strings.HasPrefix(fiberLoggingContext.Request().URI(), prefix) {
				return fiberCtx.Next()
			}
		}

		start := time.Now()

		requestID := GetReqID(fiberLoggingContext)
		loggerWithReqID := logger.WithName("request").WithName(requestID)

		ctx := WithContext(fiberCtx.UserContext(), loggerWithReqID)
		fiberCtx.SetUserContext(ctx)

		logIncomingRequest[any](fiberLoggingContext, loggerWithReqID)
		err := fiberCtx.Next()
		fiberLoggingContext.setError(err)

		logRequestCompleted(fiberLoggingContext, loggerWithReqID, start)

		return err
	}
}
