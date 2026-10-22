package web

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/NamTranQuoc/shlink-go/internal/core"
	"github.com/NamTranQuoc/shlink-go/internal/phpx"
)

const (
	problemContentTypeJSON = "application/problem+json"
	problemContentTypeXML  = "application/problem+xml"
	defaultDetailMessage   = "An unknown error occurred."
)

// problemTitles ports ProblemDetailsResponseFactory::DEFAULT_TITLE_MAP.
var problemTitles = map[int]string{
	400: "Bad Request", 401: "Unauthorized", 402: "Payment Required", 403: "Forbidden", 404: "Not Found",
	405: "Method Not Allowed", 406: "Not Acceptable", 407: "Proxy Authentication Required", 408: "Request Timeout",
	409: "Conflict", 410: "Gone", 411: "Length Required", 412: "Precondition Failed", 413: "Payload Too Large",
	414: "Request-URI Too Long", 415: "Unsupported Media Type", 416: "Requested Range Not Satisfiable",
	417: "Expectation Failed", 418: "I'm a teapot", 421: "Misdirected Request", 422: "Unprocessable Entity",
	423: "Locked", 424: "Failed Dependency", 426: "Upgrade Required", 428: "Precondition Required",
	429: "Too Many Requests", 431: "Request Header Fields Too Large", 444: "Connection Closed Without Response",
	451: "Unavailable For Legal Reasons", 499: "Client Closed Request", 500: "Internal Server Error",
	501: "Not Implemented", 502: "Bad Gateway", 503: "Service Unavailable", 504: "Gateway Timeout",
	505: "HTTP Version Not Supported", 506: "Variant Also Negotiates", 507: "Insufficient Storage",
	508: "Loop Detected", 510: "Not Extended", 511: "Network Authentication Required",
	599: "Network Connect Timeout Error",
}

// problemDefaultTypes ports the problem-details default_types_map of config/autoload/error-handler.global.php.
var problemDefaultTypes = map[int]string{
	404: core.ProblemType("not-found"),
	500: core.ProblemType("internal-server-error"),
}

// problemResponse ports ProblemDetailsResponseFactory::createResponse().
func problemResponse(r *Request, status int, detail, title, typ string, additional []core.KV) *Response {
	if status < 400 || status > 599 {
		status = 500
	}
	if title == "" {
		title = problemTitles[status]
		if title == "" {
			title = "Unknown Error"
		}
	}
	if typ == "" {
		typ = problemDefaultTypes[status]
		if typ == "" {
			typ = fmt.Sprintf("https://httpwg.org/specs/rfc9110.html#status.%d", status)
		}
	}
	// array_merge($additional, $payload): additional keys first, the standard ones overwrite in place
	payload := phpx.Map{}
	for _, kv := range additional {
		payload = payload.Set(kv.Key, kv.Value)
	}
	payload = payload.Set("title", title).Set("type", typ).Set("status", status).Set("detail", detail)

	mediaType := problemMediaType(r.Header("Accept"))
	resp := NewResponse(status)
	if mediaType == "" || !strings.Contains(mediaType, "json") {
		resp.Header.Set("Content-Type", problemContentTypeXML)
		resp.Body = []byte(problemXML(payload))
		return resp
	}
	resp.Header.Set("Content-Type", problemContentTypeJSON)
	resp.Body = phpx.Encode(payload, phpx.ProblemDetailsJSONFlags)
	return resp
}

// problemResponseFromError ports ProblemDetailsResponseFactory::createResponseFromThrowable().
func problemResponseFromError(r *Request, err error) *Response {
	var pe *core.ProblemError
	if errors.As(err, &pe) {
		return problemResponse(r, pe.Status, pe.Detail, pe.Title, pe.Type, pe.Additional)
	}
	return problemResponse(r, 500, defaultDetailMessage, "", "", nil)
}

