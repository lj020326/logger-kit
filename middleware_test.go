package logger

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gofiber/fiber/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDefaultMiddlewareConfig(t *testing.T) {
	cfg := DefaultMiddlewareConfig()

	assert.Equal(t, InfoLevel, cfg.LogLevel)
	assert.Equal(t, WarnLevel, cfg.WarnLevel)
	assert.Equal(t, ErrorLevel, cfg.ErrorLevel)
	assert.True(t, cfg.IncludeRequestID)
	assert.Equal(t, "X-Request-ID", cfg.RequestIDHeader)
	assert.True(t, cfg.IncludeLatency)
	assert.True(t, cfg.IncludeQuery)
	assert.Equal(t, 1024, cfg.MaxBodySize)
	assert.NotEmpty(t, cfg.SensitiveHeaders)
}

func TestMiddleware_Basic(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{Logger: logger})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("OK"))
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	middleware(handler).ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)

	output := buf.String()
	assert.Contains(t, output, "GET")
	assert.Contains(t, output, "/test")
	assert.Contains(t, output, "200")
}

func TestMiddleware_SkipPaths(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:    logger,
		SkipPaths: []string{"/health", "/metrics"},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Skipped path
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)
	assert.Empty(t, buf.String())

	// Logged path
	buf.Reset()
	req = httptest.NewRequest(http.MethodGet, "/api/users", nil)
	rec = httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)
	assert.NotEmpty(t, buf.String())
}

func TestMiddleware_SkipFunc(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger: logger,
		SkipFunc: func(r *http.Request) bool {
			return r.Header.Get("Skip-Logging") == "true"
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	// Skipped request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Skip-Logging", "true")
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)
	assert.Empty(t, buf.String())

	// Logged request
	buf.Reset()
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	rec = httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)
	assert.NotEmpty(t, buf.String())
}

func TestMiddleware_RequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:           logger,
		IncludeRequestID: true,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Check that request ID is in context
		id := RequestIDFromRequest(r)
		assert.NotEmpty(t, id)
		w.WriteHeader(http.StatusOK)
	})

	// Without existing request ID
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	assert.NotEmpty(t, rec.Header().Get("X-Request-ID"))
	assert.Contains(t, buf.String(), "request_id")

	// With existing request ID
	buf.Reset()
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "custom-request-id")
	rec = httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	assert.Equal(t, "custom-request-id", rec.Header().Get("X-Request-ID"))
	assert.Contains(t, buf.String(), "custom-request-id")
}

func TestMiddleware_CustomRequestIDGenerator(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:           logger,
		IncludeRequestID: true,
		GenerateRequestID: func() string {
			return "custom-generated-id"
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	assert.Equal(t, "custom-generated-id", rec.Header().Get("X-Request-ID"))
}

func TestMiddleware_LogLevelsByStatus(t *testing.T) {
	tests := []struct {
		status        int
		expectedLevel string
	}{
		{200, "info"},
		{201, "info"},
		{400, "warn"},
		{404, "warn"},
		{500, "error"},
		{503, "error"},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(Config{
				Level:  TraceLevel,
				Output: &buf,
				Format: FormatJSON,
			})

			middleware := Middleware(MiddlewareConfig{Logger: logger})

			handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(tt.status)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			rec := httptest.NewRecorder()
			middleware(handler).ServeHTTP(rec, req)

			var result map[string]interface{}
			err := json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedLevel, result["level"])
		})
	}
}

func TestMiddleware_IncludeQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:       logger,
		IncludeQuery: true,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&baz=qux", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "foo=bar")
	assert.Contains(t, output, "baz=qux")
}

func TestMiddleware_SensitiveQueryParams(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:               logger,
		IncludeQuery:         true,
		SensitiveQueryParams: []string{}, // use default list
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&password=secret&token=xyz", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "foo=bar")
	assert.Contains(t, output, "password=***")
	assert.Contains(t, output, "token=***")
	assert.NotContains(t, output, "secret")
	assert.NotContains(t, output, "xyz")
}

func TestMiddleware_IncludeHeaders(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:         logger,
		IncludeHeaders: true,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Authorization", "Bearer secret-token")
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "X-Custom-Header")
	assert.Contains(t, output, "custom-value")
	assert.Contains(t, output, "[REDACTED]")
	assert.NotContains(t, output, "secret-token")
}

