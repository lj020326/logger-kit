package logger

import (
	"bytes"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/rs/zerolog"
)

type fiberContextKey uint8

const (
	fiberLoggerKey fiberContextKey = iota
	fiberRequestIDKey
)

// MiddlewareConfig configures the logging middleware.
type MiddlewareConfig struct {
	// Logger is the logger to use.
	// If nil, the default logger is used.
	Logger *Logger

	// SkipPaths is a list of paths to skip logging.
	// Useful for health check endpoints.
	SkipPaths []string

	// SkipFunc is a function to determine if logging should be skipped.
	// If it returns true, the request is not logged.
	SkipFunc func(r *http.Request) bool

	// SkipFuncFiber is a function to determine if logging should be skipped (Fiber).
	SkipFuncFiber func(c fiber.Ctx) bool

	// LogLevel is the log level for successful requests (status < 400).
	// Default: InfoLevel
	LogLevel Level

	// WarnLevel is the log level for client errors (400-499).
	// Default: WarnLevel
	WarnLevel Level

	// ErrorLevel is the log level for server errors (500+).
	// Default: ErrorLevel
	ErrorLevel Level

	// IncludeRequestID adds request ID to the log and context.
	// Default: true
	IncludeRequestID bool

	// RequestIDHeader is the header to read/write request ID.
	// Default: "X-Request-ID"
	RequestIDHeader string

	// GenerateRequestID generates a new request ID if not present.
	// If nil, a UUID is generated.
	GenerateRequestID func() string

	// IncludeLatency includes request latency in logs.
	// Default: true
	IncludeLatency bool

	// IncludeHeaders includes request headers in logs.
	// Default: false
	IncludeHeaders bool

	// SensitiveHeaders is a list of header names to mask in logs.
	// Default: ["Authorization", "X-API-Key", "Cookie", "Set-Cookie"]
	SensitiveHeaders []string

	// IncludeQuery includes query parameters in logs.
	// Default: true
	IncludeQuery bool

	// SensitiveQueryParams is a list of query parameter names to redact (value
	// replaced with "***"). When empty, defaultSensitiveQueryParams is used.
	//
	// Use DisableQueryRedaction to turn redaction off. This used to be spelled
	// by passing nil rather than an empty slice -- a distinction that does not
	// survive a round trip through JSON or YAML, where an omitted field
	// unmarshals to nil, so deserialising a config silently disabled redaction.
	SensitiveQueryParams []string

	// DisableQueryRedaction logs query strings verbatim.
	DisableQueryRedaction bool

	// SensitiveBodyFields is a list of field names to redact inside a logged
	// request body. When empty, defaultSensitiveBodyFields is used.
	//
	// JSON and form-encoded bodies are redacted field by field; a body in any
	// other format is replaced wholesale, since there is no structure to
	// redact selectively.
	SensitiveBodyFields []string

	// DisableBodyRedaction logs request bodies verbatim.
	//
	// Only set this where bodies are known not to carry credentials. The
	// password in a JSON or form login request lives in the body, not in the
	// query string or the headers.
	DisableBodyRedaction bool

	// IncludeBody includes request body in logs. The body is redacted unless
	// DisableBodyRedaction is set; see SensitiveBodyFields.
	// Warning: This may log sensitive data.
	// Default: false
	IncludeBody bool

	// MaxBodySize is the maximum body size to log (in bytes).
	// Bodies larger than this are truncated.
	// Default: 1024
	MaxBodySize int

	// CustomFields adds custom fields to each log entry.
	CustomFields func(r *http.Request) map[string]interface{}

	// CustomFieldsFiber adds custom fields to each log entry (Fiber).
	CustomFieldsFiber func(c fiber.Ctx) map[string]interface{}

	// TrustedProxies is a list of proxy IPs (or CIDRs). When non-empty, X-Forwarded-For
	// and X-Real-IP are only used for the "ip" log field when the direct peer is in this list.
	// When empty (default), only RemoteAddr is used. Set this when behind a reverse proxy.
	TrustedProxies []string
}

