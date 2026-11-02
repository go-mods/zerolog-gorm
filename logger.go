package zerologgorm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"gorm.io/gorm/utils"
)

type GormLogger struct {
	// SlowThreshold is the delay which define the query as slow
	SlowThreshold time.Duration

	// IgnoreRecordNotFoundError is to ignore when the record is not found
	IgnoreRecordNotFoundError bool

	// FieldsOrder defines the order of fields in output.
	FieldsOrder []string

	// FieldsExclude defines contextual fields to not display in output.
	FieldsExclude []string

	// logLevel is the level set by LogMode. Its zero value means LogMode was
	// never called: nothing is filtered here and only the level of the zerolog
	// logger decides what is written.
	logLevel logger.LogLevel
}

// The field names below are read each time a query is logged. FieldsOrder and
// FieldsExclude refer to fields by these names, so set them before building a
// GormLogger, and before any query runs since they are not safe to change
// concurrently.
var (
	// TimestampFieldName is the field name used for the timestamp field.
	TimestampFieldName = zerolog.TimestampFieldName

	// DurationFieldName is the field name used for the duration field.
	DurationFieldName = "elapsed"

	// FileFieldName is the field name used for the file field.
	FileFieldName = "file"

	// SqlFieldName is the field name used for the sql field.
	SqlFieldName = "sql"

	// RowsFieldName is the field name used for the rows field.
	RowsFieldName = "rows"
)

// GormLogger implements the logger.Interface
var _ logger.Interface = &GormLogger{}

// NewGormLogger creates and initializes a new GormLogger.
func NewGormLogger() *GormLogger {
	l := &GormLogger{
		FieldsOrder: gormDefaultFieldsOrder(),
	}

	return l
}

// gormDefaultFieldsOrder defines the default order of fields
func gormDefaultFieldsOrder() []string {
	return []string{
		TimestampFieldName,
		DurationFieldName,
		FileFieldName,
		SqlFieldName,
		RowsFieldName,
	}
}

// isExcluded check if a field is excluded from the output
func (l GormLogger) isExcluded(field string) bool {
	if l.FieldsExclude == nil {
		return false
	}
	for _, f := range l.FieldsExclude {
		if f == field {
			return true
		}
	}

	return false
}

// enabled reports whether messages of the given GORM level pass the level set by
// LogMode.
func (l GormLogger) enabled(level logger.LogLevel) bool {
	return l.logLevel == 0 || l.logLevel >= level
}

// LogMode returns a copy of the logger limited to the given GORM level; the
// receiver is left untouched, as db.Debug() and gorm.Session rely on.
//
// The level only filters, the same way GORM's default logger does: logger.Silent
// drops everything, logger.Error keeps failed queries, logger.Warn adds slow
// queries, logger.Info adds all other queries. What remains still goes through the
// zerolog logger, which writes successful queries at trace level: db.Debug() does
// not show them if the zerolog logger is above that level.
func (l *GormLogger) LogMode(level logger.LogLevel) logger.Interface {
	// Below logger.Silent, GORM logs nothing; the zero value must stay "not set".
	if level < logger.Silent {
		level = logger.Silent
	}

	clone := *l
	clone.logLevel = level

	return &clone
}

// Info print info
func (l GormLogger) Info(ctx context.Context, msg string, data ...interface{}) {
	if !l.enabled(logger.Info) {
		return
	}
	zerolog.Ctx(ctx).Info().Msg(fmt.Sprintf(msg, data...))
}

// Warn print warn messages
func (l GormLogger) Warn(ctx context.Context, msg string, data ...interface{}) {
	if !l.enabled(logger.Warn) {
		return
	}
	zerolog.Ctx(ctx).Warn().Msg(fmt.Sprintf(msg, data...))
}

// Error print error messages
func (l GormLogger) Error(ctx context.Context, msg string, data ...interface{}) {
	if !l.enabled(logger.Error) {
		return
	}
	zerolog.Ctx(ctx).Error().Msg(fmt.Sprintf(msg, data...))
}

