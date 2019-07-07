package sqlmigrations

import (
	"fmt"
	"strings"

	migrations "github.com/go-pg/migrations/v7"
	funk "github.com/thoas/go-funk"

	"github.com/EladDolev/aws_audit_exporter/debug"
	"github.com/EladDolev/aws_audit_exporter/models"
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

func init() {
	migrations.MustRegisterTx(func(db migrations.DB) error {
		debug.Println("creating DB schema")
		for _, model := range billingTables {
			debug.Println("creating table", model.GetTableName())
			if err := db.Model(model).CreateTable(nil); err != nil {
				return fmt.Errorf("Failed creating table %s: %v", model.GetTableName(), err)
			}
			if err := createIndexes(db, model); err != nil {
				return fmt.Errorf("Failed creating indexes for table %s: %v",
					model.GetTableName(), err)
			}
			if err := createChecks(db, model); err != nil {
				return fmt.Errorf("Failed creating check constraints for table %s: %v",
					model.GetTableName(), err)
			}
		}
		return nil

	}, func(db migrations.DB) error {
		debug.Println("dropping all tables")
		tables := funk.Map(billingTables, func(model models.BillingTable) string {
			return model.GetTableName()
		}).([]string)
		sqlStatement := "DROP TABLE " + strings.Join(tables, ",") + " CASCADE"
		_, err := db.ExecOne(sqlStatement)
		return err
	})
}
