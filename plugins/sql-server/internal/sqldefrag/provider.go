package sqldefrag

import "gorm.io/gorm"

// DBProvider returns a GORM handle for the live SQL connection. The
// JobRegistry calls it on each Start so reconnects between requests still
// produce a fresh handle. In the plugin, the operation handler closes
// over the per-config-item *gorm.DB it resolved from the host SQL
// connection.
type DBProvider func() (*gorm.DB, error)
