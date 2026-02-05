// Copyright Mia srl
// SPDX-License-Identifier: AGPL-3.0-only or Commercial

package logger

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

const (
	IncomingRequestMessage  = "incoming request"
	RequestCompletedMessage = "request completed"
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

func removePort(host string) string {
	return strings.Split(host, ":")[0]
}

func getReqID(ctx *fiber.Ctx) string {
	if requestID := ctx.Get(fiber.HeaderXRequestID, ""); requestID != "" {
		return requestID
	}

	requestID, err := uuid.NewRandom()
	if err != nil {
		panic(fmt.Errorf("error generating request id: %w", err))
	}
	return requestID.String()
}

func getFiberError(err error) *fiber.Error {
	var fiberErr *fiber.Error
	if ok := errors.As(err, &fiberErr); err != nil && ok {
		return fiberErr
	}
	return nil
}

func BodySize(ctx *fiber.Ctx, err error) int {
	if fiberErr := getFiberError(err); fiberErr != nil {
		return len(fiberErr.Error())
	}

	if content := ctx.GetRespHeader("Content-Length"); content != "" {
		if length, err := strconv.Atoi(content); err == nil {
			return length
		}
	}
	return len(ctx.Response().Body())
}

func StatusCode(ctx *fiber.Ctx, err error) int {
	if fiberErr := getFiberError(err); fiberErr != nil {
		return fiberErr.Code
	}

	return ctx.Response().StatusCode()
}

func logIncomingRequest(ctx *fiber.Ctx, logger Logger) {
	logger.
		Trace(IncomingRequestMessage,
			"http", httpCall{
				Request: &request{
					Method: ctx.Method(),
					UserAgent: userAgent{
						Original: ctx.Get("user-agent", ""),
					},
				},
			},
			"url", url{Path: string(ctx.Request().URI().RequestURI())},
			"host", host{
				ForwardedHost: ctx.Get(fiber.HeaderXForwardedHost, ""),
				Hostname:      removePort(string(ctx.Request().Host())),
				IP:            ctx.Get(fiber.HeaderXForwardedFor, ""),
			},
		)
}

func logRequestCompleted(ctx *fiber.Ctx, logger Logger, startTime time.Time, err error) {
	logger.
		Info(RequestCompletedMessage,
			"http", httpCall{
				Request: &request{
					Method: ctx.Method(),
					UserAgent: userAgent{
						Original: ctx.Get("user-agent", ""),
					},
				},
				Response: &response{
					StatusCode: StatusCode(ctx, err),
					Body: responseBody{
						Bytes: BodySize(ctx, err),
					},
				},
			},
			"url", url{Path: string(ctx.Request().URI().RequestURI())},
			"host", host{
				ForwardedHost: ctx.Get(fiber.HeaderXForwardedHost, ""),
				Hostname:      removePort(string(ctx.Request().Host())),
				IP:            ctx.Get(fiber.HeaderXForwardedFor, ""),
			},
			"responseTime", float64(time.Since(startTime).Milliseconds()),
		)
}

// RequestMiddlewareLogger is a fiber middleware to log all requests
// It logs the incoming request and when request is completed, adding latency of the request
func RequestMiddlewareLogger(appCtx context.Context, logger Logger, excludedPrefix []string) func(*fiber.Ctx) error {
	return func(fiberCtx *fiber.Ctx) error {
		for _, prefix := range excludedPrefix {
			if strings.HasPrefix(string(fiberCtx.Request().URI().RequestURI()), prefix) {
				return fiberCtx.Next()
			}
		}

		start := time.Now()

		requestID := getReqID(fiberCtx)
		loggerWithReqID := logger.WithName("server:request:" + requestID)
		logIncomingRequest(fiberCtx, loggerWithReqID)

		ctx := WithContext(appCtx, logger)
		fiberCtx.SetUserContext(ctx)
		err := fiberCtx.Next()

		logRequestCompleted(fiberCtx, loggerWithReqID, start, err)
		return err
	}
}
