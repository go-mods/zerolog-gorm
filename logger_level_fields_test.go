package zerologgorm_test

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	zerologgorm "github.com/go-mods/zerolog-gorm"
)

const (
	testSQL  = "SELECT 1"
	testRows = int64(3)
)

// newLogContext returns a context carrying a zerolog logger that accepts every
// level, and the writer collecting what it logs.
func newLogContext() (context.Context, *MockWriter) {
	out := NewMockWriter()
	z := zerolog.New(out).Level(zerolog.TraceLevel)

	return z.WithContext(context.Background()), out
}

// traceQuery calls Trace the way GORM does and returns the entries written, and
// whether GORM's callback was called.
func traceQuery(l logger.Interface, begin time.Time, err error) ([]map[string]interface{}, bool) {
	ctx, out := newLogContext()

	called := false
	l.Trace(ctx, begin, func() (string, int64) {
		called = true
		return testSQL, testRows
	}, err)

	return out.Entries, called
}

// messages returns the message of each entry.
func messages(entries []map[string]interface{}) []string {
	msgs := make([]string, 0, len(entries))
	for _, e := range entries {
		msgs = append(msgs, e[zerolog.MessageFieldName].(string))
	}

	return msgs
}

func Test_LogMode_Trace(t *testing.T) {
	failure := errors.New("failure")

	queries := map[string]struct {
		slow bool
		err  error
	}{
		"failed":    {err: failure},
		"slow":      {slow: true},
		"success":   {},
		"not found": {err: gorm.ErrRecordNotFound},
	}

	all := map[string][]string{
		"failed":    {"SQL error"},
		"slow":      {"SQL slow query"},
		"success":   {"SQL"},
		"not found": {"SQL"},
	}

	tests := []struct {
		name string
		l    logger.Interface
		want map[string][]string
	}{
		{
			name: "level never set",
			l:    &zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true},
			want: all,
		},
		{
			name: "info",
			l:    (&zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true}).LogMode(logger.Info),
			want: all,
		},
		{
			name: "warn",
			l:    (&zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true}).LogMode(logger.Warn),
			want: map[string][]string{"failed": {"SQL error"}, "slow": {"SQL slow query"}},
		},
		{
			name: "error",
			l:    (&zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true}).LogMode(logger.Error),
			want: map[string][]string{"failed": {"SQL error"}},
		},
		{
			name: "silent",
			l:    (&zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true}).LogMode(logger.Silent),
			want: map[string][]string{},
		},
		{
			name: "below silent",
			l:    (&zerologgorm.GormLogger{SlowThreshold: time.Minute, IgnoreRecordNotFoundError: true}).LogMode(logger.LogLevel(0)),
			want: map[string][]string{},
		},
	}

	for _, tt := range tests {
		for name, q := range queries {
			t.Run(tt.name+"/"+name, func(t *testing.T) {
				begin := time.Now()
				if q.slow {
					begin = begin.Add(-time.Hour)
				}

				entries, called := traceQuery(tt.l, begin, q.err)

				want := tt.want[name]
				assert.ElementsMatch(t, want, messages(entries))
				assert.Equal(t, len(want) > 0, called, "the callback builds the SQL: it must run only for what is logged")
			})
		}
	}
}

func Test_LogMode_Messages(t *testing.T) {
	tests := []struct {
		name  string
		level logger.LogLevel
		want  []string
	}{
		{name: "info", level: logger.Info, want: []string{"info", "warn", "error"}},
		{name: "warn", level: logger.Warn, want: []string{"warn", "error"}},
		{name: "error", level: logger.Error, want: []string{"error"}},
		{name: "silent", level: logger.Silent, want: []string{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, out := newLogContext()
			l := (&zerologgorm.GormLogger{}).LogMode(tt.level)

			l.Info(ctx, "info")
			l.Warn(ctx, "warn")
			l.Error(ctx, "error")

			assert.Equal(t, tt.want, messages(out.Entries))
		})
	}

	t.Run("level never set", func(t *testing.T) {
		ctx, out := newLogContext()
		l := &zerologgorm.GormLogger{}

		l.Info(ctx, "info")
		l.Warn(ctx, "warn")
		l.Error(ctx, "error")

		assert.Equal(t, []string{"info", "warn", "error"}, messages(out.Entries))
	})
}

