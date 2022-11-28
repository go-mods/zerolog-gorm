# gorm-zerolog
[![license](https://img.shields.io/github/license/go-mods/zerolog-gorm)](./LICENSE)

Zerolog logger for gorm

```go
package main

import (
    "context"
    "github.com/go-mods/zerolog-gorm"
    "gorm.io/driver/sqlite"
    "gorm.io/gorm"
    "time"
)

func main() {
    logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

    db, err := gorm.Open(sqlite.Open("test.db"), &gorm.Config{
        NowFunc: func() time.Time { return time.Now() },
        Logger: &zerologgorm.GormLogger{
            FieldsExclude: []string{zerologgorm.DurationFieldName, zerologgorm.FileFieldName},
        },
    })

    if err != nil {
        panic("failed to connect the database")
    }

    db = db.WithContext(logger.WithContext(context.Background()))
}
```
