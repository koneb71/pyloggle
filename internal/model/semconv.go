package model

// OpenTelemetry HTTP semantic conventions as emitted by FastAPI's native telemetry (0.142+)
// and the contrib instrumentations. The dashboards' "otel metrics" request source reads these;
// a rename upstream is a one-line change here.
const (
	// HTTPServerDuration is the request-duration histogram (unit: s).
	HTTPServerDuration = "http.server.request.duration"
	// HTTPServerActiveRequests is the in-flight request gauge (up-down counter).
	HTTPServerActiveRequests = "http.server.active_requests"

	AttrHTTPRoute      = "http.route"
	AttrHTTPMethod     = "http.request.method"
	AttrHTTPStatusCode = "http.response.status_code"
	AttrErrorType      = "error.type"
)

// AttrSDKEventID is the span attribute the pyloggle SDK sets on the active OpenTelemetry
// span when it captures an exception: the span's own failure must not become a second issue.
const AttrSDKEventID = "pyloggle.event_id"
