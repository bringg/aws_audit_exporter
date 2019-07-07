package postgres

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	pg "github.com/go-pg/pg/v9"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EladDolev/aws_audit_exporter/debug"
	"github.com/EladDolev/aws_audit_exporter/models"
)

// DB global variable for postgres connection
var DB *pg.DB

type dbLogger struct{}

func (d dbLogger) BeforeQuery(c context.Context, q *pg.QueryEvent) (context.Context, error) {
	debug.Println(q.FormattedQuery())
	return c, nil
}

func (d dbLogger) AfterQuery(c context.Context, q *pg.QueryEvent) (context.Context, error) {
	return c, nil
}

// ConnectPostgres initialize connection to postgresql server, and runs migrations
func ConnectPostgres(dbURL string) error {
	var pgOptions *pg.Options
	var err error

	if pgOptions, err = pg.ParseURL(dbURL); err != nil {
		return fmt.Errorf("Failed parsing postgres parameters: %v", err)
	}
	if DB = pg.Connect(pgOptions); DB == nil {
		return fmt.Errorf("Failed to open postgres connection")
	}

	DB.AddQueryHook(dbLogger{})
	return nil
}

// upsert takes a model, and performs simple upsert
func upsert(model models.BillingTable, onConflictTuple *[]string, columnsToUpdate *[]string) error {

	onConflict := fmt.Sprintf("(%s)", strings.Join(*onConflictTuple, ",")) + " DO UPDATE"

	var set string
	for _, column := range *columnsToUpdate {
		set += fmt.Sprintf("%s = EXCLUDED.%s, ", column, column)
	}
	set = set[:len(set)-2]

	_, err := DB.Model(model).OnConflict(onConflict).Set(set).Insert()
	return err
}

// parseUnits receives units as a string, and returns them as floa32
// returns -1 on error
func parseUnits(unitsString string) float32 {
	units, err := strconv.ParseFloat(unitsString, 32)
	if err != nil {
		return -1
	}
	return float32(units)
}

// parseDate receives a date as a string, and returns it in valid time format
// returns zero time instant on error (January 1, year 1, 00:00:00 UTC)
func parseDate(dateString string) time.Time {
	date, err := time.Parse("2006-01-02 15:04:05", dateString)
	if err != nil {
		return time.Time{}
	}
	return date
}

// InsertIntoPGInstances responsible for updating instances information
func InsertIntoPGInstances(values *prometheus.Labels, tags map[string]string) error {
	// exist silently if database was not initialized
	if DB == nil {
		return nil
	}

	ownerID, err := strconv.ParseInt((*values)["owner_id"], 10, 64)
	if err != nil {
		return fmt.Errorf("Failed parsing ownerID: %v", err)
	}
	requesterID, err := strconv.ParseInt((*values)["requester_id"], 10, 64)
	if err != nil {
		return fmt.Errorf("Failed parsing requesterID: %v", err)
	}

	instance := models.InstancesTable{
		InstanceID:   (*values)["instance_id"],
		Az:           (*values)["az"],
		Family:       (*values)["family"],
		Groups:       (*values)["groups"],
		InstanceType: (*values)["instance_type"],
		LaunchTime:   parseDate((*values)["launch_time"]),
		Lifecycle:    (*values)["lifecycle"],
		OwnerID:      uint64(ownerID),
		RequesterID:  uint64(requesterID),
		Tags:         tags,
		Units:        parseUnits((*values)["units"]),
		State:        (*values)["state"],
	}

	instanceUpTime := models.InstancesUptimeTable{
		InstanceID: (*values)["instance_id"],
		LaunchTime: parseDate((*values)["launch_time"]),
		State:      (*values)["state"],
	}

	return DB.RunInTransaction(func(tx *pg.Tx) error {
		if err := upsert(&instance, &[]string{"instance_id"},
			&[]string{"az", "family", "groups", "instance_type",
				"tags", "units", "state", "updated_at"}); err != nil {
			return err
		}

		return upsert(&instanceUpTime, &[]string{"instance_id",
			"launch_time", "state"}, &[]string{"updated_at"})
	})
}

// InsertIntoPGSpotPrices responsible for updating spots price information
func InsertIntoPGSpotPrices(values *prometheus.Labels, RC float64) error {
	// exist silently if database was not initialized
	if DB == nil {
		return nil
	}

	spot := models.SpotPricesTable{
		Az:               (*values)["az"],
		Family:           (*values)["family"],
		InstanceType:     (*values)["instance_type"],
		Product:          (*values)["product"],
		RecurringCharges: uint64(RC * 100000000),
		Units:            parseUnits((*values)["units"]),
	}
	_, err := DB.Model(&spot).Insert()
	return err
}

// InsertIntoPGReservations responsible for updating reservations information
func InsertIntoPGReservations(values *prometheus.Labels, RC float64, FP float64, EP float64) error {
	// exist silently if database was not initialized
	if DB == nil {
		return nil
	}

	count, err := strconv.ParseInt((*values)["count"], 10, 16)
	if err != nil {
		return fmt.Errorf("Failed parsing count: %v", err)
	}

	duration, err := strconv.ParseInt((*values)["duration"], 10, 32)
	if err != nil {
		return fmt.Errorf("Failed parsing duration: %v", err)
	}

	reservation := models.ReservationsTable{
		Az:               (*values)["az"],
		Count:            int16(count),
		Duration:         int32(duration),
		EffectivePrice:   uint64(EP * 100000000),
		EndDate:          parseDate((*values)["end_date"]),
		Family:           (*values)["family"],
		InstanceType:     (*values)["instance_type"],
		OfferClass:       (*values)["offer_class"],
		OfferType:        (*values)["offer_type"],
		Product:          (*values)["product"],
		RecurringCharges: uint64(RC * 100000000),
		Region:           (*values)["region"],
		ReservationID:    (*values)["id"],
		Scope:            (*values)["scope"],
		StartDate:        parseDate((*values)["start_date"]),
		State:            (*values)["state"],
		Tenancy:          (*values)["tenancy"],
		Units:            parseUnits((*values)["units"]),
		UpfrontPrice:     uint64(FP * 100000000),
	}

	return upsert(&reservation, &[]string{"reservation_id"}, &[]string{"state"})
}
