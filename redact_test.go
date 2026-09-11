package logger

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRedactBodyJSON covers the case the surrounding redaction made easy to
// miss: query parameters and headers were redacted while the body -- where a
// JSON login request actually carries its password -- was logged verbatim.
func TestRedactBodyJSON(t *testing.T) {
	body := []byte(`{"email":"a@b.com","password":"hunter2","nested":{"api_key":"k-123","keep":"visible"},"list":[{"token":"t-1"}]}`)

	got := redactBody("application/json", body, defaultSensitiveBodyFields)

	for _, secret := range []string{"hunter2", "k-123", "t-1"} {
		if strings.Contains(got, secret) {
			t.Errorf("redacted body still contains %q: %s", secret, got)
		}
	}
	for _, kept := range []string{"a@b.com", "visible"} {
		if !strings.Contains(got, kept) {
			t.Errorf("redacted body lost non-sensitive value %q: %s", kept, got)
		}
	}

	// Still valid JSON, so the log line stays machine-readable.
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(got), &parsed); err != nil {
		t.Fatalf("redacted body is not valid JSON: %v (%s)", err, got)
	}
	if parsed["password"] != "***" {
		t.Errorf("password = %v, want \"***\"", parsed["password"])
	}
}

func TestRedactBodyForm(t *testing.T) {
	got := redactBody("application/x-www-form-urlencoded",
		[]byte("username=alice&password=hunter2&remember=1"), defaultSensitiveBodyFields)

	if strings.Contains(got, "hunter2") {
		t.Errorf("form body still contains the password: %s", got)
	}
	if !strings.Contains(got, "username=alice") || !strings.Contains(got, "remember=1") {
		t.Errorf("form body lost non-sensitive fields: %s", got)
	}
}

// A body with no field structure cannot be redacted selectively, so it must
// not be logged raw either.
func TestRedactBodyUnstructured(t *testing.T) {
	for _, ct := range []string{"application/octet-stream", "text/plain", "multipart/form-data; boundary=x"} {
		got := redactBody(ct, []byte("password=hunter2 raw payload"), defaultSensitiveBodyFields)
		if strings.Contains(got, "hunter2") {
			t.Errorf("content type %q was logged raw: %s", ct, got)
		}
		if !strings.Contains(got, "REDACTED") {
			t.Errorf("content type %q produced %q, want a redaction marker", ct, got)
		}
	}

	if got := redactBody("application/json", []byte("{not json"), defaultSensitiveBodyFields); strings.Contains(got, "not json") {
		t.Errorf("unparseable JSON was logged raw: %s", got)
	}
}

// TestMiddlewareRedactsBodyEndToEnd is the regression test at the middleware
// level.
func TestMiddlewareRedactsBodyEndToEnd(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Output: &buf, Level: InfoLevel, Format: FormatJSON})

	mw := Middleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: 1024,
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"a@b.com","password":"hunter2"}`))
	req.Header.Set("Content-Type", "application/json")
	mw(handler).ServeHTTP(httptest.NewRecorder(), req)

	out := buf.String()
	if strings.Contains(out, "hunter2") {
		t.Errorf("the password reached the log: %s", out)
	}
	if !strings.Contains(out, "a@b.com") {
		t.Errorf("the non-sensitive field was lost: %s", out)
	}

	// Opting out is still possible.
	buf.Reset()
	mwRaw := Middleware(MiddlewareConfig{
		Logger:               logger,
		IncludeBody:          true,
		MaxBodySize:          1024,
		DisableBodyRedaction: true,
	})
	req2 := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"password":"hunter2"}`))
	req2.Header.Set("Content-Type", "application/json")
	mwRaw(handler).ServeHTTP(httptest.NewRecorder(), req2)

	if !strings.Contains(buf.String(), "hunter2") {
		t.Error("DisableBodyRedaction did not take effect")
	}
}

// TestExactSizeBodyIsNotMarkedTruncated: the peek was capped at exactly
// MaxBodySize, so a complete body of that size looked truncated.
func TestExactSizeBodyIsNotMarkedTruncated(t *testing.T) {
	var buf bytes.Buffer
	logger := New(Config{Output: &buf, Level: InfoLevel, Format: FormatJSON})

	payload := `{"a":"bb"}` // 10 bytes
	mw := Middleware(MiddlewareConfig{
		Logger:      logger,
		IncludeBody: true,
		MaxBodySize: len(payload),
	})
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	req := httptest.NewRequest(http.MethodPost, "/t", strings.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	mw(handler).ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(buf.String(), "[truncated]") {
		t.Errorf("a complete body of exactly MaxBodySize was marked truncated: %s", buf.String())
	}
}
