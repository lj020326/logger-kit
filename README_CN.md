# Logger Kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/logger-kit/v2.svg)](https://pkg.go.dev/github.com/soulteary/logger-kit/v2)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/logger-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/logger-kit)

[English](README.md)

基于 [zerolog](https://github.com/rs/zerolog) 的 Go 应用结构化日志工具包。提供动态日志级别管理、基于上下文的日志记录，以及用于运行时日志级别调整的 HTTP 端点。

## 功能特性

- **zerolog 封装**：支持 JSON 和控制台输出格式的结构化日志
- **动态日志级别**：线程安全的日志级别管理，支持运行时调整
- **HTTP 端点**：RESTful API 用于在运行时获取/设置日志级别
- **上下文日志**：请求 ID、追踪 ID、Span ID 和用户 ID 的传播
- **中间件**：支持 net/http 和 Fiber 的请求日志中间件
- **格式选项**：JSON 和人类可读的控制台输出

## 安全说明

- **Level 端点**：生产环境中必须设置 `AllowedIPs` 或 `RequireAuth`，且不要将端点暴露到公网。若部署在反向代理后，请设置 `TrustedProxies` 为代理 IP。
- **Query/Body 日志**：默认会记录 URL 查询参数；可通过 `SensitiveQueryParams`（默认会脱敏 password、token 等常见参数）避免泄露敏感信息，`DisableQueryRedaction` 可关闭脱敏。开启请求体记录时，`SensitiveBodyFields` / `DisableBodyRedaction` 提供同样的控制。敏感接口请勿开启 `IncludeBody`。
- 详见 [SECURITY.md](SECURITY.md) 及漏洞报告方式。

## 环境要求

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）
- `github.com/rs/zerolog`
- Fiber 中间件与处理器需要 `github.com/gofiber/fiber/v3` v3.4.0+

v2 模块线面向 Fiber v3。仍在 Fiber v2 上的应用请继续使用
`github.com/soulteary/logger-kit` v1。

## 安装

```bash
go get github.com/soulteary/logger-kit/v2
```

v2 的所有 Fiber 专用 API 均基于 Fiber v3。仍使用 Fiber v2 的应用应继续使用 logger-kit v1；net/http API 的行为保持不变。

## 快速开始

### 基本用法

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // 使用默认日志器
    logger.Info().Msg("应用程序已启动")
    
    // 带字段
    logger.Info().
        Str("user", "john").
        Int("attempt", 1).
        Msg("登录尝试")
    
    // 带错误
    logger.Error().
        Err(err).
        Str("operation", "database_query").
        Msg("查询失败")
}
```

### 自定义日志器

```go
package main

import (
    "os"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // 创建自定义日志器
    log := logger.New(logger.Config{
        Level:          logger.DebugLevel,
        Output:         os.Stdout,
        Format:         logger.FormatJSON,
        ServiceName:    "my-service",
        ServiceVersion: "1.0.0",
        CallerEnabled:  true,
    })
    
    log.Info().Msg("自定义日志器已就绪")
}
```

### 动态日志级别

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // 获取当前级别
    currentLevel := log.GetLevel()
    
    // 在运行时更改级别
    log.SetLevel(logger.DebugLevel)
    
    // 注册级别更改回调
    unregister := log.LevelManager().OnChange(func(old, new logger.Level) {
        fmt.Printf("日志级别从 %s 更改为 %s\n", old, new)
    })
    defer unregister()
}
```

### 日志级别管理 HTTP 端点

```go
package main

import (
    "net/http"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // 注册日志级别端点
    mux := http.NewServeMux()
    logger.RegisterLevelEndpoint(mux, "/log/level", logger.LevelHandlerConfig{
        Logger: log,
        AllowedIPs: []string{"127.0.0.1"},
    })
    
    http.ListenAndServe(":8080", mux)
}

// GET /log/level - 获取当前日志级别
// PUT 或 POST /log/level - 设置日志级别（请求体：{"level": "debug"} 或查询参数：?level=debug）
```

**安全（Level 端点）：** 生产环境必须设置 `AllowedIPs` 或 `RequireAuth`，仅允许受信任的调用方修改日志级别；不要将端点暴露到公网。若在反向代理后，请设置 `TrustedProxies` 为代理 IP，以便正确识别客户端 IP。详见 [SECURITY.md](SECURITY.md)。

