package sqlmigrations

import (
	"log"

	migrations "github.com/go-pg/migrations/v7"
)

// RunMigrations if necessary, runs migration on the DB, and/or creates initial schema
func RunMigrations(db migrations.DB, cmd string) error {

	var oldVersion int64
	var newVersion int64
	var err error

	if cmd == "" {
		oldVersion, newVersion, err = migrations.Run(db)
	} else {
		oldVersion, newVersion, err = migrations.Run(db, cmd)
	}

	if err != nil {
		return err
	}

	if newVersion != oldVersion {
		log.Printf("migrated schema from version %d to %d\n", oldVersion, newVersion)
	} else {
		log.Println("schema version is", oldVersion)
	}

	return nil
}
