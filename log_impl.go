package flume

import (
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"sync/atomic"
)

var _ DeprecatedLogger = (*logger)(nil)

type atomicLogger struct {
	innerLoggerPtr atomic.Value
}

func (af *atomicLogger) get() *zap.SugaredLogger {
	return af.innerLoggerPtr.Load().(*zap.SugaredLogger)
}

func (af *atomicLogger) set(logger *zap.SugaredLogger) {
	af.innerLoggerPtr.Store(logger)
}

type logger struct {
	*atomicLogger
	context []interface{}
}

// Trace is an alias for Debug.  Here for API compatibility with logxi
// deprecated: use debug
func (l *logger) Trace(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Debugw(msg, append(l.context, args...)...)
	} else {
		l.get().Debugw(msg, args...)

	}
}

// Debug logs at DBG level.  args should be alternative keys and values.  keys should be strings.
func (l *logger) Debug(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Debugw(msg, append(l.context, args...)...)
	} else {
		l.get().Debugw(msg, args...)

	}
}

// Info logs at INF level. args should be alternative keys and values.  keys should be strings.
func (l *logger) Info(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Infow(msg, append(l.context, args...)...)
	} else {
		l.get().Infow(msg, args...)

	}
}

// Warn logs at WRN level.  args should be alternative keys and values.  keys should be strings.
// deprecated: use Info.  here for API compatibility with logxi
func (l *logger) Warn(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Warnw(msg, append(l.context, args...)...)
	} else {
		l.get().Warnw(msg, args...)

	}
}

// Error logs at ERR level.  args should be alternative keys and values.  keys should be strings.
func (l *logger) Error(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Errorw(msg, append(l.context, args...)...)
	} else {
		l.get().Errorw(msg, args...)

	}
}

// Fatal logs at PNC level, and will cause a panic after logging.
// deprecated: use error.  here for API compatibility with logxi.
func (l *logger) Fatal(msg string, args ...interface{}) {
	args = normalizeArgs(args)
	if len(l.context) > 0 {
		l.get().Panicw(msg, append(l.context, args...)...)
	} else {
		l.get().Panicw(msg, args...)

	}
}

// IsDebug returns true if DBG level is enabled.
func (l *logger) IsDebug() bool {
	return l.get().Desugar().Core().Enabled(zap.DebugLevel)
}

// IsInfo returns true if INF level is enabled.
func (l *logger) IsInfo() bool {
	return l.get().Desugar().Core().Enabled(zap.InfoLevel)
}

// IsTrace returns true if DBG level is enabled.
// deprecated: use debug.  Here for logxi compatibility.
func (l *logger) IsTrace() bool {
	return l.get().Desugar().Core().Enabled(zap.DebugLevel)
}

// IsWarn returns true if WRN level is enabled.
// deprecated: use info.  here for logxi compat
func (l *logger) IsWarn() bool {
	return l.get().Desugar().Core().Enabled(zap.WarnLevel)
}

// With returns a new Logger with some context baked in.  All entries
// logged with the new logger will include this context.
//
// args should be alternative keys and values.  keys should be strings.
//
//     reqLogger := l.With("requestID", reqID)
//
func (l *logger) With(args ...interface{}) Logger {
	l2 := l.clone()
	args = normalizeArgs(args)
	switch len(args) {
	case 0:
	default:
		l2.context = append(l2.context, args...)
	}
	return l2
}

func (l *logger) clone() *logger {
	l2 := *l
	l2.context = nil
	if len(l.context) > 0 {
		l2.context = append(l2.context, l.context...)
	}
	return &l2
}

func normalizeArgs(args []interface{}) []interface{} {
	if len(args) == 1 {
		// just a bare field value was passed, with no field name
		// massage it so zap still logs it correctly
		switch args[0].(type) {
		case zapcore.Field:
			// leave it alone
		default:
			args[0] = zap.Any("", args[0])
		}
		return args
	}
	return args
}
