package otlp

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	commonv1 "go.opentelemetry.io/proto/otlp/common/v1"
	logsv1 "go.opentelemetry.io/proto/otlp/logs/v1"
	resourcev1 "go.opentelemetry.io/proto/otlp/resource/v1"
	tracev1 "go.opentelemetry.io/proto/otlp/trace/v1"

	"pyloggle/internal/model"
)

// resourceInfo is what we lift out of OTEL resource attributes.
type resourceInfo struct {
	service, env, release, hostname string
	runtime                         model.RuntimeInfo
	sdk                             model.SDKInfo
	attrs                           model.Attrs
}

func readResource(res *resourcev1.Resource) resourceInfo {
	ri := resourceInfo{attrs: model.Attrs{}}
	if res == nil {
		return ri
	}
	for _, kv := range res.GetAttributes() {
		v := anyValueString(kv.GetValue())
		switch kv.GetKey() {
		case "service.name":
			ri.service = v
		case "deployment.environment", "deployment.environment.name":
			ri.env = v
		case "service.version":
			ri.release = v
		case "host.name":
			ri.hostname = v
		case "process.pid":
			ri.runtime.PID, _ = strconv.Atoi(v)
		case "process.runtime.name":
			ri.runtime.Name = v
		case "process.runtime.version":
			ri.runtime.Version = v
		case "os.description", "os.type":
			if ri.runtime.Platform == "" {
				ri.runtime.Platform = v
			}
		case "telemetry.sdk.name":
			ri.sdk.Name = v
		case "telemetry.sdk.version":
			ri.sdk.Version = v
		case "telemetry.sdk.language":
			if ri.runtime.Name == "" {
				ri.runtime.Name = v
			}
		default:
			ri.attrs["resource."+kv.GetKey()] = v
		}
	}
	if ri.sdk.Name == "" {
		ri.sdk.Name = "opentelemetry"
	}
	return ri
}

func (ri resourceInfo) apply(ev *model.Event) {
	ev.Service = ri.service
	ev.Env = ri.env
	ev.Release = ri.release
	ev.Hostname = ri.hostname
	ev.Source = "otlp"
	rt := ri.runtime
	ev.Runtime = &rt
	sdk := ri.sdk
	ev.SDK = &sdk
	for k, v := range ri.attrs {
		ev.Attrs = ev.Attrs.Set(k, v)
	}
}

// anyValueString renders an OTEL AnyValue as a string (nested values as compact JSON).
func anyValueString(v *commonv1.AnyValue) string {
	if v == nil {
		return ""
	}
	switch x := v.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return x.StringValue
	case *commonv1.AnyValue_BoolValue:
		return strconv.FormatBool(x.BoolValue)
	case *commonv1.AnyValue_IntValue:
		return strconv.FormatInt(x.IntValue, 10)
	case *commonv1.AnyValue_DoubleValue:
		return strconv.FormatFloat(x.DoubleValue, 'f', -1, 64)
	case *commonv1.AnyValue_BytesValue:
		return hex.EncodeToString(x.BytesValue)
	case *commonv1.AnyValue_ArrayValue, *commonv1.AnyValue_KvlistValue:
		b, _ := json.Marshal(anyValueJSON(v))
		return string(b)
	}
	return ""
}

func anyValueJSON(v *commonv1.AnyValue) any {
	switch x := v.GetValue().(type) {
	case *commonv1.AnyValue_StringValue:
		return x.StringValue
	case *commonv1.AnyValue_BoolValue:
		return x.BoolValue
	case *commonv1.AnyValue_IntValue:
		return x.IntValue
	case *commonv1.AnyValue_DoubleValue:
		return x.DoubleValue
	case *commonv1.AnyValue_BytesValue:
		return hex.EncodeToString(x.BytesValue)
	case *commonv1.AnyValue_ArrayValue:
		out := make([]any, 0, len(x.ArrayValue.GetValues()))
		for _, e := range x.ArrayValue.GetValues() {
			out = append(out, anyValueJSON(e))
		}
		return out
	case *commonv1.AnyValue_KvlistValue:
		out := map[string]any{}
		for _, kv := range x.KvlistValue.GetValues() {
			out[kv.GetKey()] = anyValueJSON(kv.GetValue())
		}
		return out
	}
	return nil
}

func unixNano(n uint64) model.Timestamp {
	if n == 0 {
		return model.Timestamp{}
	}
	return model.At(time.Unix(0, int64(n)).UTC())
}

