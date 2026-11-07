package repository

import (
	"context"
	"errors"
	"time"

	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// A scheduler lease scan and a terminal status update can deadlock on InnoDB
// status indexes. Retry the entire rolled-back transaction, including its lease
// check and recording insert, so retries cannot duplicate communication records.
func (r *Repository) finishRunTransaction(ctx context.Context, fn func(*gorm.DB) error) error {
	const attempts = 3
	for attempt := 0; ; attempt++ {
		err := r.db.WithContext(ctx).Transaction(fn)
		var mysqlError *drivermysql.MySQLError
		if err == nil || attempt == attempts-1 || !errors.As(err, &mysqlError) || (mysqlError.Number != 1213 && mysqlError.Number != 1205) {
			return err
		}
		timer := time.NewTimer((10 * time.Millisecond) << attempt)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
	}
}