### 请求日志中间件

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
        IncludeHeaders:   false, // 设置为 true 以记录请求头
    })
    
    handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // 从上下文访问日志器
        l := logger.LoggerFromRequest(r)
        l.Info().Msg("处理请求")
        
        // 访问请求 ID
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
        // 从 Fiber 上下文访问日志器
        l := logger.LoggerFromFiberCtx(c)
        l.Info().Msg("处理请求")
        
        return c.SendString("OK")
    })
    
    // 注册日志级别端点
    logger.RegisterLevelEndpointFiber(app, "/log/level", logger.LevelHandlerConfig{
        Logger: log,
    })
    
    app.Listen(":3000")
}
```

### 基于上下文的日志记录

```go
package main

import (
    "context"
    
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    log := logger.NewDefault()
    
    // 创建带有日志器和 ID 的上下文
    ctx := context.Background()
    ctx = logger.ContextWithLogger(ctx, log)
    ctx = logger.ContextWithRequestID(ctx, "req-123")
    ctx = logger.ContextWithTraceID(ctx, "trace-456")
    ctx = logger.ContextWithUserID(ctx, "user-001")
    
    // 使用上下文记录日志 - 自动包含所有 ID
    l := logger.Ctx(ctx)
    l.Info().Msg("处理请求")
    // 输出：{"level":"info","request_id":"req-123","trace_id":"trace-456","user_id":"user-001","message":"处理请求"}
}
```

### 控制台格式（人类可读）

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
        Format: logger.FormatConsole, // 人类可读格式
    })
    
    log.Info().Str("user", "john").Msg("用户已登录")
    // 输出：3:04PM INF 用户已登录 user=john
}
```

### 从环境变量解析日志级别

```go
package main

import (
    "github.com/soulteary/logger-kit/v2"
)

func main() {
    // 从 LOG_LEVEL 环境变量读取日志级别
    // 如果未设置或无效，则回退到 InfoLevel
    level := logger.ParseLevelFromEnv("LOG_LEVEL", logger.InfoLevel)
    
    log := logger.New(logger.Config{
        Level: level,
    })
    
    log.Info().Msg("日志器已初始化")
}
```

## API 参考

### 日志级别

| 级别 | 描述 |
|------|------|
| `TraceLevel` | 最详细的级别 |
| `DebugLevel` | 调试消息 |
| `InfoLevel` | 信息性消息（默认） |
| `WarnLevel` | 警告消息 |
| `ErrorLevel` | 错误消息 |
| `FatalLevel` | 致命消息（调用 os.Exit(1)） |
| `PanicLevel` | 恐慌消息（触发 panic） |
| `Disabled` | 禁用日志 |

### 配置选项

```go
type Config struct {
    Level                Level     // 最低日志级别（默认：InfoLevel）
    Output               io.Writer // 日志输出（默认：os.Stderr）
    Format               Format    // 输出格式：FormatJSON 或 FormatConsole
    TimeFormat           string    // 时间戳格式
    CallerEnabled        bool      // 包含调用者信息
    CallerSkipFrameCount int       // 调用者跳过帧数
    StackTraceEnabled    bool      // 错误日志是否输出堆栈
    ServiceName          string    // 服务名称字段
    ServiceVersion       string    // 版本字段
}
```

### 中间件配置

```go
type MiddlewareConfig struct {
    Logger                *Logger       // 日志器实例（nil=使用默认）
    SkipPaths             []string      // 跳过日志记录的路径
    SkipFunc              func(*http.Request) bool // net/http 跳过条件
    SkipFuncFiber         func(fiber.Ctx) bool  // Fiber 跳过条件
    LogLevel              Level         // 2xx 响应的日志级别
    WarnLevel             Level         // 4xx 响应的日志级别
    ErrorLevel            Level         // 5xx 响应的日志级别
    IncludeRequestID      bool          // 生成/传播请求 ID
    RequestIDHeader       string        // 请求 ID 的头名称
    GenerateRequestID     func() string // 自定义 ID 生成（nil=UUID）
    IncludeLatency        bool          // 记录请求持续时间
    IncludeHeaders        bool          // 记录请求头
    SensitiveHeaders      []string      // 需要脱敏的头
    IncludeQuery          bool          // 记录查询参数
    SensitiveQueryParams  []string      // 需脱敏的 query 键（为空=使用默认列表）
    DisableQueryRedaction bool          // 原样记录 query 字符串
    IncludeBody           bool          // 记录请求体（慎用）
    SensitiveBodyFields   []string      // 需脱敏的请求体字段名（为空=使用默认列表）
    DisableBodyRedaction  bool          // 原样记录请求体
    MaxBodySize           int           // 记录的最大请求体大小
    CustomFields          func(*http.Request) map[string]interface{}   // 自定义字段（net/http）
    CustomFieldsFiber     func(fiber.Ctx) map[string]interface{}   // 自定义字段（Fiber）
    TrustedProxies        []string      // 代理 IP/CIDR，用于从 X-Forwarded-For 解析客户端 IP
}
```