// idHex renders a trace/span id. OTLP/JSON writes ids as hex strings while protojson decodes
// bytes fields as base64, so a 32-char hex id arrives as 24 bytes (16-char span ids as 12):
// re-encoding to base64 recovers the original hex text.
func idHex(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	if len(b) == 24 || len(b) == 12 {
		if s := base64.StdEncoding.EncodeToString(b); isHex(s) {
			return strings.ToLower(s)
		}
	}
	return hex.EncodeToString(b)
}

func isHex(s string) bool {
	for _, c := range s {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')) {
			return false
		}
	}
	return s != ""
}

// severityLevel maps OTEL severity numbers (1-24) onto the canonical levels.
func severityLevel(n logsv1.SeverityNumber, text string) string {
	switch {
	case n >= 21:
		return model.LevelFatal
	case n >= 17:
		return model.LevelError
	case n >= 13:
		return model.LevelWarn
	case n >= 9:
		return model.LevelInfo
	case n >= 1:
		return model.LevelDebug
	}
	return model.NormalizeLevel(text)
}

// applyCommonAttrs lifts well-known attributes into structured fields and keeps the rest.
// It returns true when exception.* attributes produced an exception.
func applyCommonAttrs(ev *model.Event, attrs []*commonv1.KeyValue) {
	var excType, excMsg, excStack string
	var escaped bool
	var h model.HTTPInfo
	hasHTTP := false
	for _, kv := range attrs {
		k := kv.GetKey()
		v := anyValueString(kv.GetValue())
		switch k {
		case "exception.type":
			excType = v
		case "exception.message":
			excMsg = v
		case "exception.stacktrace":
			excStack = v
		case "exception.escaped":
			escaped = v == "true"
		case "http.request.method", "http.method":
			h.Method, hasHTTP = v, true
		case "http.route":
			h.Route, hasHTTP = v, true
		case "http.response.status_code", "http.status_code":
			if n, err := strconv.Atoi(v); err == nil {
				h.Status, hasHTTP = uint16(n), true
			}
		case "url.full", "http.url":
			h.URL, hasHTTP = v, true
		case "url.path", "http.target":
			// Raw paths are not routes: they would explode the route cardinality of the
			// request statistics. They stay available as attributes.
			ev.Attrs = ev.Attrs.Set(k, v)
			hasHTTP = true
		case "url.query":
			h.Query, hasHTTP = v, true
		case "user_agent.original", "http.user_agent":
			h.UserAgent, hasHTTP = v, true
		case "client.address", "http.client_ip", "net.peer.ip":
			h.ClientIP, hasHTTP = v, true
		case "server.address", "net.host.name":
			h.ServerAddr, hasHTTP = v, true
		case "enduser.id", "user.id":
			if ev.User == nil {
				ev.User = &model.UserInfo{}
			}
			ev.User.ID = v
		case "user.email":
			if ev.User == nil {
				ev.User = &model.UserInfo{}
			}
			ev.User.Email = v
		case "request_id", "http.request_id", "request.id":
			ev.RequestID = v
		case "code.filepath", "code.function", "code.lineno", "code.namespace":
			ev.Attrs = ev.Attrs.Set(k, v)
		case "thread.name":
			if ev.Runtime == nil {
				ev.Runtime = &model.RuntimeInfo{}
			}
			ev.Runtime.Thread = v
		case "service.version":
			ev.Release = v
		default:
			ev.Attrs = ev.Attrs.Set(k, v)
		}
	}
	if hasHTTP {
		ev.HTTP = &h
	}
	if excType != "" || excMsg != "" || excStack != "" {
		exc := &model.Exception{Message: excMsg}
		exc.Module, exc.Type = SplitTypeModule(excType)
		if excStack != "" {
			if t, m, frames, chain, ok := ParsePythonTraceback(excStack); ok {
				exc.Frames, exc.Chain = frames, chain
				if exc.Type == "" {
					exc.Module, exc.Type = SplitTypeModule(t)
				}
				if exc.Message == "" {
					exc.Message = m
				}
			} else {
				ev.Attrs = ev.Attrs.Set("exception.stacktrace", excStack)
			}
		}
		if exc.Type == "" {
			exc.Type = "Exception"
		}
		ev.Exception = exc
		handled := !escaped
		ev.Mechanism = &model.Mechanism{Type: "otlp", Handled: &handled}
	}
}