func Test_LogMode_ReturnsCopy(t *testing.T) {
	original := &zerologgorm.GormLogger{
		SlowThreshold:             time.Minute,
		IgnoreRecordNotFoundError: true,
		FieldsOrder:               []string{zerologgorm.SqlFieldName},
		FieldsExclude:             []string{zerologgorm.RowsFieldName},
	}

	copied, ok := original.LogMode(logger.Silent).(*zerologgorm.GormLogger)
	require.True(t, ok)

	assert.NotSame(t, original, copied)
	assert.Equal(t, original.SlowThreshold, copied.SlowThreshold)
	assert.Equal(t, original.IgnoreRecordNotFoundError, copied.IgnoreRecordNotFoundError)
	assert.Equal(t, original.FieldsOrder, copied.FieldsOrder)
	assert.Equal(t, original.FieldsExclude, copied.FieldsExclude)

	entries, _ := traceQuery(original, time.Now(), nil)
	assert.Len(t, entries, 1, "the receiver must keep logging")

	entries, _ = traceQuery(copied, time.Now(), nil)
	assert.Empty(t, entries)

	entries, _ = traceQuery(copied.LogMode(logger.Info), time.Now(), nil)
	assert.Len(t, entries, 1, "a later LogMode replaces the level")
}

// openTestDB opens an in-memory database logging to a writer the test can read.
func openTestDB(t *testing.T, l logger.Interface) (*gorm.DB, *MockWriter) {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: l})
	require.NoError(t, err)

	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { _ = sqlDB.Close() })

	ctx, out := newLogContext()

	return db.WithContext(ctx), out
}

func Test_LogMode_Gorm(t *testing.T) {
	selectOne := func(db *gorm.DB) error {
		var n int

		return db.Raw(testSQL).Scan(&n).Error
	}

	t.Run("Debug overrides the level for its session only", func(t *testing.T) {
		db, out := openTestDB(t, (&zerologgorm.GormLogger{}).LogMode(logger.Silent))

		require.NoError(t, selectOne(db))
		assert.Empty(t, out.Entries)

		require.NoError(t, selectOne(db.Debug()))
		assert.Len(t, out.Entries, 1)

		out.Reset()
		require.NoError(t, selectOne(db))
		assert.Empty(t, out.Entries, "the parent session must stay silent")
	})

	t.Run("Debug on a logger whose level was never set", func(t *testing.T) {
		db, out := openTestDB(t, &zerologgorm.GormLogger{})

		require.NoError(t, selectOne(db))
		assert.Len(t, out.Entries, 1)

		out.Reset()
		require.NoError(t, selectOne(db.Debug()))
		assert.Len(t, out.Entries, 1)
	})

	t.Run("Session logger applies to its session only", func(t *testing.T) {
		base := &zerologgorm.GormLogger{}
		db, out := openTestDB(t, base)

		require.NoError(t, selectOne(db.Session(&gorm.Session{Logger: base.LogMode(logger.Silent)})))
		assert.Empty(t, out.Entries)

		require.NoError(t, selectOne(db))
		assert.Len(t, out.Entries, 1)
	})
}