// DefaultMiddlewareConfig returns the default middleware configuration.
func DefaultMiddlewareConfig() MiddlewareConfig {
	return MiddlewareConfig{
		LogLevel:         InfoLevel,
		WarnLevel:        WarnLevel,
		ErrorLevel:       ErrorLevel,
		IncludeRequestID: true,
		RequestIDHeader:  "X-Request-ID",
		IncludeLatency:   true,
		IncludeQuery:     true,
		MaxBodySize:      1024,
		SensitiveHeaders: []string{
			"Authorization",
			"X-API-Key",
			"X-Signature",
			"Cookie",
			"Set-Cookie",
		},
		SensitiveQueryParams: defaultSensitiveQueryParams,
	}
}

// defaultSensitiveQueryParams are query keys redacted in logs when SensitiveQueryParams is empty.
var defaultSensitiveQueryParams = []string{
	"password", "token", "code", "secret", "key", "api_key", "apikey",
	"access_token", "refresh_token", "session", "session_id",
}

// redactQuery redacts sensitive query parameters. An empty sensitiveKeys means
// nothing is redacted; callers pass the default list when they want redaction.
func redactQuery(rawQuery string, sensitiveKeys []string) string {
	if rawQuery == "" {
		return ""
	}
	if len(sensitiveKeys) == 0 {
		return rawQuery
	}
	keysMap := make(map[string]bool)
	for _, k := range sensitiveKeys {
		keysMap[strings.ToLower(strings.TrimSpace(k))] = true
	}
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return "[UNPARSEABLE QUERY REDACTED]"
	}
	var buf strings.Builder
	for k, v := range vals {
		keyLower := strings.ToLower(k)
		if keysMap[keyLower] {
			buf.WriteString(url.QueryEscape(k))
			buf.WriteString("=***&")
		} else {
			for _, vv := range v {
				buf.WriteString(url.QueryEscape(k))
				buf.WriteString("=")
				buf.WriteString(url.QueryEscape(vv))
				buf.WriteString("&")
			}
		}
	}
	s := buf.String()
	if len(s) > 0 {
		s = s[:len(s)-1]
	}
	return s
}

// responseWriter wraps http.ResponseWriter to capture status code.
type responseWriter struct {
	http.ResponseWriter
	status int
	size   int
}

func (rw *responseWriter) WriteHeader(status int) {
	rw.status = status
	rw.ResponseWriter.WriteHeader(status)
}

func (rw *responseWriter) Write(b []byte) (int, error) {
	size, err := rw.ResponseWriter.Write(b)
	rw.size += size
	return size, err
}

