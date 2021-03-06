package flume

import (
	"errors"
	"fmt"
	"github.com/ansel1/merry"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"io"
	"os"
	"strings"
	"sync"
)

type loggerInfo struct {
	levelEnabler zapcore.LevelEnabler
	atomicLogger atomicLogger
}

// Factory is a log management core.  It spawns loggers.  The Factory has
// methods for dynamically reconfiguring all the loggers spawned from Factory.
//
// The flume package has mirrors of most of the functions which delegate to a
// default, package-level factory.
type Factory struct {
	defaultLevel zap.AtomicLevel

	encoder zapcore.Encoder
	out     io.Writer

	loggers map[string]*loggerInfo
	sync.Mutex

	addCaller bool
}

// Encoder serializes log entries.  Re-exported from zap for now to avoid exporting zap.
type Encoder zapcore.Encoder

// NewFactory returns a factory.  The default level is set to OFF (all logs disabled)
func NewFactory() *Factory {
	f := Factory{
		defaultLevel: zap.NewAtomicLevel(),
		loggers:      map[string]*loggerInfo{},
	}
	f.SetDefaultLevel(OffLevel)

	return &f
}

func (r *Factory) getEncoder() zapcore.Encoder {
	if r.encoder == nil {
		return NewLTSVEncoder(NewEncoderConfig())
	}
	return r.encoder
}

// SetEncoder sets the encoder for all loggers created by (in the past or future) this factory.
func (r *Factory) SetEncoder(e Encoder) {
	r.Lock()
	defer r.Unlock()
	r.encoder = e
	r.refreshLoggers()
}

// SetOut sets the output writer for all logs produced by this factory.
// Returns a function which sets the output writer back to the prior setting.
func (r *Factory) SetOut(w io.Writer) func() {
	r.Lock()
	defer r.Unlock()
	prior := r.out
	r.out = w
	r.refreshLoggers()
	return func() {
		r.SetOut(prior)
	}
}

// SetAddCaller enables adding the logging callsite (file and line number) to the log entries.
func (r *Factory) SetAddCaller(b bool) {
	r.Lock()
	defer r.Unlock()
	r.addCaller = b
	r.refreshLoggers()
}

func (r *Factory) getOut() io.Writer {
	if r.out == nil {
		return os.Stdout
	}
	return r.out
}

func (r *Factory) refreshLoggers() {
	for name, info := range r.loggers {
		info.atomicLogger.set(r.newLogger(name, info))
	}
}

func (r *Factory) getLoggerInfo(name string) *loggerInfo {
	info, found := r.loggers[name]
	if !found {
		info = &loggerInfo{}
		r.loggers[name] = info
		info.atomicLogger.set(r.newLogger(name, info))
	}
	return info
}

func (r *Factory) newLogger(name string, info *loggerInfo) *zap.SugaredLogger {
	var l zapcore.LevelEnabler
	switch {
	case info.levelEnabler != nil:
		l = info.levelEnabler
	default:
		l = r.defaultLevel
	}
	fac := zapcore.NewCore(
		r.getEncoder(),
		zapcore.AddSync(r.getOut()),
		l,
	)

	opts := []zap.Option{zap.AddCallerSkip(1)}

	if r.addCaller {
		opts = append(opts, zap.AddCaller())
	}
	return zap.New(fac, opts...).Named(name).Sugar()
}

// NewDeprecatedLogger returns a new DeprecatedLogger.
func (r *Factory) NewDeprecatedLogger(name string) DeprecatedLogger {
	r.Lock()
	defer r.Unlock()
	info := r.getLoggerInfo(name)
	return &logger{
		atomicLogger: &info.atomicLogger,
	}
}

// NewLogger returns a new Logger
func (r *Factory) NewLogger(name string) Logger {
	return r.NewDeprecatedLogger(name)
}

func (r *Factory) setLevel(name string, l Level) {
	info := r.getLoggerInfo(name)
	info.levelEnabler = zapcore.Level(l)
}

// SetLevel sets the log level for a particular named logger.  All loggers with this same
// are affected, in the past or future.
func (r *Factory) SetLevel(name string, l Level) {
	r.Lock()
	defer r.Unlock()
	r.setLevel(name, l)
	r.refreshLoggers()
}

// SetDefaultLevel sets the default log level for all loggers which don't have a specific level
// assigned to them
func (r *Factory) SetDefaultLevel(l Level) {
	r.defaultLevel.SetLevel(zapcore.Level(l))
}

func parseConfigString(s string) map[string]interface{} {
	if s == "" {
		return nil
	}
	items := strings.Split(s, ",")
	m := map[string]interface{}{}
	for _, setting := range items {
		parts := strings.Split(setting, "=")

		switch len(parts) {
		case 1:
			name := parts[0]
			if strings.HasPrefix(name, "-") {
				m[name[1:]] = false
			} else {
				m[name] = true
			}
		case 2:
			m[parts[0]] = parts[1]
		}
	}
	return m
}

