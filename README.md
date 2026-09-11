# Logger Kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/logger-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/logger-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/logger-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/logger-kit)

[中文文档](README_CN.md)

A structured logging toolkit for Go applications based on [zerolog](https://github.com/rs/zerolog). Provides dynamic log level management, context-based logging, and HTTP endpoints for runtime log level adjustment.

## Features

- **zerolog Wrapper**: Structured logging with JSON and console output formats
- **Dynamic Log Level**: Thread-safe log level management with runtime adjustment
- **HTTP Endpoints**: RESTful API to get/set log levels at runtime
- **Context Logging**: Request ID, trace ID, span ID, and user ID propagation
- **Middleware**: Request logging middleware for both net/http and Fiber
- **Format Options**: JSON and human-readable console output

## Security

- **Level endpoint**: In production, always set `AllowedIPs` or `RequireAuth`; do not expose the endpoint publicly. When behind a reverse proxy, set `TrustedProxies` to your proxy IPs. If `RequireAuth` is true, you must supply `AuthFunc`/`AuthFuncFiber` or requests will be rejected.
- **Query/body logging**: Query parameters are logged by default; `SensitiveQueryParams` (default list redacts common keys like `password`, `token`) avoids leaking secrets, and `DisableQueryRedaction` turns it off. Logged request bodies get the same treatment through `SensitiveBodyFields` / `DisableBodyRedaction`. Avoid enabling `IncludeBody` on sensitive routes. Unparseable query strings are fully redacted.
- See [SECURITY.md](SECURITY.md) for details and how to report vulnerabilities.

## Installation

```bash
go get github.com/soulteary/logger-kit/v2
```

Version 2 uses Fiber v3 for all Fiber-specific APIs. Applications that still use Fiber v2 should remain on logger-kit v1. The net/http APIs keep the same behavior.

## Quick Start

### Basic Usage

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // Use default logger
    logger.Info().Msg("Application started")
    
    // With fields
    logger.Info().
        Str("user", "john").
        Int("attempt", 1).
        Msg("Login attempt")
    
    // With error
    logger.Error().
        Err(err).
        Str("operation", "database_query").
        Msg("Query failed")
}
```

### Custom Logger

```go
package main

import (
    "os"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // Create custom logger
    log := logger.New(logger.Config{
        Level:          logger.DebugLevel,
        Output:         os.Stdout,
        Format:         logger.FormatJSON,
        ServiceName:    "my-service",
        ServiceVersion: "1.0.0",
        CallerEnabled:  true,
    })
    
    log.Info().Msg("Custom logger ready")
}
```

### Dynamic Log Level

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // Get current level
    currentLevel := log.GetLevel()
    
    // Change level at runtime
    log.SetLevel(logger.DebugLevel)
    
    // Register callback for level changes
    unregister := log.LevelManager().OnChange(func(old, new logger.Level) {
        fmt.Printf("Log level changed from %s to %s\n", old, new)
    })
    defer unregister()
}
```

### HTTP Endpoint for Log Level Management

```go
package main

import (
    "net/http"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // Register log level endpoint
    mux := http.NewServeMux()
    logger.RegisterLevelEndpoint(mux, "/log/level", logger.LevelHandlerConfig{
        Logger: log,
        AllowedIPs: []string{"127.0.0.1"},
    })
    
    http.ListenAndServe(":8080", mux)
}

// GET /log/level - Get current log level
// PUT or POST /log/level - Set log level (body: {"level": "debug"} or query: ?level=debug)
```

**Security (Level endpoint):** In production you must set `AllowedIPs` or `RequireAuth` so only trusted callers can change the log level. Do not expose this endpoint to the public. When behind a reverse proxy, set `TrustedProxies` to your proxy IPs so client IP checks work correctly. If you enable `RequireAuth`, you must provide an `AuthFunc`/`AuthFuncFiber`. See [SECURITY.md](SECURITY.md) for details.

### Request Logging Middleware

#### net/http

```go
package main

import (
    "net/http"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    middleware := logger.Middleware(logger.MiddlewareConfig{
        Logger:           log,
        SkipPaths:        []string{"/health", "/metrics"},
        IncludeRequestID: true,
        IncludeLatency:   true,
        IncludeHeaders:   false, // Set to true to log headers
    })
    
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // Access logger from context
        l := logger.LoggerFromRequest(r)
        l.Info().Msg("Processing request")
        
        // Access request ID
        requestID := logger.RequestIDFromRequest(r)
        
        w.Write([]byte("OK"))
    })
    
    http.ListenAndServe(":8080", middleware(handler))
}
```

#### Fiber

```go
package main

import (
    "github.com/gofiber/fiber/v3"
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    app := fiber.New()
    
    app.Use(logger.FiberMiddleware(logger.MiddlewareConfig{
        Logger:           log,
        SkipPaths:        []string{"/health"},
        IncludeRequestID: true,
    }))
    
    app.Get("/", func(c fiber.Ctx) error {
        // Access logger from Fiber context
        l := logger.LoggerFromFiberCtx(c)
        l.Info().Msg("Processing request")
        
        return c.SendString("OK")
    })
    
    // Register log level endpoint
    logger.RegisterLevelEndpointFiber(app, "/log/level", logger.LevelHandlerConfig{
        Logger: log,
    })
    
    app.Listen(":3000")
}
```

### Context-Based Logging