func Test_Timestamp(t *testing.T) {
	// trace logs one query per logger given and returns the raw lines: decoding
	// them into a map would hide a duplicate key.
	trace := func(l *zerologgorm.GormLogger, z zerolog.Logger, out *bytes.Buffer) string {
		out.Reset()
		l.Trace(z.WithContext(context.Background()), time.Now(), func() (string, int64) {
			return testSQL, testRows
		}, nil)

		return out.String()
	}
	timeKey := `"` + zerolog.TimestampFieldName + `":`

	t.Run("logger with a timestamp gets a single time", func(t *testing.T) {
		var out bytes.Buffer
		z := zerolog.New(&out).Level(zerolog.TraceLevel).With().Timestamp().Logger()

		assert.Equal(t, 1, strings.Count(trace(&zerologgorm.GormLogger{}, z, &out), timeKey))
	})

	t.Run("logger without a timestamp keeps the time of the query", func(t *testing.T) {
		var out bytes.Buffer
		z := zerolog.New(&out).Level(zerolog.TraceLevel)

		assert.Equal(t, 1, strings.Count(trace(&zerologgorm.GormLogger{}, z, &out), timeKey))
	})

	t.Run("each logger is checked on its own", func(t *testing.T) {
		var out bytes.Buffer
		with := zerolog.New(&out).Level(zerolog.TraceLevel).With().Timestamp().Logger()
		without := zerolog.New(&out).Level(zerolog.TraceLevel)
		l := &zerologgorm.GormLogger{}

		for range 2 {
			assert.Equal(t, 1, strings.Count(trace(l, with, &out), timeKey))
			assert.Equal(t, 1, strings.Count(trace(l, without, &out), timeKey))
		}
	})

	t.Run("excluded field stays out of a logger without a timestamp", func(t *testing.T) {
		var out bytes.Buffer
		z := zerolog.New(&out).Level(zerolog.TraceLevel)
		l := &zerologgorm.GormLogger{FieldsExclude: []string{zerologgorm.TimestampFieldName}}

		assert.NotContains(t, trace(l, z, &out), timeKey)
	})

	t.Run("hooks run one extra time per new logger only", func(t *testing.T) {
		var out bytes.Buffer
		calls := 0
		z := zerolog.New(&out).Level(zerolog.TraceLevel).Hook(zerolog.HookFunc(
			func(*zerolog.Event, zerolog.Level, string) { calls++ },
		))
		ctx := z.WithContext(context.Background())
		l := &zerologgorm.GormLogger{}

		for range 3 {
			l.Trace(ctx, time.Now(), func() (string, int64) { return testSQL, testRows }, nil)
		}

		assert.Equal(t, 4, calls, "3 queries and 1 check")
	})
}

func Test_NoLoggerInContext(t *testing.T) {
	failure := errors.New("failure")

	// useGlobalLogger replaces the global logger for the duration of the test.
	useGlobalLogger := func(t *testing.T) *bytes.Buffer {
		t.Helper()

		old := log.Logger
		t.Cleanup(func() { log.Logger = old })

		var out bytes.Buffer
		log.Logger = zerolog.New(&out)

		return &out
	}
	query := func() (string, int64) { return testSQL, testRows }

	t.Run("failed query goes to the global logger", func(t *testing.T) {
		out := useGlobalLogger(t)

		(&zerologgorm.GormLogger{}).Trace(context.Background(), time.Now(), query, failure)

		assert.Contains(t, out.String(), "SQL error")
		assert.Contains(t, out.String(), testSQL)
	})

	t.Run("error message goes to the global logger", func(t *testing.T) {
		out := useGlobalLogger(t)

		(&zerologgorm.GormLogger{}).Error(context.Background(), "failure")

		assert.Contains(t, out.String(), "failure")
	})

	t.Run("successful query stays silent", func(t *testing.T) {
		out := useGlobalLogger(t)

		(&zerologgorm.GormLogger{}).Trace(context.Background(), time.Now(), query, nil)

		assert.Empty(t, out.String())
	})

	t.Run("ignored not found stays silent", func(t *testing.T) {
		out := useGlobalLogger(t)
		l := &zerologgorm.GormLogger{IgnoreRecordNotFoundError: true}

		l.Trace(context.Background(), time.Now(), query, gorm.ErrRecordNotFound)

		assert.Empty(t, out.String())
	})

	t.Run("silent level stays silent", func(t *testing.T) {
		out := useGlobalLogger(t)
		l := (&zerologgorm.GormLogger{}).LogMode(logger.Silent)

		l.Trace(context.Background(), time.Now(), query, failure)
		l.Error(context.Background(), "failure")

		assert.Empty(t, out.String())
	})

	t.Run("logger disabled in the context stays silent", func(t *testing.T) {
		out := useGlobalLogger(t)
		ctx, _ := newLogContext()
		ctx = zerolog.Nop().WithContext(ctx)

		(&zerologgorm.GormLogger{}).Trace(ctx, time.Now(), query, failure)

		assert.Empty(t, out.String())
	})
}