// LogsToEvents maps an OTLP logs payload onto events.
func LogsToEvents(ld *logsv1.LogsData, now time.Time) []model.Event {
	var out []model.Event
	for _, rl := range ld.GetResourceLogs() {
		ri := readResource(rl.GetResource())
		for _, sl := range rl.GetScopeLogs() {
			scope := sl.GetScope().GetName()
			for _, lr := range sl.GetLogRecords() {
				ev := model.Event{Attrs: model.Attrs{}}
				ri.apply(&ev)
				ev.Timestamp = unixNano(lr.GetTimeUnixNano())
				if ev.Timestamp.IsZero() {
					ev.Timestamp = unixNano(lr.GetObservedTimeUnixNano())
				}
				if ev.Timestamp.IsZero() {
					ev.Timestamp = model.At(now)
				}
				ev.Level = severityLevel(lr.GetSeverityNumber(), lr.GetSeverityText())
				ev.Logger = scope
				ev.Message = anyValueString(lr.GetBody())
				ev.TraceID = idHex(lr.GetTraceId())
				ev.SpanID = idHex(lr.GetSpanId())
				applyCommonAttrs(&ev, lr.GetAttributes())
				if ev.Logger == "" {
					ev.Logger = ev.Attrs.Get("code.namespace")
				}
				out = append(out, ev)
			}
		}
	}
	return out
}

// TracesToEvents maps spans onto events only (see TracesToEventsAndSpans).
func TracesToEvents(td *tracev1.TracesData, storeServerSpans bool, now time.Time) []model.Event {
	evs, _ := TracesToEventsAndSpans(td, storeServerSpans, now)
	return evs
}

// TracesToEventsAndSpans walks the payload once. Every span becomes a Span row; exception
// events become error events, ERROR spans without one become error logs, and (optionally) OK
// server spans become "request" logs with a duration.
func TracesToEventsAndSpans(td *tracev1.TracesData, storeServerSpans bool, now time.Time) ([]model.Event, []model.Span) {
	var out []model.Event
	var spans []model.Span
	for _, rs := range td.GetResourceSpans() {
		ri := readResource(rs.GetResource())
		for _, ss := range rs.GetScopeSpans() {
			scope := ss.GetScope().GetName()
			for _, sp := range ss.GetSpans() {
				base := model.Event{Attrs: model.Attrs{}}
				ri.apply(&base)
				base.Logger = scope
				base.TraceID = idHex(sp.GetTraceId())
				base.SpanID = idHex(sp.GetSpanId())
				base.Transaction = sp.GetName()
				applyCommonAttrs(&base, sp.GetAttributes())
				base.Exception = nil // span attributes rarely carry exception.*; events do
				if sp.GetEndTimeUnixNano() > sp.GetStartTimeUnixNano() {
					base.DurationMS = float64(sp.GetEndTimeUnixNano()-sp.GetStartTimeUnixNano()) / 1e6
				}
				spans = append(spans, spanRow(ri, scope, sp, base, now))
				hadException := false
				for _, sev := range sp.GetEvents() {
					if sev.GetName() != "exception" {
						continue
					}
					ev := base
					ev.Attrs = cloneAttrs(base.Attrs)
					ev.Timestamp = unixNano(sev.GetTimeUnixNano())
					if ev.Timestamp.IsZero() {
						ev.Timestamp = model.At(now)
					}
					applyCommonAttrs(&ev, sev.GetAttributes())
					if ev.Exception == nil {
						continue
					}
					ev.Level = model.LevelError
					ev.Message = ev.Exception.Message
					if ev.Message == "" {
						ev.Message = ev.Exception.Type
					}
					hadException = true
					out = append(out, ev)
				}
				status := sp.GetStatus()
				if !hadException && status.GetCode() == tracev1.Status_STATUS_CODE_ERROR {
					ev := base
					ev.Attrs = cloneAttrs(base.Attrs)
					ev.Timestamp = unixNano(sp.GetEndTimeUnixNano())
					if ev.Timestamp.IsZero() {
						ev.Timestamp = model.At(now)
					}
					ev.Level = model.LevelError
					// The span name leads so the issue title says what failed, not only why
					// ("GET /inventory/{sku} failed: 503" rather than "503").
					ev.Message = sp.GetName() + " failed"
					if msg := strings.TrimSpace(status.GetMessage()); msg != "" {
						ev.Message += ": " + msg
					}
					out = append(out, ev)
					continue
				}
				if storeServerSpans && !hadException && sp.GetKind() == tracev1.Span_SPAN_KIND_SERVER {
					ev := base
					ev.Attrs = cloneAttrs(base.Attrs)
					ev.Timestamp = unixNano(sp.GetEndTimeUnixNano())
					if ev.Timestamp.IsZero() {
						ev.Timestamp = model.At(now)
					}
					ev.Level = model.LevelInfo
					if ev.HTTP != nil && ev.HTTP.Status >= 500 {
						ev.Level = model.LevelError
					} else if ev.HTTP != nil && ev.HTTP.Status >= 400 {
						ev.Level = model.LevelWarn
					}
					ev.Message = "request"
					ev.Attrs = ev.Attrs.Set("latency_ms", strconv.FormatInt(int64(ev.DurationMS), 10))
					out = append(out, ev)
				}
			}
		}
	}
	return out, spans
}