```go
package main

import (
    "context"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // Create context with logger and IDs
    ctx := context.Background()
    ctx = logger.ContextWithLogger(ctx, log)
    ctx = logger.ContextWithRequestID(ctx, "req-123")
    ctx = logger.ContextWithTraceID(ctx, "trace-456")
    ctx = logger.ContextWithUserID(ctx, "user-001")
    
    // Log with context - automatically includes all IDs
    l := logger.Ctx(ctx)
    l.Info().Msg("Processing request")
    // Output: {"level":"info","request_id":"req-123","trace_id":"trace-456","user_id":"user-001","message":"Processing request"}
}
```

### Console Format (Human-Readable)

```go
package main

import (
    "os"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.New(logger.Config{
        Level:  logger.InfoLevel,
        Output: os.Stdout,
        Format: logger.FormatConsole, // Human-readable format
    })
    
    log.Info().Str("user", "john").Msg("User logged in")
    // Output: 3:04PM INF User logged in user=john
}
```

### Parse Log Level from Environment

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // Read log level from LOG_LEVEL environment variable
    // Falls back to InfoLevel if not set or invalid
    level := logger.ParseLevelFromEnv("LOG_LEVEL", logger.InfoLevel)
    
    log := logger.New(logger.Config{
        Level: level,
    })
    
    log.Info().Msg("Logger initialized")
}
```

## API Reference

### Log Levels

| Level | Description |
|-------|-------------|
| `TraceLevel` | Most verbose level |
| `DebugLevel` | Debug messages |
| `InfoLevel` | Informational messages (default) |
| `WarnLevel` | Warning messages |
| `ErrorLevel` | Error messages |
| `FatalLevel` | Fatal messages (calls os.Exit(1)) |
| `PanicLevel` | Panic messages (panics) |
| `Disabled` | Disable logging |

### Configuration Options

```go
type Config struct {
    Level                Level     // Minimum log level (default: InfoLevel)
    Output               io.Writer // Log output (default: os.Stderr)
    Format               Format    // Output format: FormatJSON or FormatConsole
    TimeFormat           string    // Timestamp format
    CallerEnabled        bool      // Include caller information
    CallerSkipFrameCount int       // Skip frames for caller
    StackTraceEnabled    bool      // Stack trace for error logs
    ServiceName          string    // Service name field
    ServiceVersion       string    // Version field
}
```

### Middleware Configuration

```go
type MiddlewareConfig struct {
    Logger                *Logger       // Logger instance (nil = default)
    SkipPaths             []string      // Paths to skip logging
    SkipFunc              func(*http.Request) bool // Skip for net/http
    SkipFuncFiber         func(fiber.Ctx) bool   // Skip for Fiber
    LogLevel              Level         // Level for 2xx responses
    WarnLevel             Level         // Level for 4xx responses
    ErrorLevel            Level         // Level for 5xx responses
    IncludeRequestID      bool          // Generate/propagate request ID
    RequestIDHeader       string        // Header name for request ID
    GenerateRequestID     func() string // Custom ID generator (nil = UUID)
    IncludeLatency        bool          // Log request duration
    IncludeHeaders        bool          // Log request headers
    SensitiveHeaders      []string      // Headers to redact
    IncludeQuery          bool          // Log query parameters
    SensitiveQueryParams  []string      // Query keys to redact (empty = use default list)
    DisableQueryRedaction bool          // Log query strings verbatim
    IncludeBody           bool          // Log request body (use with caution)
    SensitiveBodyFields   []string      // Body field names to redact (empty = use default list)
    DisableBodyRedaction  bool          // Log request bodies verbatim
    MaxBodySize           int           // Max body size to log
    CustomFields          func(*http.Request) map[string]interface{}     // Extra fields (net/http)
    CustomFieldsFiber     func(fiber.Ctx) map[string]interface{}        // Extra fields (Fiber)
    TrustedProxies        []string      // Proxy IPs/CIDRs for client IP from X-Forwarded-For
}
```

**Sensitive data:** `IncludeQuery` is true by default; query strings often contain tokens or passwords. Use `SensitiveQueryParams` (default list includes `password`, `token`, `code`, `secret`, `api_key`, etc.) to redact those values in logs; leave it empty to get that default list, and set `DisableQueryRedaction` to turn redaction off. Redaction used to be disabled by passing `nil` instead of an empty slice — a distinction that does not survive a round trip through JSON or YAML, where an omitted field unmarshals to `nil`, so deserialising a config silently disabled it. Unparseable query strings are fully redacted. `IncludeBody` is false by default; enabling it logs bodies with the same treatment — `SensitiveBodyFields` (empty = default list) redacts fields inside JSON and form-encoded bodies, a body in any other format is replaced wholesale, and `DisableBodyRedaction` logs bodies verbatim. For console format, the default field value formatter uses `%v`; avoid logging sensitive fields (see `logger.SensitiveFieldNames`) or set a custom `FormatFieldValue` to mask them.

### Level Endpoint Configuration

```go
type LevelHandlerConfig struct {
    Logger          *Logger  // Logger to control (nil = default)
    AllowedIPs      []string // IP allowlist (empty = allow all)
    TrustedProxies  []string // Proxy IPs/CIDRs for X-Forwarded-For
    RequireAuth     bool     // Require AuthFunc/AuthFuncFiber
    AuthFunc        func(*http.Request) bool  // Auth for net/http
    AuthFuncFiber   func(fiber.Ctx) bool     // Auth for Fiber
    MaxBodyBytes    int64    // Max body for PUT/POST (default 4096)
}
```

## Testing

```bash
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## License

Apache License 2.0