func Test_Trace_SkipsWorkWhenNothingIsWritten(t *testing.T) {
	out := NewMockWriter()
	ctx := zerolog.New(out).Level(zerolog.InfoLevel).WithContext(context.Background())

	called := false
	(&zerologgorm.GormLogger{}).Trace(ctx, time.Now(), func() (string, int64) {
		called = true
		return testSQL, testRows
	}, nil)

	assert.False(t, called)
	assert.Empty(t, out.Entries)
}

func Test_FieldNames(t *testing.T) {
	// setFieldNames changes the package-level names for the duration of the test.
	setFieldNames := func(t *testing.T, file, sql, rows string) {
		t.Helper()

		oldFile, oldSQL, oldRows := zerologgorm.FileFieldName, zerologgorm.SqlFieldName, zerologgorm.RowsFieldName
		t.Cleanup(func() {
			zerologgorm.FileFieldName, zerologgorm.SqlFieldName, zerologgorm.RowsFieldName = oldFile, oldSQL, oldRows
		})

		zerologgorm.FileFieldName, zerologgorm.SqlFieldName, zerologgorm.RowsFieldName = file, sql, rows
	}

	t.Run("default names keep the historical keys", func(t *testing.T) {
		entries, _ := traceQuery(zerologgorm.NewGormLogger(), time.Now(), nil)

		require.Len(t, entries, 1)
		assert.Contains(t, entries[0], "file")
		assert.Equal(t, testSQL, entries[0]["sql"])
		assert.EqualValues(t, testRows, entries[0]["rows"])
	})

	t.Run("custom names replace the keys", func(t *testing.T) {
		setFieldNames(t, "caller", "query", "affected")

		for name, l := range map[string]logger.Interface{
			"NewGormLogger": zerologgorm.NewGormLogger(),
			"zero value":    &zerologgorm.GormLogger{},
		} {
			t.Run(name, func(t *testing.T) {
				entries, _ := traceQuery(l, time.Now(), nil)

				require.Len(t, entries, 1)
				assert.Contains(t, entries[0], "caller")
				assert.Equal(t, testSQL, entries[0]["query"])
				assert.EqualValues(t, testRows, entries[0]["affected"])
				assert.NotContains(t, entries[0], "file")
				assert.NotContains(t, entries[0], "sql")
				assert.NotContains(t, entries[0], "rows")
			})
		}
	})

	t.Run("names set after NewGormLogger still apply", func(t *testing.T) {
		l := zerologgorm.NewGormLogger()
		setFieldNames(t, "caller", "query", "affected")

		entries, _ := traceQuery(l, time.Now(), nil)

		require.Len(t, entries, 1)
		assert.Contains(t, entries[0], "caller")
		assert.Equal(t, testSQL, entries[0]["query"])
		assert.EqualValues(t, testRows, entries[0]["affected"])
	})

	t.Run("custom names work with FieldsExclude", func(t *testing.T) {
		setFieldNames(t, "caller", "query", "affected")

		l := &zerologgorm.GormLogger{FieldsExclude: []string{zerologgorm.FileFieldName, zerologgorm.RowsFieldName}}
		entries, _ := traceQuery(l, time.Now(), nil)

		require.Len(t, entries, 1)
		assert.Equal(t, testSQL, entries[0]["query"])
		assert.NotContains(t, entries[0], "caller")
		assert.NotContains(t, entries[0], "affected")
	})
}