**敏感数据**默认会被脱敏，详见下文的[脱敏](#脱敏)一节。

### 脱敏

一个请求里有三处可能携带凭证，每一处都有自己的开关。

| 来源 | 默认是否记录 | 脱敏名单 | 关闭方式 |
|------|-------------|----------|----------|
| 请求头 | `IncludeHeaders`（false） | `SensitiveHeaders` | 把该头从名单中移除 |
| 查询字符串 | `IncludeQuery`（**true**） | `SensitiveQueryParams` | `DisableQueryRedaction` |
| 请求体 | `IncludeBody`（false） | `SensitiveBodyFields` | `DisableBodyRedaction` |

两个名单都遵循同一规则：**切片为空或未设置表示"使用默认名单"**——默认覆盖
`password`、`token`、`code`、`secret`、`api_key` 等。需要原样记录请设置对应的
`Disable…` 开关。

```go
config := logger.DefaultMiddlewareConfig()
config.IncludeBody = true
config.SensitiveBodyFields = []string{"password", "otp", "card_number"}

// 或者对确定不含凭证的路由原样记录
config.DisableBodyRedaction = true
```

**请求体**按结构脱敏：

- **JSON 对象**会逐字段、任意深度地重写，所以日志行仍是合法 JSON，非敏感字段也得以
  保留。数字按原样保留——超过 2^53 的整数不会因为经过 `float64` 而被舍入。
- **表单编码**的请求体以同样方式重写。
- **其它格式**没有可供选择性脱敏的字段结构，会被整体替换，而不是原样记录。

超过 `MaxBodySize` 的请求体会被截断并标记 `...[truncated]`。恰好等于 `MaxBodySize`
的请求体不会被标记——探读会多读一个字节，这样"完整"和"被截断"才能区分开。

无法解析的查询字符串会被整体脱敏，而不是原样记录。

控制台格式下，默认的字段值格式化函数使用 `%v`；请避免记录敏感字段（见
`logger.SensitiveFieldNames`），或者设置自定义的 `FormatFieldValue` 来遮蔽它们。

### 日志级别端点配置

```go
type LevelHandlerConfig struct {
    Logger          *Logger  // 要控制的日志器（nil=默认）
    AllowedIPs      []string // IP 白名单（空=允许所有）
    TrustedProxies  []string // 代理 IP/CIDR，用于解析 X-Forwarded-For
    RequireAuth     bool     // 是否要求 AuthFunc/AuthFuncFiber
    AuthFunc        func(*http.Request) bool  // net/http 鉴权
    AuthFuncFiber   func(fiber.Ctx) bool    // Fiber 鉴权
    MaxBodyBytes    int64    // PUT/POST 最大 body（默认 4096）
}
```

### 上下文与请求辅助函数

```go
// 传递 logger
ctx = logger.ContextWithLogger(ctx, l)
l = logger.LoggerFromContext(ctx)
l, ok := logger.LoggerFromContextOK(ctx)
r = logger.SetLoggerInRequest(r, l)
l = logger.LoggerFromRequest(r)
l = logger.LoggerFromFiberCtx(c)

// 传递关联 ID
ctx = logger.ContextWithRequestID(ctx, id)
ctx = logger.ContextWithTraceID(ctx, traceID)
ctx = logger.ContextWithSpanID(ctx, spanID)
ctx = logger.ContextWithUserID(ctx, userID)
ctx = logger.ContextWithIDs(ctx, requestID, traceID, spanID) // 三个一次设置

id = logger.RequestIDFromContext(ctx)
id = logger.RequestIDFromRequest(r)
id = logger.RequestIDFromFiberCtx(c)
traceID = logger.TraceIDFromContext(ctx)
traceID = logger.TraceIDFromRequest(r)
spanID = logger.SpanIDFromContext(ctx)
userID = logger.UserIDFromContext(ctx)
userID = logger.UserIDFromRequest(r)

r = logger.SetRequestIDInRequest(r, id)
r = logger.SetTraceIDInRequest(r, traceID)
r = logger.SetUserIDInRequest(r, userID)

// 已经带上 context 中各 ID 的 zerolog logger
zl := logger.Ctx(ctx)
zl = logger.LogFromContext(ctx)
zl = logger.CtxFiber(c)
```

### 包级 logger

```go
logger.SetDefault(l)
l := logger.Default()

logger.Trace().Msg("…")
logger.Debug().Msg("…")
logger.Info().Msg("…")
logger.Warn().Msg("…")
logger.Error().Err(err).Msg("…")
logger.Fatal().Msg("…")   // 会退出进程
logger.Panic().Msg("…")   // 会 panic
```

### 级别与格式

```go
lvl, err := logger.ParseLevel("debug")
lvl = logger.MustParseLevel("debug")        // 取值非法时 panic
lvl = logger.FromZerolog(zerolog.DebugLevel)
logger.AllLevels()                          // 所有 Level
logger.ValidLevelStrings()                  // 它们的字符串写法

logger.SetGlobalLevel(lvl)                  // 进程级下限
lvl = logger.GetGlobalLevel()
logger.SetDefaultLevel(lvl)                 // 新建 logger 的默认级别
lvl = logger.GetDefaultLevel()

mgr := logger.NewLevelManager(logger.InfoLevel) // 运行时可切换的级别
mgr = logger.GlobalLevelManager

format := logger.ParseFormat("console")     // 或 "json"
```

### Writer

```go
// 扇出到多个 writer
w := logger.MultiWriter(os.Stdout, fileWriter)

// 扇出，但每条记录只发给级别允许它的 writer
w = logger.FilteredMultiWriter(
    logger.LevelWriter{Writer: os.Stdout, Level: logger.InfoLevel},
    logger.LevelWriter{Writer: errFile, Level: logger.ErrorLevel},
)

// 人类可读的控制台输出
cw := logger.NewConsoleWriter(logger.DefaultConsoleWriterConfig())
```

`logger.TimeFormatPresets` 提供了现成的时间戳布局，`logger.DefaultFieldNames()`
返回 `FieldNames` 结构体，可用于重命名 `level`、`message`、`time`、`caller`、
`error` 和 `stack`。

## 升级说明（v2.2.0）

新增三个字段，没有删除任何东西。其中两处改变会影响日志里最终写下的内容。

- **请求体默认会被脱敏。** `IncludeBody` 此前把请求体原样写进日志。查询参数有
  `redactQuery`、请求头有 `SensitiveHeaders`，而请求体——JSON 或表单登录请求真正携带
  密码的地方——什么都没有，而周围那一圈脱敏机制很容易让人以为它也被覆盖了。现在 JSON
  和表单请求体会逐字段重写，其它格式整体替换。对确定不含凭证的路由可设置
  `DisableBodyRedaction`。
- **关闭 query 脱敏要用 `DisableQueryRedaction`，而不是传 `nil` 切片。** 旧写法依赖
  `nil` 与空切片的区别——而这个区别无法在 JSON 或 YAML 往返后保留，字段缺失时反序列化
  即为 `nil`。**于是加载一份配置就会在没人要求的情况下关掉 query 脱敏。** 现在
  `SensitiveQueryParams` 为空或未设置都表示"使用默认名单"。如果你是刻意传 `nil`，
  请改为设置 `DisableQueryRedaction: true`。
- **大整数在脱敏后不会被破坏。** 请求体此前被解码进一个裸 `interface{}`，每个 JSON
  数字都变成 `float64`，于是重新序列化会改写任何超过 2^53 的整数：日志里本该是
  `9007199254740993` 的订单号变成了 `9007199254740992`。现在数字 token 按原样保留。
- **恰好等于 `MaxBodySize` 的请求体不再被标记为截断。** 探读此前正好截到这个字节数，
  于是一个完整的该尺寸请求体和一个被截断的看起来完全一样。现在会多读一个字节，以便
  检测是否真的超出。
- **新增 `SensitiveBodyFields` 和 `DisableBodyRedaction`**，以及
  `DisableQueryRedaction`。

## 测试

```bash
go test -v -race -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

## 许可证

Apache License 2.0
