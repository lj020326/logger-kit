package logger

import (
	"encoding/json"
	"net/url"
	"strings"
)

// defaultSensitiveBodyFields are field names redacted inside a logged request
// body. They match the query-parameter list: a login form posts the same
// "password" whether it arrives in the query string or the body.
var defaultSensitiveBodyFields = []string{
	"password", "passwd", "pwd", "old_password", "new_password",
	"token", "code", "secret", "key", "api_key", "apikey",
	"access_token", "refresh_token", "id_token", "session", "session_id",
	"authorization", "credential", "credentials", "private_key", "client_secret",
	"otp", "pin", "cvv", "card_number",
}

// redactBody removes sensitive values from a request body before it is logged.
//
// Query parameters and headers were already redacted while the body was
// written out verbatim -- and a JSON or form login request carries its password
// in exactly that body. The surrounding redaction made it easy to assume the
// body was covered too.
//
// JSON objects and form-encoded bodies are redacted field by field. A body in
// any other format cannot be redacted field-wise, so it is replaced wholesale
// rather than logged raw.
func redactBody(contentType string, body []byte, fields []string) string {
	if len(body) == 0 {
		return ""
	}

	keys := make(map[string]bool, len(fields))
	for _, f := range fields {
		keys[strings.ToLower(strings.TrimSpace(f))] = true
	}

	mediaType := strings.ToLower(strings.TrimSpace(strings.SplitN(contentType, ";", 2)[0]))

	switch {
	case strings.HasSuffix(mediaType, "json"):
		if out, ok := redactJSON(body, keys); ok {
			return out
		}
		return "[UNPARSEABLE JSON BODY REDACTED]"

	case mediaType == "application/x-www-form-urlencoded":
		return redactForm(string(body), keys)

	case strings.HasPrefix(mediaType, "text/"):
		// Plain text carries no field structure to redact selectively.
		return "[" + mediaType + " BODY REDACTED]"

	default:
		return "[" + mediaType + " BODY REDACTED]"
	}
}

// redactJSON rewrites a JSON document with sensitive values replaced.
func redactJSON(body []byte, keys map[string]bool) (string, bool) {
	var parsed interface{}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", false
	}
	out, err := json.Marshal(redactValue(parsed, keys))
	if err != nil {
		return "", false
	}
	return string(out), true
}

// redactValue walks a decoded JSON value, replacing values under sensitive
// keys at any depth.
func redactValue(v interface{}, keys map[string]bool) interface{} {
	switch typed := v.(type) {
	case map[string]interface{}:
		out := make(map[string]interface{}, len(typed))
		for k, val := range typed {
			if keys[strings.ToLower(k)] {
				out[k] = "***"
				continue
			}
			out[k] = redactValue(val, keys)
		}
		return out
	case []interface{}:
		out := make([]interface{}, len(typed))
		for i, val := range typed {
			out[i] = redactValue(val, keys)
		}
		return out
	default:
		return v
	}
}

// redactForm rewrites a form-encoded body with sensitive values replaced.
func redactForm(body string, keys map[string]bool) string {
	vals, err := url.ParseQuery(body)
	if err != nil {
		return "[UNPARSEABLE FORM BODY REDACTED]"
	}

	var b strings.Builder
	first := true
	for k, list := range vals {
		for _, v := range list {
			if !first {
				b.WriteByte('&')
			}
			first = false
			b.WriteString(url.QueryEscape(k))
			b.WriteByte('=')
			if keys[strings.ToLower(k)] {
				b.WriteString("***")
			} else {
				b.WriteString(url.QueryEscape(v))
			}
		}
	}
	return b.String()
}
