package sqlmigrations

import (
	"fmt"
	"strings"

	migrations "github.com/go-pg/migrations/v7"
	funk "github.com/thoas/go-funk"

	"github.com/EladDolev/aws_audit_exporter/debug"
	"github.com/EladDolev/aws_audit_exporter/models"
)

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