func TestMiddleware_IncludeBody(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: 100,
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"key":"value"}`, string(body))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "request_body")
	assert.Contains(t, output, "key")
}

func TestMiddleware_IncludeBody_Truncated(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: 10,
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		assert.Equal(t, `{"key":"this is a very long value that exceeds the max body size"}`, string(body))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"key":"this is a very long value that exceeds the max body size"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "[truncated]")
}

func TestMiddleware_CustomFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	middleware := Middleware(MiddlewareConfig{
		Logger: logger,
		CustomFields: func(r *http.Request) map[string]interface{} {
			return map[string]interface{}{
				"custom_field": "custom_value",
				"request_path": r.URL.Path,
			}
		},
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "custom_field")
	assert.Contains(t, output, "custom_value")
}

func TestMiddleware_DefaultLogger(t *testing.T) {
	middleware := Middleware(MiddlewareConfig{})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rec := httptest.NewRecorder()

	// Should not panic
	assert.NotPanics(t, func() {
		middleware(handler).ServeHTTP(rec, req)
	})
}

func TestFiberMiddleware_Basic(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{Logger: logger}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, http.StatusOK, resp.StatusCode)
	output := buf.String()
	assert.Contains(t, output, "GET")
	assert.Contains(t, output, "/test")
}

func TestFiberMiddleware_SkipPaths(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:    logger,
		SkipPaths: []string{"/health"},
	}))
	app.Get("/health", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})
	app.Get("/api", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Skipped path
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
	assert.Empty(t, buf.String())

	// Logged path
	req = httptest.NewRequest(http.MethodGet, "/api", nil)
	_, err = app.Test(req)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestFiberMiddleware_RequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:           logger,
		IncludeRequestID: true,
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		id := RequestIDFromFiberCtx(c)
		assert.NotEmpty(t, id)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.NotEmpty(t, resp.Header.Get("X-Request-ID"))
	assert.Contains(t, buf.String(), "request_id")
}

func TestFiberMiddleware_LogLevelsByStatus(t *testing.T) {
	tests := []struct {
		status        int
		expectedLevel string
	}{
		{200, "info"},
		{400, "warn"},
		{500, "error"},
	}

	for _, tt := range tests {
		t.Run(http.StatusText(tt.status), func(t *testing.T) {
			var buf bytes.Buffer
			logger := New(Config{
				Level:  TraceLevel,
				Output: &buf,
				Format: FormatJSON,
			})

			app := fiber.New()
			app.Use(FiberMiddleware(MiddlewareConfig{Logger: logger}))
			app.Get("/test", func(c fiber.Ctx) error {
				return c.SendStatus(tt.status)
			})

			req := httptest.NewRequest(http.MethodGet, "/test", nil)
			_, err := app.Test(req)
			require.NoError(t, err)

			var result map[string]interface{}
			err = json.Unmarshal(buf.Bytes(), &result)
			require.NoError(t, err)

			assert.Equal(t, tt.expectedLevel, result["level"])
		})
	}
}

func TestLoggerFromFiberCtx(t *testing.T) {
	var buf bytes.Buffer
	expectedLogger := New(Config{
		Level:  DebugLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{Logger: expectedLogger}))
	app.Get("/test", func(c fiber.Ctx) error {
		logger := LoggerFromFiberCtx(c)
		assert.Equal(t, expectedLogger, logger)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestLoggerFromFiberCtx_NotFound(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		logger := LoggerFromFiberCtx(c)
		assert.Equal(t, defaultLogger, logger)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestRequestIDFromFiberCtx(t *testing.T) {
	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{IncludeRequestID: true}))
	app.Get("/test", func(c fiber.Ctx) error {
		id := RequestIDFromFiberCtx(c)
		assert.NotEmpty(t, id)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestRequestIDFromFiberCtx_NotFound(t *testing.T) {
	app := fiber.New()
	app.Get("/test", func(c fiber.Ctx) error {
		id := RequestIDFromFiberCtx(c)
		assert.Empty(t, id)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestCtxFiber(t *testing.T) {
	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{IncludeRequestID: true}))
	app.Get("/test", func(c fiber.Ctx) error {
		logger := CtxFiber(c)
		assert.NotNil(t, logger)
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)
}

func TestGenerateUUID(t *testing.T) {
	uuid := generateUUID()
	assert.Len(t, uuid, 36)
	assert.Equal(t, '-', rune(uuid[8]))
	assert.Equal(t, '-', rune(uuid[13]))
	assert.Equal(t, '4', rune(uuid[14]))
	assert.Equal(t, '-', rune(uuid[18]))
	assert.Equal(t, '-', rune(uuid[23]))
}

func TestGenerateUUID_Unique(t *testing.T) {
	uuids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		uuid := generateUUID()
		assert.False(t, uuids[uuid], "UUID should be unique")
		uuids[uuid] = true
	}
}

func TestFiberMiddleware_SkipFunc(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger: logger,
		SkipFuncFiber: func(c fiber.Ctx) bool {
			return c.Get("Skip-Logging") == "true"
		},
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Skipped request
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("Skip-Logging", "true")
	_, err := app.Test(req)
	require.NoError(t, err)
	assert.Empty(t, buf.String())

	// Logged request
	buf.Reset()
	req = httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err = app.Test(req)
	require.NoError(t, err)
	assert.NotEmpty(t, buf.String())
}

func TestFiberMiddleware_IncludeQuery(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:       logger,
		IncludeQuery: true,
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test?foo=bar&baz=qux", nil)
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "foo=bar")
	assert.Contains(t, output, "baz=qux")
}

func TestFiberMiddleware_IncludeHeaders(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:         logger,
		IncludeHeaders: true,
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Custom-Header", "custom-value")
	req.Header.Set("Authorization", "Bearer secret-token")
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "X-Custom-Header")
	assert.Contains(t, output, "custom-value")
	assert.Contains(t, output, "[REDACTED]")
	assert.NotContains(t, output, "secret-token")
}

func TestFiberMiddleware_IncludeBody(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: 100,
	}))
	app.Post("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Small body
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"key":"value"}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "request_body")
	assert.Contains(t, output, "key")
}

func TestFiberMiddleware_IncludeBody_Truncated(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: 10, // Very small limit
	}))
	app.Post("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Large body that will be truncated
	req := httptest.NewRequest(http.MethodPost, "/test", strings.NewReader(`{"key":"this is a very long value that exceeds the max body size"}`))
	req.Header.Set("Content-Type", "application/json")
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "[truncated]")
}

func TestFiberMiddleware_CustomFields(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger: logger,
		CustomFieldsFiber: func(c fiber.Ctx) map[string]interface{} {
			return map[string]interface{}{
				"custom_field": "custom_value",
			}
		},
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "custom_field")
	assert.Contains(t, output, "custom_value")
}

func TestFiberMiddleware_WithError(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{Logger: logger}))
	app.Get("/test", func(c fiber.Ctx) error {
		return fiber.NewError(fiber.StatusInternalServerError, "test error")
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	_, err := app.Test(req)
	require.NoError(t, err)

	output := buf.String()
	assert.Contains(t, output, "error")
}

func TestFiberMiddleware_ExistingRequestID(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:           logger,
		IncludeRequestID: true,
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	// Request with existing request ID
	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Request-ID", "existing-request-id")
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, "existing-request-id", resp.Header.Get("X-Request-ID"))
	assert.Contains(t, buf.String(), "existing-request-id")
}

func TestFiberMiddleware_CustomRequestIDGenerator(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	app := fiber.New()
	app.Use(FiberMiddleware(MiddlewareConfig{
		Logger:           logger,
		IncludeRequestID: true,
		GenerateRequestID: func() string {
			return "custom-generated-id"
		},
	}))
	app.Get("/test", func(c fiber.Ctx) error {
		return c.SendStatus(fiber.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	resp, err := app.Test(req)
	require.NoError(t, err)

	assert.Equal(t, "custom-generated-id", resp.Header.Get("X-Request-ID"))
}

// Test redactQuery with nil sensitiveKeys (no redaction) via Middleware.
func TestMiddleware_Query_NilSensitiveParams(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{
		Level:  InfoLevel,
		Output: &buf,
		Format: FormatJSON,
	})

	// Disabling redaction is now an explicit flag. Spelling it as a nil slice
	// did not survive a round trip through JSON or YAML, where an omitted
	// field unmarshals to nil -- so deserialising a config turned redaction
	// off without anyone asking.
	middleware := Middleware(MiddlewareConfig{
		Logger:                logger,
		IncludeQuery:          true,
		DisableQueryRedaction: true,
	})

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/test?token=secret123&foo=bar", nil)
	rec := httptest.NewRecorder()
	middleware(handler).ServeHTTP(rec, req)

	output := buf.String()
	assert.Contains(t, output, "token=secret123", "DisableQueryRedaction should not redact")
	assert.Contains(t, output, "foo=bar")
}
