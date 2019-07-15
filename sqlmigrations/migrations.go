package sqlmigrations

import (
	"fmt"
	"log"

	"github.com/EladDolev/aws_audit_exporter/models"
	migrations "github.com/go-pg/migrations/v7"
)

var billingTables = []models.BillingTable{
	&models.InstancesTable{},
	&models.InstancesUptimeTable{},
	&models.ReservationsTable{},
	&models.SpotPricesTable{},
}

// createIndexes creates indexes for BillingTable
// acts on a map of index suffix to command suffix
// index prefix: "idx_%tableName%_"
// command prefix: "CREATE INDEX %indexName% ON %tableName% "
func createIndexes(db migrations.DB, model models.BillingTable) error {
	for iSuffix, cSuffix := range *model.GetTableIndexes() {
		sqlStatement := fmt.Sprintf("CREATE INDEX idx_%s_%s ON %s %s",
			model.GetTableName(), iSuffix, model.GetTableName(), cSuffix)
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
}

// createChecks creates check constraints for BillingTable
// acts on a map of check name to check command
func createChecks(db migrations.DB, model models.BillingTable) error {
	for checkName, check := range *model.GetTableChecks() {
		tabelName := model.GetTableName()
		constraintName := fmt.Sprintf("%s_%s_check", tabelName, checkName)
		sqlStatement := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)",
			tabelName, constraintName, check)
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
}

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
