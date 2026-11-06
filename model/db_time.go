package model

import (
	"github.com/QuantumNous/new-api/common"
	"gorm.io/gorm"
)

// GetDBTimestamp returns a UNIX timestamp from database time.
// Falls back to application time on error.
func GetDBTimestamp() int64 {
	return GetDBTimestampTx(nil)
}

// GetDBTimestampTx reads database time on the given transaction.
//
// Passing the transaction matters rather than being a convenience: the clock
// query needs a connection, and a transaction already holds one. When the pool
// has no spare connection — SQLite in particular runs a single one — reading the
// clock through the shared DB handle from inside an open transaction blocks
// waiting for a connection that the transaction itself will never release. Any
// code path that runs inside DB.Transaction must use this variant.
//
// A nil tx falls back to the shared pool, so read-only callers keep the plain
// GetDBTimestamp behavior.
func GetDBTimestampTx(tx *gorm.DB) int64 {
	target := tx
	if target == nil {
		target = DB
	}
	var ts int64
	var err error
	switch {
	case common.UsingMainDatabase(common.DatabaseTypePostgreSQL):
		err = target.Raw("SELECT EXTRACT(EPOCH FROM NOW())::bigint").Scan(&ts).Error
	case common.UsingMainDatabase(common.DatabaseTypeSQLite):
		err = target.Raw("SELECT strftime('%s','now')").Scan(&ts).Error
	default:
		err = target.Raw("SELECT UNIX_TIMESTAMP()").Scan(&ts).Error
	}
	if err != nil || ts <= 0 {
		return common.GetTimestamp()
	}
	return ts
}
