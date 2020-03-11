package types

import (
	"github.com/getsentry/sentry-go"
	"go.uber.org/zap/zapcore"
)

// HTTPRequest implements zapcore.ObjectMarshaller.
// It is supposed to be used to wrap *http.Request
// so that it can be passed to Zap and marshalled properly:
//
//   zap.Object("http_request", types.HTTPRequest{req})
type SentryUser struct {
	*sentry.User
}

// MarshalLogObject implements zapcore.ObjectMarshaller interface.
func (su SentryUser) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("email", su.Email)
	enc.AddString("id", su.ID)
	enc.AddString("ip_address", su.IPAddress)
	enc.AddString("username", su.Username)

	return nil
}
