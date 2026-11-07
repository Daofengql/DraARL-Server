package observability

import (
	"context"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"gorm.io/gorm"
)

var databaseQueries atomic.Uint64
var databaseQueryNanos atomic.Uint64
var databaseErrors atomic.Uint64

func DatabaseMetrics() (queries, nanos, failures uint64) {
	return databaseQueries.Load(), databaseQueryNanos.Load(), databaseErrors.Load()
}

type databaseOperation struct {
	start  time.Time
	span   trace.Span
	parent context.Context
}

// InstrumentDatabase records timing without SQL text, arguments or credentials.
func InstrumentDatabase(db *gorm.DB) error {
	before := func(tx *gorm.DB) {
		ctx, span := Start(tx.Statement.Context, "db."+tx.Statement.Table, attribute.String("db.system.name", "mysql"), attribute.String("db.collection.name", tx.Statement.Table))
		parent := tx.Statement.Context
		tx.Statement.Context = ctx
		tx.InstanceSet("draarl:db_operation", databaseOperation{time.Now(), span, parent})
	}
	after := func(tx *gorm.DB) {
		stored, ok := tx.InstanceGet("draarl:db_operation")
		if !ok {
			return
		}
		op := stored.(databaseOperation)
		databaseQueries.Add(1)
		databaseQueryNanos.Add(uint64(time.Since(op.start))) // #nosec G115 -- Elapsed monotonic duration is nonnegative.
		if tx.Error != nil && tx.Error != gorm.ErrRecordNotFound {
			databaseErrors.Add(1)
			op.span.SetStatus(codes.Error, "database operation failed")
		}
		op.span.End()
		// Restore the parent so subsequent statements do not parent ended spans.
		tx.Statement.Context = op.parent
	}
	for _, callbacks := range []struct {
		before, after func(string, func(*gorm.DB)) error
	}{
		{func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().Before("gorm:query").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Query().After("gorm:after_query").Register(n, f)
		}},
		{func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().Before("gorm:create").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Create().After("gorm:after_create").Register(n, f)
		}},
		{func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().Before("gorm:update").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Update().After("gorm:after_update").Register(n, f)
		}},
		{func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().Before("gorm:delete").Register(n, f)
		}, func(n string, f func(*gorm.DB)) error {
			return db.Callback().Delete().After("gorm:after_delete").Register(n, f)
		}},
		{func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().Before("gorm:raw").Register(n, f) }, func(n string, f func(*gorm.DB)) error { return db.Callback().Raw().After("gorm:raw").Register(n, f) }},
		{func(n string, f func(*gorm.DB)) error { return db.Callback().Row().Before("gorm:row").Register(n, f) }, func(n string, f func(*gorm.DB)) error { return db.Callback().Row().After("gorm:row").Register(n, f) }},
	} {
		if err := callbacks.before("draarl:before", before); err != nil {
			return err
		}
		if err := callbacks.after("draarl:after", after); err != nil {
			return err
		}
	}
	return nil
}