// Middleware creates a standard net/http logging middleware.
func Middleware(cfg MiddlewareConfig) func(http.Handler) http.Handler {
	if cfg.Logger == nil {
		cfg.Logger = defaultLogger
	}
	if cfg.RequestIDHeader == "" {
		cfg.RequestIDHeader = "X-Request-ID"
	}
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = 1024
	}
	// Apply default log levels if not set (zero value is DebugLevel)
	// We check if all three are at their zero value to avoid overwriting intentional DebugLevel
	defaults := DefaultMiddlewareConfig()
	if cfg.LogLevel == DebugLevel && cfg.WarnLevel == DebugLevel && cfg.ErrorLevel == DebugLevel {
		cfg.LogLevel = defaults.LogLevel
		cfg.WarnLevel = defaults.WarnLevel
		cfg.ErrorLevel = defaults.ErrorLevel
	}

	skipPathMap := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipPathMap[p] = true
	}

	sensitiveHeaderMap := make(map[string]bool)
	if len(cfg.SensitiveHeaders) == 0 {
		cfg.SensitiveHeaders = []string{"Authorization", "X-API-Key", "X-Signature", "Cookie", "Set-Cookie"}
	}
	for _, h := range cfg.SensitiveHeaders {
		sensitiveHeaderMap[strings.ToLower(h)] = true
	}

	sensitiveBodyFields := cfg.SensitiveBodyFields
	if len(sensitiveBodyFields) == 0 {
		sensitiveBodyFields = defaultSensitiveBodyFields
	}

	// An empty list means "use the defaults"; turning redaction off is spelled
	// DisableQueryRedaction. Relying on nil-versus-empty did not survive a
	// round trip through JSON or YAML, where an omitted field unmarshals to
	// nil -- so deserialising a config silently disabled redaction.
	var sensitiveQueryKeys []string
	if !cfg.DisableQueryRedaction {
		sensitiveQueryKeys = cfg.SensitiveQueryParams
		if len(sensitiveQueryKeys) == 0 {
			sensitiveQueryKeys = defaultSensitiveQueryParams
		}
	}

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip if path is in skip list
			if skipPathMap[r.URL.Path] {
				next.ServeHTTP(w, r)
				return
			}

			// Skip if skip function returns true
			if cfg.SkipFunc != nil && cfg.SkipFunc(r) {
				next.ServeHTTP(w, r)
				return
			}

			start := time.Now()

			// Handle request ID
			requestID := r.Header.Get(cfg.RequestIDHeader)
			if requestID == "" && cfg.IncludeRequestID {
				if cfg.GenerateRequestID != nil {
					requestID = cfg.GenerateRequestID()
				} else {
					requestID = generateUUID()
				}
				r.Header.Set(cfg.RequestIDHeader, requestID)
			}

			// Set request ID in response header
			if cfg.IncludeRequestID && requestID != "" {
				w.Header().Set(cfg.RequestIDHeader, requestID)
			}

			// Add request ID to context
			if requestID != "" {
				r = r.WithContext(ContextWithRequestID(r.Context(), requestID))
			}

			// Add logger to context
			r = r.WithContext(ContextWithLogger(r.Context(), cfg.Logger))

			var requestBodyForLog []byte
			if cfg.IncludeBody && (r.Method == http.MethodPost || r.Method == http.MethodPut || r.Method == http.MethodPatch) && r.Body != nil {
				// One byte past the limit, so truncation is detectable. Capping
				// the read at exactly MaxBodySize made a complete body of that
				// size indistinguishable from a truncated one.
				requestBodyForLog, _ = io.ReadAll(io.LimitReader(r.Body, int64(cfg.MaxBodySize)+1))
				r.Body = io.NopCloser(io.MultiReader(bytes.NewReader(requestBodyForLog), r.Body))
			}

			// Wrap response writer to capture status
			rw := &responseWriter{ResponseWriter: w, status: http.StatusOK}

			// Process request
			next.ServeHTTP(rw, r)

			// Calculate latency
			latency := time.Since(start)

			// Determine log level based on status code
			var logLevel Level
			switch {
			case rw.status >= 500:
				logLevel = cfg.ErrorLevel
			case rw.status >= 400:
				logLevel = cfg.WarnLevel
			default:
				logLevel = cfg.LogLevel
			}

			// Build log event
			zl := cfg.Logger.Zerolog()
			event := zl.WithLevel(logLevel.ToZerolog())

			// Add standard fields
			event = event.
				Str("method", r.Method).
				Str("path", r.URL.Path).
				Int("status", rw.status).
				Str("ip", getClientIPStd(r, cfg.TrustedProxies)).
				Str("user_agent", r.UserAgent())

			// Add request ID
			if cfg.IncludeRequestID && requestID != "" {
				event = event.Str("request_id", requestID)
			}

			// Add latency
			if cfg.IncludeLatency {
				event = event.Dur("latency", latency)
			}

			// Add query parameters (with sensitive keys redacted)
			if cfg.IncludeQuery && r.URL.RawQuery != "" {
				event = event.Str("query", redactQuery(r.URL.RawQuery, sensitiveQueryKeys))
			}

			// Add headers
			if cfg.IncludeHeaders {
				headers := make(map[string]string)
				for name, values := range r.Header {
					if sensitiveHeaderMap[strings.ToLower(name)] {
						headers[name] = "[REDACTED]"
					} else if len(values) > 0 {
						headers[name] = values[0]
					}
				}
				event = event.Interface("headers", headers)
			}

			// Add request body (net/http: from buffered peek)
			if cfg.IncludeBody && len(requestBodyForLog) > 0 {
				body := requestBodyForLog
				truncated := len(body) > cfg.MaxBodySize
				if truncated {
					body = body[:cfg.MaxBodySize]
				}
				logged := string(body)
				if !cfg.DisableBodyRedaction {
					logged = redactBody(r.Header.Get("Content-Type"), body, sensitiveBodyFields)
				}
				if truncated {
					logged += "...[truncated]"
				}
				event = event.Str("request_body", logged)
			}

			// Add response size
			if rw.size > 0 {
				event = event.Int("size", rw.size)
			}

			// Add custom fields
			if cfg.CustomFields != nil {
				for key, value := range cfg.CustomFields(r) {
					event = event.Interface(key, value)
				}
			}

			// Send log
			event.Msg("HTTP request")
		})
	}
}

