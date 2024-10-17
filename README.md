# zerolog-gorm

[![Go Reference](https://pkg.go.dev/badge/github.com/go-mods/zerolog-gorm.svg)](https://pkg.go.dev/github.com/go-mods/zerolog-gorm)
[![Go Report Card](https://goreportcard.com/badge/github.com/go-mods/zerolog-gorm)](https://goreportcard.com/report/github.com/go-mods/zerolog-gorm)
[![Release](https://img.shields.io/github/release/go-mods/zerolog-gorm.svg?style=flat)](https://github.com/go-mods/zerolog-gorm/releases)
[![License](https://img.shields.io/badge/License-MIT-blue.svg)](https://github.com/go-mods/zerolog-gorm/blob/master/LICENSE)

`zerolog-gorm` is a logger middleware for GORM that integrates the `zerolog` logger, providing a fast and efficient logging solution for your database operations. Designed to be lightweight and performant, `zerolog-gorm` captures and structures logs optimally, making debugging and performance analysis easier. With its compatibility with the advanced features of `zerolog`, it allows for customizing logs according to the specific needs of your application while maintaining a minimal memory footprint.

## Installation

To install the package, use the following command:

```bash
go get github.com/go-mods/zerolog-gorm
```

## Usage

Here is an example of how to use `zerolog-gorm` with a GORM application:

```go
package main

import (
	"context"
	"github.com/glebarez/sqlite"
	zerologgorm "github.com/go-mods/zerolog-gorm"
	"github.com/rs/zerolog"
	"gorm.io/gorm"
	"os"
	"time"
)

func main() {
	// Configure the logger
	logger := zerolog.New(zerolog.ConsoleWriter{Out: os.Stdout}).With().Timestamp().Logger()

	// Initialize GORM with a SQLite in-memory database
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{
		NowFunc: func() time.Time { return time.Now() },
		Logger: &zerologgorm.GormLogger{
			FieldsExclude: []string{zerologgorm.DurationFieldName, zerologgorm.FileFieldName},
		},
	})

	if err != nil {
		logger.Fatal().Err(err).Msg("Failed to initialize GORM")
	}

	// Set the logger context for the database
	db = db.WithContext(logger.WithContext(context.Background()))
}
```

## License

This project is licensed under the MIT License - see the [LICENSE.md](LICENSE.md) file for details.