// spanRow builds the stored span from the decoded span and the event template that already
// carries the resource and attribute mapping. Resource attributes stay on the log rows; the
// span keeps its own attributes plus the HTTP details that have no column.
func spanRow(ri resourceInfo, scope string, sp *tracev1.Span, base model.Event, now time.Time) model.Span {
	row := model.Span{
		Service: ri.service, Env: ri.env, Release: ri.release, Hostname: ri.hostname, Scope: scope,
		TraceID: base.TraceID, SpanID: base.SpanID, ParentSpanID: idHex(sp.GetParentSpanId()),
		Name: sp.GetName(), Kind: spanKind(sp.GetKind()), Status: spanStatus(sp.GetStatus().GetCode()), StatusMsg: sp.GetStatus().GetMessage(),
		StartTS: unixNano(sp.GetStartTimeUnixNano()).Time, Attrs: model.Attrs{},
	}
	if row.StartTS.IsZero() {
		row.StartTS = now.UTC()
	}
	if sp.GetEndTimeUnixNano() > sp.GetStartTimeUnixNano() {
		row.DurationUS = (sp.GetEndTimeUnixNano() - sp.GetStartTimeUnixNano()) / 1000
	}
	for k, v := range base.Attrs {
		if strings.HasPrefix(k, "resource.") {
			continue
		}
		row.Attrs[k] = v
	}
	if base.RequestID != "" {
		row.Attrs["request_id"] = base.RequestID
	}
	if h := base.HTTP; h != nil {
		row.HTTPMethod, row.HTTPRoute, row.HTTPStatus = h.Method, h.Route, h.Status
		for k, v := range map[string]string{"url.full": h.URL, "url.query": h.Query, "user_agent.original": h.UserAgent, "client.address": h.ClientIP, "server.address": h.ServerAddr} {
			if v != "" {
				row.Attrs[k] = v
			}
		}
	}
	if base.User != nil && base.User.ID != "" {
		row.Attrs["enduser.id"] = base.User.ID
	}
	for _, sev := range sp.GetEvents() {
		e := model.SpanEvent{TS: unixNano(sev.GetTimeUnixNano()).Time, Name: sev.GetName()}
		if e.TS.IsZero() {
			e.TS = row.StartTS
		}
		if attrs := sev.GetAttributes(); len(attrs) > 0 {
			e.Attrs = model.Attrs{}
			for _, kv := range attrs {
				e.Attrs[kv.GetKey()] = anyValueString(kv.GetValue())
			}
		}
		row.Events = append(row.Events, e)
	}
	return row
}

func spanKind(k tracev1.Span_SpanKind) string {
	switch k {
	case tracev1.Span_SPAN_KIND_INTERNAL:
		return "internal"
	case tracev1.Span_SPAN_KIND_SERVER:
		return "server"
	case tracev1.Span_SPAN_KIND_CLIENT:
		return "client"
	case tracev1.Span_SPAN_KIND_PRODUCER:
		return "producer"
	case tracev1.Span_SPAN_KIND_CONSUMER:
		return "consumer"
	}
	return "unspecified"
}

func spanStatus(c tracev1.Status_StatusCode) string {
	switch c {
	case tracev1.Status_STATUS_CODE_OK:
		return "ok"
	case tracev1.Status_STATUS_CODE_ERROR:
		return "error"
	}
	return "unset"
}

func cloneAttrs(a model.Attrs) model.Attrs {
	out := make(model.Attrs, len(a)+4)
	for k, v := range a {
		out[k] = v
	}
	return out
}

// strip removes a prefix for mapping helpers in tests.
func strip(s, prefix string) string { return strings.TrimPrefix(s, prefix) }
