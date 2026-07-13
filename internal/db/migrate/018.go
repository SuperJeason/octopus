package migrate

import (
	"github.com/bestruirui/octopus/internal/model"
	"gorm.io/gorm"
)

func init() {
	RegisterAfterAutoMigration(Migration{
		Version: 18,
		Up:      migrateChannelHealthTables,
	})
}

func migrateChannelHealthTables(db *gorm.DB) error {
	return db.AutoMigrate(
		&model.ChannelHealthSnapshot{},
		&model.ChannelHealthAttempt{},
	)
}
