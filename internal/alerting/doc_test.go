package alerting

import (
	"bytes"
	"encoding/json"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"
	"time"
)

// TestWebhookPayloadDoc keeps docs/reference/webhook-payload.md true: every JSON example
// decodes into PublicAlert with no unknown field, and every field of the payload is named on
// the page.
func TestWebhookPayloadDoc(t *testing.T) {
	b, err := os.ReadFile("../../docs/reference/webhook-payload.md")
	if err != nil {
		t.Fatal(err)
	}
	doc := string(b)

	blocks := regexp.MustCompile("(?s)```json\n(.*?)```").FindAllStringSubmatch(doc, -1)
	if len(blocks) < 4 {
		t.Fatalf("found %d JSON examples", len(blocks))
	}
	for _, m := range blocks {
		src := strings.TrimSpace(m[1])
		if !strings.HasPrefix(src, "{") {
			src = "{" + src + "}" // a fragment: the fields one kind adds
		}
		dec := json.NewDecoder(bytes.NewReader([]byte(src)))
		dec.DisallowUnknownFields()
		var p PublicAlert
		if err := dec.Decode(&p); err != nil {
			t.Errorf("example does not match PublicAlert: %v\n%s", err, firstLine(src))
		}
	}

	for _, name := range jsonNames(reflect.TypeOf(PublicAlert{}), map[reflect.Type]bool{}) {
		if !strings.Contains(doc, "`"+name+"`") {
			t.Errorf("the payload field %q is not described on the page", name)
		}
	}
}

// jsonNames lists the JSON field names of t and of every struct inside it.
func jsonNames(t reflect.Type, seen map[reflect.Type]bool) []string {
	for t.Kind() == reflect.Pointer || t.Kind() == reflect.Slice {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct || t == reflect.TypeOf(time.Time{}) || seen[t] {
		return nil
	}
	seen[t] = true
	var out []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if !f.IsExported() || name == "-" || name == "" {
			continue
		}
		out = append(out, name)
		out = append(out, jsonNames(f.Type, seen)...)
	}
	return out
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}