// Trace print sql message
//
// TimestampFieldName is emitted once per line. When the zerolog logger already
// adds that field, for instance through zerolog.Context.Timestamp, its value is
// kept and FieldsOrder and FieldsExclude have no effect on it. Otherwise Trace adds
// the time at which the query began. To find out, a blank event is written to a
// private buffer whenever the logger differs from the previous one, which runs its
// hooks one extra time.
func (l GormLogger) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {

	// get zerolog from context
	z := zerolog.Ctx(ctx)

	// return if zerolog is disabled or the level set by LogMode is silent
	if z.GetLevel() == zerolog.Disabled || !l.enabled(logger.Error) {
		return
	}

	if l.FieldsOrder == nil {
		l.FieldsOrder = gormDefaultFieldsOrder()
	}

	elapsed := time.Since(begin)

	var event *zerolog.Event
	var eventError bool
	var eventWarn bool

	// set message level
	if err != nil && (!l.IgnoreRecordNotFoundError || !errors.Is(err, gorm.ErrRecordNotFound)) {
		eventError = true
		event = z.Error()
	} else if l.SlowThreshold != 0 && elapsed > l.SlowThreshold && l.enabled(logger.Warn) {
		eventWarn = true
		event = z.Warn()
	} else if l.enabled(logger.Info) {
		event = z.Trace()
	} else {
		return
	}

	sql, rows := fc()

	// add fields
	for _, f := range l.FieldsOrder {
		// add time field
		if f == TimestampFieldName && !l.isExcluded(f) && event.Enabled() && !loggerHasTimestamp(z) {
			event.Time(TimestampFieldName, begin)
		}

		// add duration field
		if f == DurationFieldName && !l.isExcluded(f) {
			var durationFieldName string
			switch zerolog.DurationFieldUnit {
			case time.Nanosecond:
				durationFieldName = DurationFieldName + "_ns"
			case time.Microsecond:
				durationFieldName = DurationFieldName + "_us"
			case time.Millisecond:
				durationFieldName = DurationFieldName + "_ms"
			case time.Second:
				durationFieldName = DurationFieldName
			case time.Minute:
				durationFieldName = DurationFieldName + "_min"
			case time.Hour:
				durationFieldName = DurationFieldName + "_hr"
			default:
				z.Error().Interface("zerolog.DurationFieldUnit", zerolog.DurationFieldUnit).Msg("unknown value for DurationFieldUnit")
				durationFieldName = DurationFieldName
			}
			event.Dur(durationFieldName, elapsed)
		}

		// add file field
		if f == FileFieldName && !l.isExcluded(f) {
			event.Str(FileFieldName, utils.FileWithLineNum())
		}

		// add sql field
		if f == SqlFieldName && !l.isExcluded(f) {
			if sql != "" {
				event.Str(SqlFieldName, sql)
			}
		}

		// add rows field
		if f == RowsFieldName && !l.isExcluded(f) {
			if rows > -1 {
				event.Int64(RowsFieldName, rows)
			}
		}
	}

	// post the message
	if eventError {
		event.Msg("SQL error")
	} else if eventWarn {
		event.Msg("SQL slow query")
	} else {
		event.Msg("SQL")
	}
}

// timestampCheck is the outcome of checking a logger for TimestampFieldName.
type timestampCheck struct {
	logger *zerolog.Logger
	found  bool
}

// lastTimestampCheck keeps a single outcome: the logger comes from the context of
// each query, and a context can carry a new logger per request, which a cache
// holding every logger seen would never release.
var lastTimestampCheck atomic.Pointer[timestampCheck]

// loggerHasTimestamp reports whether the events of z already carry
// TimestampFieldName, checking again only when z differs from the previous logger.
func loggerHasTimestamp(z *zerolog.Logger) bool {
	if last := lastTimestampCheck.Load(); last != nil && last.logger == z {
		return last.found
	}

	found := emitsField(z, TimestampFieldName)
	lastTimestampCheck.Store(&timestampCheck{logger: z, found: found})

	return found
}

// emitsField reports whether every event of z carries the given field on its
// own, from its context or its hooks. The events are JSON unless zerolog is built
// with the binary_log tag: it then always reports false.
func emitsField(z *zerolog.Logger, field string) bool {
	var buf bytes.Buffer
	probe := z.Output(&buf).Sample(nil)
	probe.Log().Msg("")

	var fields map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &fields); err != nil {
		return false
	}
	_, found := fields[field]

	return found
}