// LevelsString reconfigures all the log levels.  All named loggers not in the
// config string are reset to the default level.  If the string contains an "*"
// entry, that will set the default log level.  The format is compatible with logxi's
// LOGXI environment variable.  Examples:
//
//     *		// set all loggers to default, and set default level to ALL
//     *=INF		// same, but set default level to INF
//     *,sql=WRN	// set default to ALL, set sql to WRN
//     *=INF,http=ALL	// set default to INF, set http to ALL
//     *=INF,http	// same as above.  If name has no level, level is set to ALL
//     *=INF,-http	// set default to INF, set http to OFF
//     http=INF		// leave default setting unchanged.  all loggers (except "http") are still reset to default level
//
func (r *Factory) LevelsString(s string) error {
	m := parseConfigString(s)
	levelMap := map[string]Level{}
	var errMsgs []string
	for key, val := range m {
		switch t := val.(type) {
		case bool:
			if t {
				levelMap[key] = AllLevel
			} else {
				levelMap[key] = OffLevel
			}
		case string:
			l, err := levelForAbbr(t)
			levelMap[key] = l
			if err != nil {
				errMsgs = append(errMsgs, err.Error())
			}
		}
	}
	// first, check default setting
	if defaultLevel, found := levelMap["*"]; found {
		r.SetDefaultLevel(defaultLevel)
		delete(levelMap, "*")
	}

	r.Lock()
	defer r.Unlock()

	// iterate through the current level map first.
	// Any existing loggers which aren't in the levels map
	// get reset to the default level.
	for name, info := range r.loggers {
		if _, found := levelMap[name]; !found {
			info.levelEnabler = r.defaultLevel
		}
	}

	// iterate through the levels map and set the specific levels
	for name, level := range levelMap {
		r.setLevel(name, level)
	}

	if len(errMsgs) > 0 {
		return merry.New("errors parsing config string: " + strings.Join(errMsgs, ", "))
	}

	r.refreshLoggers()
	return nil
}

// Configure uses a serializable struct to configure most of the options.
// This is useful when fully configuring the logging from an env var or file.
//
// The zero value for Config will set defaults for a standard, production logger:
//
// See the Config docs for details on settings.
func (r *Factory) Configure(cfg Config) error {

	r.SetDefaultLevel(cfg.DefaultLevel)

	var encCfg *EncoderConfig
	if cfg.EncoderConfig != nil {
		encCfg = cfg.EncoderConfig
	} else {
		if cfg.Development {
			encCfg = NewDevelopmentEncoderConfig()
		} else {
			encCfg = NewEncoderConfig()
		}
	}

	// These *Caller properties *must* be set or errors
	// will occur
	if encCfg.EncodeCaller == nil {
		encCfg.EncodeCaller = zapcore.ShortCallerEncoder
	}
	if encCfg.EncodeLevel == nil {
		encCfg.EncodeLevel = AbbrLevelEncoder
	}

	var encoder zapcore.Encoder
	switch cfg.Encoding {
	case "json":
		encoder = NewJSONEncoder(encCfg)
	case "ltsv":
		encoder = NewLTSVEncoder(encCfg)
	case "term":
		encoder = NewConsoleEncoder(encCfg)
	case "term-color":
		encoder = NewColorizedConsoleEncoder(encCfg, nil)
	case "console":
		encoder = zapcore.NewConsoleEncoder((zapcore.EncoderConfig)(*encCfg))
	case "":
		if cfg.Development {
			encoder = NewColorizedConsoleEncoder(encCfg, nil)
		} else {
			encoder = NewLTSVEncoder(encCfg)
		}
	default:
		return merry.Errorf("%s is not a valid encoding, must be one of: json, ltsv, term, or term-color", cfg.Encoding)
	}

	var addCaller bool
	if cfg.AddCaller != nil {
		addCaller = *cfg.AddCaller
	} else {
		addCaller = cfg.Development
	}

	// todo: break up LevelsString into parse and apply phases, so I
	// can avoid taking the lock twice
	r.LevelsString(cfg.Levels)
	r.Lock()
	defer r.Unlock()
	r.encoder = encoder
	r.addCaller = addCaller
	r.refreshLoggers()
	return nil
}

func levelForAbbr(abbr string) (Level, error) {
	switch strings.ToLower(abbr) {
	case "":
		return AllLevel, nil
	case "off":
		return OffLevel, nil
	case "trc", "trace":
		return DebugLevel, errors.New("TRC is deprecated, mapped to DEBUG")
	case "dbg", "debug":
		return DebugLevel, nil
	case "inf", "info":
		return InfoLevel, nil
	case "wrn", "warn":
		return WarnLevel, errors.New("WRN is deprecated, use INF")
	case "err", "error":
		return ErrorLevel, nil
	case "pan", "panic":
		return PanicLevel, errors.New("PAN is deprecated, use ERR")
	case "ftl", "fatal":
		return PanicLevel, errors.New("FTL is deprecated, use ERR, mapped to PANIC")
	default:
		return WarnLevel, fmt.Errorf("%s not recognized level, defaulting to warn", abbr)
	}
}
