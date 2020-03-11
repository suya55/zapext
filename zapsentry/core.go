package zapsentry

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tchap/zapext/v2/types"

	"github.com/getsentry/sentry-go"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

//
// Levels
//

var zapLevelToSentrySeverity = map[zapcore.Level]sentry.Level{
	zapcore.DebugLevel:  sentry.LevelDebug,
	zapcore.InfoLevel:   sentry.LevelInfo,
	zapcore.WarnLevel:   sentry.LevelWarning,
	zapcore.ErrorLevel:  sentry.LevelError,
	zapcore.DPanicLevel: sentry.LevelFatal,
	zapcore.PanicLevel:  sentry.LevelFatal,
	zapcore.FatalLevel:  sentry.LevelFatal,
}

//
// Significant field keys
//

const TagPrefix = "#"

const (
	EventIDKey     = "event_id"
	ProjectKey     = "project"
	TimestampKey   = "timestamp"
	LoggerKey      = "logger"
	PlatformKey    = "platform"
	CulpritKey     = "culprit"
	ServerNameKey  = "server_name"
	ErrorKey       = "error"
	HTTPRequestKey = "http_request"
	SentryUserKey  = "sentry_user"
)

const ErrorStackTraceKey = "error_stack_trace"

const SkipKey = "_zapsentry_skip"

// Skip returns a field that tells zapsentry to skip the log entry.
func Skip() zapcore.Field {
	return zap.Bool(SkipKey, true)
}

//
// Core options
//

type Option func(*Core)

func SetStackTraceSkip(skip int) Option {
	return func(core *Core) {
		core.stSkip = skip
	}
}

func SetFlushTimeout(timeout time.Duration) Option {
	return func(core *Core) {
		core.stFlushTimeout = timeout
	}
}

//
// Core
//

const (
	DefaultFlushTimeout = 5 * time.Second
)

type Core struct {
	zapcore.LevelEnabler

	client *sentry.Client

	stSkip         int
	stFlushTimeout time.Duration

	fields []zapcore.Field
}

func NewCore(enab zapcore.LevelEnabler, client *sentry.Client, options ...Option) *Core {
	core := &Core{
		LevelEnabler:   enab,
		client:         client,
		stFlushTimeout: DefaultFlushTimeout,
	}

	for _, opt := range options {
		opt(core)
	}

	return core
}

func (core *Core) With(fields []zapcore.Field) zapcore.Core {
	// Clone core.
	clone := *core

	// Clone and append fields.
	clone.fields = make([]zapcore.Field, len(core.fields)+len(fields))
	copy(clone.fields, core.fields)
	copy(clone.fields[len(core.fields):], fields)

	// Done.
	return &clone
}

func (core *Core) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if core.Enabled(entry.Level) {
		return checked.AddCore(entry, core)
	}
	return checked
}

func (core *Core) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	// When set, relevant Sentry interfaces are added.
	var (
		err error
		req *http.Request
		user *sentry.User
	)

	//hub := sentry.CurrentHub()
	hub := sentry.CurrentHub().Clone()
	// Create a Sentry Event.
	event := sentry.NewEvent()
	event.Message = entry.Message
	event.Platform = "go"

	// Process entry.
	event.Level = zapLevelToSentrySeverity[entry.Level]
	event.Timestamp = entry.Time.Unix()

	// Process fields.
	encoder := zapcore.NewMapObjectEncoder()

	// processField processes the given field.
	// When false is returned, the whole entry is to be skipped.
	processField := func(field zapcore.Field) bool {
		// Check for significant keys.
		switch field.Key {
		case EventIDKey:
			event.EventID = sentry.EventID(field.String)

		case PlatformKey:
			event.Platform = field.String

		case SentryUserKey:
			user = field.Interface.(*sentry.User)

		case ServerNameKey:
			event.ServerName = field.String

		case ErrorKey:
			if ex, ok := field.Interface.(error); ok {
				err = ex
			} else {
				field.AddTo(encoder)
			}

		case HTTPRequestKey:
			switch r := field.Interface.(type) {
			case *http.Request:
				req = r
			case types.HTTPRequest:
				req = r.R
			case *types.HTTPRequest:
				req = r.R
			default:
				field.AddTo(encoder)
			}

		case SkipKey:
			return false

		default:
			// Add to the encoder in case this is not a significant key.
			field.AddTo(encoder)
		}

		return true
	}

	// Process core fields first.
	for _, field := range core.fields {
		if !processField(field) {
			return nil
		}
	}

	// Then process the fields passed directly.
	// These can be then used to overwrite the core fields.
	for _, field := range fields {
		if !processField(field) {
			return nil
		}
	}

	// Split fields into tags and extra.
	tags := make(map[string]string)
	extra := make(map[string]interface{})

	for key, value := range encoder.Fields {
		if strings.HasPrefix(key, TagPrefix) {
			key = key[len(TagPrefix):]
			if v, ok := value.(string); ok {
				tags[key] = v
			} else {
				tags[key] = fmt.Sprintf("%v", value)
			}
		} else {
			extra[key] = value
		}
	}

	hub.WithScope(func(scope *sentry.Scope) {
		if req != nil {
			scope.SetRequest(sentry.Request{}.FromHTTPRequest(req))
		}
		scope.SetUser(*user)
		scope.SetTags(tags)
		scope.SetExtras(extra)
		if err != nil {
			hub.CaptureException(err)
		} else {
			hub.CaptureEvent(event)
		}
	})
	return nil
}

func (core *Core) Sync() error {
	core.client.Flush(core.stFlushTimeout)
	return nil
}

type StackTracer interface {
	StackTrace() errors.StackTrace
}
