package sqlmigrations

import (
	"fmt"
	"log"
	"regexp"
	"strings"

	"github.com/go-pg/migrations"

	"github.com/EladDolev/aws_audit_exporter/models"
	"github.com/EladDolev/aws_audit_exporter/postgres"
)

var billingTables = []models.BillingTable{
	&models.Instances{},
	&models.InstancesUptime{},
	&models.ReservationsListings{},
	&models.Reservations{},
	&models.ReservationsListingsTerms{},
	&models.ReservationsRelations{},
	&models.SpotPrices{},
}

var enums = models.Enums

func createEnums(db migrations.DB) error {
	for name, values := range enums {
		sqlStatement := fmt.Sprintf("CREATE TYPE %s AS ENUM ('%s');",
			name, strings.Join(values, "', '"))
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
}

func destroyEnums(db migrations.DB) error {
	for name := range enums {
		sqlStatement := fmt.Sprintf("DROP TYPE IF EXISTS %s;", name)
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
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
		constraintName := fmt.Sprintf("check_%s_%s", tabelName, checkName)
		sqlStatement := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s CHECK (%s)",
			tabelName, constraintName, check)
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
}

// createForeignKeys creates foreign key constraints for BillingTable
// acts on a map of source columns tuple to destination table and columns tuple
func createForeignKeys(db migrations.DB, model models.BillingTable) error {
	for sourceColumns, destination := range *model.GetTableForeignKeys() {
		tabelName := model.GetTableName()
		constraintName := fmt.Sprintf("fk_%s_%s", tabelName, regexp.MustCompile(",").ReplaceAllString(sourceColumns, "_"))
		sqlStatement := fmt.Sprintf("ALTER TABLE %s ADD CONSTRAINT %s FOREIGN KEY (%s) REFERENCES %s;",
			tabelName, constraintName, sourceColumns, destination)
		if _, err := db.ExecOne(sqlStatement); err != nil {
			return err
		}
	}
	return nil
}

// RunMigrations if necessary, runs migration on the DB, and/or creates initial schema
func RunMigrations(cmd string) error {

	var oldVersion int64
	var newVersion int64
	var err error

	if cmd == "" {
		oldVersion, newVersion, err = migrations.Run(postgres.DB)
	} else {
		oldVersion, newVersion, err = migrations.Run(postgres.DB, cmd)
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
