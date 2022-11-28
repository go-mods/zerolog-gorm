package zerologgorm_test

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/go-mods/zerolog-gorm"
	"github.com/rs/zerolog"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"io"
	"os"
	"testing"
	"time"
)

type MockWriter struct {
	Entries []map[string]interface{}
}

func NewMockWriter() *MockWriter {
	return &MockWriter{make([]map[string]interface{}, 0)}
}

func (m *MockWriter) Write(p []byte) (int, error) {
	entry := map[string]interface{}{}

	if err := json.Unmarshal(p, &entry); err != nil {
		panic(fmt.Sprintf("Failed to parse JSON %v: %s", p, err.Error()))
	}

	m.Entries = append(m.Entries, entry)

	return len(p), nil
}

func (m *MockWriter) Reset() {
	m.Entries = make([]map[string]interface{}, 0)
}

func Test_Logger_Sqlite(t *testing.T) {

	var mockOut = NewMockWriter()

	var consoleOut io.Writer = zerolog.ConsoleWriter{
		Out:        os.Stdout,
		TimeFormat: time.RFC3339Nano,
	}

	logOut := zerolog.MultiLevelWriter(mockOut, consoleOut)

	logger := zerolog.New(logOut).With().Timestamp().Logger()

	logger.Info().Msg("Start")

	now := time.Now()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NowFunc: func() time.Time { return now },
		Logger: &zerologgorm.GormLogger{
			FieldsExclude: []string{zerologgorm.DurationFieldName, zerologgorm.FileFieldName},
		},
	})

	if err != nil {
		panic("failed to connect the database")
	}

	db = db.WithContext(logger.WithContext(context.Background()))

	type Post struct {
		Title, Body string
		CreatedAt   time.Time
	}
	_ = db.AutoMigrate(&Post{})

	cases := []struct {
		run   func() error
		sql   string
		errOk bool
	}{
		{
			run: func() error { return db.Create(&Post{Title: "awesome"}).Error },
			sql: fmt.Sprintf(
				"INSERT INTO `posts` (`title`,`body`,`created_at`) VALUES (%q,%q,%q)",
				"awesome", "", now.Format("2006-01-02 15:04:05.000"),
			),
			errOk: false,
		},
		{
			run:   func() error { return db.Model(&Post{}).Find(&[]*Post{}).Error },
			sql:   "SELECT * FROM `posts`",
			errOk: false,
		},
		{
			run: func() error {
				return db.Where(&Post{Title: "awesome", Body: "This is awesome post !"}).First(&Post{}).Error
			},
			sql: fmt.Sprintf(
				"SELECT * FROM `posts` WHERE `posts`.`title` = %q AND `posts`.`body` = %q ORDER BY `posts`.`title` LIMIT 1",
				"awesome", "This is awesome post !",
			),
			errOk: true,
		},
		{
			run:   func() error { return db.Raw("THIS is,not REAL sql").Scan(&Post{}).Error },
			sql:   "THIS is,not REAL sql",
			errOk: true,
		},
	}

	for _, c := range cases {
		mockOut.Reset()

		err := c.run()

		if err != nil && !c.errOk {
			t.Fatalf("Unexpected error: %s (%T)", err, err)
		}

		// TODO: Must get from log entries
		entries := mockOut.Entries

		if got, want := len(entries), 1; got != want {
			t.Errorf("GormLogger logged %d items, want %d items", got, want)
		} else {
			fieldByName := entries[0]

			if got, want := fieldByName["sql"].(string), c.sql; got != want {
				t.Errorf("Logged sql was %q, want %q", got, want)
			}
		}
	}

	logger.Info().Msg("End")

}