// FiberMiddleware creates a Fiber logging middleware.
func FiberMiddleware(cfg MiddlewareConfig) fiber.Handler {
	if cfg.Logger == nil {
		cfg.Logger = defaultLogger
	}
	if cfg.RequestIDHeader == "" {
		cfg.RequestIDHeader = "X-Request-ID"
	}
	if cfg.MaxBodySize <= 0 {
		cfg.MaxBodySize = 1024
	}
	// Apply default log levels if not set (zero value is DebugLevel)
	defaults := DefaultMiddlewareConfig()
	if cfg.LogLevel == DebugLevel && cfg.WarnLevel == DebugLevel && cfg.ErrorLevel == DebugLevel {
		cfg.LogLevel = defaults.LogLevel
		cfg.WarnLevel = defaults.WarnLevel
		cfg.ErrorLevel = defaults.ErrorLevel
	}

	skipPathMap := make(map[string]bool)
	for _, p := range cfg.SkipPaths {
		skipPathMap[p] = true
	}

	sensitiveHeaderMap := make(map[string]bool)
	if len(cfg.SensitiveHeaders) == 0 {
		cfg.SensitiveHeaders = []string{"Authorization", "X-API-Key", "X-Signature", "Cookie", "Set-Cookie"}
	}
	for _, h := range cfg.SensitiveHeaders {
		sensitiveHeaderMap[strings.ToLower(h)] = true
	}

	sensitiveBodyFieldsFiber := cfg.SensitiveBodyFields
	if len(sensitiveBodyFieldsFiber) == 0 {
		sensitiveBodyFieldsFiber = defaultSensitiveBodyFields
	}

	// An empty list means "use the defaults"; turning redaction off is spelled
	// DisableQueryRedaction. Relying on nil-versus-empty did not survive a
	// round trip through JSON or YAML, where an omitted field unmarshals to
	// nil -- so deserialising a config silently disabled redaction.
	var sensitiveQueryKeysFiber []string
	if !cfg.DisableQueryRedaction {
		sensitiveQueryKeysFiber = cfg.SensitiveQueryParams
		if len(sensitiveQueryKeysFiber) == 0 {
			sensitiveQueryKeysFiber = defaultSensitiveQueryParams
		}
	}

	return func(c fiber.Ctx) error {
		// Skip if path is in skip list
		if skipPathMap[c.Path()] {
			return c.Next()
		}

		// Skip if skip function returns true
		if cfg.SkipFuncFiber != nil && cfg.SkipFuncFiber(c) {
			return c.Next()
		}

		start := time.Now()

		// Handle request ID
		requestID := c.Get(cfg.RequestIDHeader)
		if requestID == "" && cfg.IncludeRequestID {
			if cfg.GenerateRequestID != nil {
				requestID = cfg.GenerateRequestID()
			} else {
				requestID = generateUUID()
			}
			c.Request().Header.Set(cfg.RequestIDHeader, requestID)
		}

		// Set request ID in response header
		if cfg.IncludeRequestID && requestID != "" {
			c.Set(cfg.RequestIDHeader, requestID)
		}

		// Store request ID in locals
		if requestID != "" {
			c.Locals(fiberRequestIDKey, requestID)
		}

		// Store logger in locals
		c.Locals(fiberLoggerKey, cfg.Logger)

		// Process request
		err := c.Next()

		// Calculate latency
		latency := time.Since(start)

		// Get status code
		status := c.Response().StatusCode()

		// Determine log level based on status code
		var logLevel Level
		switch {
		case status >= 500:
			logLevel = cfg.ErrorLevel
		case status >= 400:
			logLevel = cfg.WarnLevel
		default:
			logLevel = cfg.LogLevel
		}

		// Build log event
		zl := cfg.Logger.Zerolog()
		event := zl.WithLevel(logLevel.ToZerolog())

		// Add standard fields
		event = event.
			Str("method", c.Method()).
			Str("path", c.Path()).
			Int("status", status).
			Str("ip", getClientIPFiber(c, cfg.TrustedProxies)).
			Str("user_agent", c.Get("User-Agent"))

		// Add request ID
		if cfg.IncludeRequestID && requestID != "" {
			event = event.Str("request_id", requestID)
		}

		// Add latency
		if cfg.IncludeLatency {
			event = event.Dur("latency", latency)
		}

		// Add query parameters (with sensitive keys redacted)
		if cfg.IncludeQuery {
			query := c.Request().URI().QueryString()
			if len(query) > 0 {
				event = event.Str("query", redactQuery(string(query), sensitiveQueryKeysFiber))
			}
		}

		// Add headers
		if cfg.IncludeHeaders {
			headers := make(map[string]string)
			for key, value := range c.Request().Header.All() {
				headerName := string(key)
				if sensitiveHeaderMap[strings.ToLower(headerName)] {
					headers[headerName] = "[REDACTED]"
				} else {
					headers[headerName] = string(value)
				}
			}
			event = event.Interface("headers", headers)
		}

		// Add request body if enabled
		if cfg.IncludeBody {
			body := c.Body()
			truncated := false
			if len(body) > cfg.MaxBodySize {
				body = body[:cfg.MaxBodySize]
				truncated = true
			}
			if len(body) > 0 {
				logged := string(body)
				if !cfg.DisableBodyRedaction {
					logged = redactBody(c.Get("Content-Type"), body, sensitiveBodyFieldsFiber)
				}
				if truncated {
					logged += "...[truncated]"
				}
				event = event.Str("request_body", logged)
			}
		}

		// Add custom fields
		if cfg.CustomFieldsFiber != nil {
			for key, value := range cfg.CustomFieldsFiber(c) {
				event = event.Interface(key, value)
			}
		}

		// Add error if present
		if err != nil {
			event = event.Err(err)
		}

		// Send log
		event.Msg("HTTP request")

		return err
	}
}

// LoggerFromFiberCtx extracts the logger from Fiber context.
func LoggerFromFiberCtx(c fiber.Ctx) *Logger {
	if l, ok := c.Locals(fiberLoggerKey).(*Logger); ok {
		return l
	}
	return defaultLogger
}

// RequestIDFromFiberCtx extracts the request ID from Fiber context.
func RequestIDFromFiberCtx(c fiber.Ctx) string {
	if id, ok := c.Locals(fiberRequestIDKey).(string); ok {
		return id
	}
	return ""
}

// CtxFiber returns a zerolog.Logger enriched with Fiber context values.
func CtxFiber(c fiber.Ctx) *zerolog.Logger {
	l := LoggerFromFiberCtx(c)
	logger := l.Zerolog()

	if requestID := RequestIDFromFiberCtx(c); requestID != "" {
		logger = logger.With().Str("request_id", requestID).Logger()
	}

	return &logger
}

// generateUUID generates a UUID v4 using crypto/rand (via google/uuid).
func generateUUID() string {
	return uuid.New().String()
}