var (
	xmlInvalidChar  = regexp.MustCompile(`[^A-Za-z_\x{C0}-\x{D6}\x{D8}-\x{F6}\x{F8}-\x{2FF}\x{370}-\x{37D}\x{37F}-\x{1FFF}\x{200C}-\x{200D}\x{2070}-\x{218F}\x{2C00}-\x{2FEF}\x{3001}-\x{D7FF}\x{F900}-\x{FDCF}\x{FDF0}-\x{FFFD}\-.0-9\x{B7}\x{300}-\x{36F}\x{203F}-\x{2040}]`)
	xmlInvalidStart = regexp.MustCompile(`^[^A-Za-z_\x{C0}-\x{D6}\x{D8}-\x{F6}\x{F8}-\x{2FF}\x{370}-\x{37D}\x{37F}-\x{1FFF}\x{200C}-\x{200D}\x{2070}-\x{218F}\x{2C00}-\x{2FEF}\x{3001}-\x{D7FF}\x{F900}-\x{FDCF}\x{FDF0}-\x{FFFD}]`)
	xmlControlChars = regexp.MustCompile(`[\x00-\x09\x0B\x0C\x0E-\x1F\x7F]`)
)

// cleanXMLKey ports ProblemDetailsResponseFactory::cleanKeysForXml() for one key.
func cleanXMLKey(key string) string {
	key = strings.ReplaceAll(key, "\n", "_")
	key = xmlInvalidChar.ReplaceAllString(key, "_")
	return xmlInvalidStart.ReplaceAllString(key, "_")
}

// cleanXMLValue ports the json_encode()/json_decode() round trip plus cleanKeysForXml(): every array becomes a map
// whose keys are valid XML names. List indexes are numeric keys too, so they collapse ("0".."9" all become "_"); like
// PHP array assignment, a repeated key keeps its first position and its last value.
func cleanXMLValue(v any) any {
	clean := func(items []phpx.MapItem) phpx.Map {
		out := phpx.Map{}
		for _, it := range items {
			out = out.Set(cleanXMLKey(it.Key), cleanXMLValue(it.Value))
		}
		return out
	}
	switch t := v.(type) {
	case phpx.Map:
		return clean(t)
	case []any:
		items := make([]phpx.MapItem, len(t))
		for i, item := range t {
			items[i] = phpx.MapItem{Key: strconv.Itoa(i), Value: item}
		}
		if len(items) == 0 {
			return []any{}
		}
		return clean(items)
	case []string:
		list := make([]any, len(t))
		for i, item := range t {
			list[i] = item
		}
		return cleanXMLValue(list)
	case map[string]any:
		items := make([]phpx.MapItem, 0, len(t))
		for _, k := range sortedMapKeys(t) {
			items = append(items, phpx.MapItem{Key: k, Value: t[k]})
		}
		return clean(items)
	}
	return v
}

// xmlText reproduces a scalar the way ArrayToXml stores it: htmlspecialchars() output assigned as a DOM node value,
// which libxml decodes back, then serialized escaping only &, <, > and carriage returns.
func xmlText(v any) string {
	var s string
	switch t := v.(type) {
	case nil:
	case bool:
		if t {
			s = "1"
		}
	case string:
		s = t
	case int:
		s = strconv.Itoa(t)
	case int64:
		s = strconv.FormatInt(t, 10)
	default:
		s = string(phpx.Encode(t, 0))
	}
	s = xmlControlChars.ReplaceAllString(s, "")
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\r", "&#13;").Replace(s)
}

func writeXMLElement(b *strings.Builder, name string, value any) {
	switch t := value.(type) {
	case phpx.Map:
		if len(t) == 0 {
			fmt.Fprintf(b, "<%s/>", name)
			return
		}
		fmt.Fprintf(b, "<%s>", name)
		for _, it := range t {
			writeXMLElement(b, strings.ReplaceAll(it.Key, " ", "_"), it.Value)
		}
		fmt.Fprintf(b, "</%s>", name)
	case []any:
		fmt.Fprintf(b, "<%s/>", name)
	default:
		text := xmlText(value)
		if text == "" {
			fmt.Fprintf(b, "<%s/>", name)
			return
		}
		fmt.Fprintf(b, "<%s>%s</%s>", name, text, name)
	}
}

// problemXML ports ProblemDetailsResponseFactory::generateXmlResponse().
func problemXML(payload phpx.Map) string {
	var b strings.Builder
	b.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n")
	root := cleanXMLValue(payload).(phpx.Map)
	if len(root) == 0 {
		b.WriteString(`<problem xmlns="urn:ietf:rfc:7807"/>`)
	} else {
		b.WriteString(`<problem xmlns="urn:ietf:rfc:7807">`)
		for _, it := range root {
			writeXMLElement(&b, strings.ReplaceAll(it.Key, " ", "_"), it.Value)
		}
		b.WriteString("</problem>")
	}
	b.WriteString("\n")
	return b.String()
}

func sortedMapKeys(m map[string]any) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
