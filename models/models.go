package models

import (
	"time"
)

// BillingTable represents a table in the aws_audit_exporter RDBMS
type BillingTable interface {
	GetTableName() string
	GetTableIndexes() *map[string]string
	GetTableChecks() *map[string]string
}

// -------------------------------------------------------------
// ---------------------- instances table ----------------------
// -------------------------------------------------------------

var instancesIndexes = map[string]string{
	"az":            "(az)",
	"family":        "(family)",
	"instance_type": "(instance_type)",
	"lifecycle":     "(lifecycle)",
	"state":         "(state)",
	"tags":          "USING HASH (tags)",
}

var instancesChecks = map[string]string{
	"state": `state IN ('pending', 'running', 'shutting-down', 'rebooting',
			  'terminated', 'stopping', 'stopped')`,
	"times": `launch_time >= '2006-08-25'
			  AND created_at >= launch_time
			  AND updated_at >= created_at`,
	"lifecycle":         `lifecycle IN ('spot', 'normal')`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

// InstancesTable hold information about ec2 instances
type InstancesTable struct {
	TableName    struct{}  `sql:"instances"`
	InstanceID   string    `sql:"type:varchar(25),pk"`
	Az           string    `sql:"type:varchar(15),notnull"`
	CreatedAt    time.Time `sql:"default:now(),notnull"`
	Family       string    `sql:"type:varchar(4),notnull"`
	InstanceType string    `sql:"type:varchar(13),notnull"`
	LaunchTime   time.Time `sql:",notnull"`
	Lifecycle    string    `sql:"type:varchar(9),notnull"`
	OwnerID      uint64    `sql:",notnull"`
	RequesterID  uint64    `sql:",notnull"`
	State        string    `sql:"type:varchar(13),notnull"`
	Units        float32   `sql:",notnull"`
	UpdatedAt    time.Time `sql:"default:now(),notnull"`
	Groups       string
	Tags         map[string]string `sql:",hstore"`
}

// GetTableName returns table name
func (i *InstancesTable) GetTableName() string {
	return "instances"
}

// GetTableIndexes returns table indexes
func (i *InstancesTable) GetTableIndexes() *map[string]string {
	return &instancesIndexes
}

// GetTableChecks returns table check constraints
func (i *InstancesTable) GetTableChecks() *map[string]string {
	return &instancesChecks
}

// ------------------------------------------------------------
// ------------------ instances_uptime table ------------------
// ------------------------------------------------------------

var instancesUptimeIndexes = map[string]string{
	"updated_at": "(updated_at)",
}

var instancesUptimeChecks = map[string]string{
	"state": `state IN ('pending', 'running', 'shutting-down', 'rebooting',
			  'terminated', 'stopping', 'stopped')`,
	"times": `launch_time >= '2006-08-25'
			  AND created_at >= launch_time
			  AND updated_at >= created_at`,
}

// InstancesUptimeTable holds information about instance state changes over time
type InstancesUptimeTable struct {
	TableName  struct{}  `sql:"instances_uptime"`
	ID         int64     `sql:",pk"`
	CreatedAt  time.Time `sql:"default:now(),notnull"`
	InstanceID string    `sql:"type:varchar(25),notnull,unique:instance_id_launch_time_state"`
	LaunchTime time.Time `sql:",notnull,unique:instance_id_launch_time_state"`
	State      string    `sql:"type:varchar(13),notnull,unique:instance_id_launch_time_state"`
	UpdatedAt  time.Time `sql:"default:now(),notnull"`
}

// GetTableName returns table name
func (i *InstancesUptimeTable) GetTableName() string {
	return "instances_uptime"
}

// GetTableIndexes returns table indexes
func (i *InstancesUptimeTable) GetTableIndexes() *map[string]string {
	return &instancesUptimeIndexes
}

// GetTableChecks returns table check constraints
func (i *InstancesUptimeTable) GetTableChecks() *map[string]string {
	return &instancesUptimeChecks
}

// ------------------------------------------------------------
// -------------------- reservations table --------------------
// ------------------------------------------------------------

var reservationsIndexes = map[string]string{
	"end_date":   "(end_date)",
	"family":     "(family)",
	"region":     "(region)",
	"start_date": "(start_date)",
}

var reservationsChecks = map[string]string{
	"az": `az != NULL
	          OR scope = 'Region'`,
	"dates": `start_date >= '2009-03-12'
			  AND end_date <= start_date + interval '3 years'
			  AND end_date >= start_date
			  AND updated_at >= created_at
			  AND created_at >= start_date
			  AND duration <= 94608000`,
	"recurring_charges": `recurring_charges > 0
						  OR offer_type = 'All Upfront'`,
	"offer_class":       `offer_class IN ('standard', 'convertible', 'scheduled')`,
	"offer_type":        `offer_type IN ('Partial Upfront', 'All Upfront', 'No Upfront')`,
	"scope":             `scope IN ('Region', 'Availability Zone')`,
	"state":             `state IN ('active', 'retired', 'payment failed', 'payment pending')`,
	"tenancy":           `tenancy IN ('default', 'dedicated')`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

// ReservationsTable holds information for reserved instances
type ReservationsTable struct {
	TableName        struct{}  `sql:"reservations"`
	ID               int32     `sql:",pk"`
	Az               string    `sql:"type:varchar(15)"`
	Count            int16     `sql:",notnull"`
	CreatedAt        time.Time `sql:"default:now(),notnull"`
	Duration         int32     `sql:",notnull"`
	EffectivePrice   uint64    `sql:"default:,notnull"`
	EndDate          time.Time `sql:",notnull"`
	Family           string    `sql:"type:varchar(4),notnull"`
	InstanceType     string    `sql:"type:varchar(13),notnull"`
	OfferClass       string    `sql:"type:varchar(11),notnull"`
	OfferType        string    `sql:"type:varchar(15),notnull"`
	Product          string    `sql:"type:varchar(37),notnull"`
	RecurringCharges uint64    `sql:",notnull"`
	Region           string    `sql:"type:varchar(14),notnull"`
	ReservationID    string    `sql:"type:varchar(40),notnull,unique"`
	Scope            string    `sql:"type:varchar(17),notnull"`
	StartDate        time.Time `sql:",notnull"`
	State            string    `sql:"type:varchar(15),notnull"`
	Tenancy          string    `sql:"type:varchar(9),notnull"`
	Units            float32   `sql:",notnull"`
	UpdatedAt        time.Time `sql:"default:now(),notnull"`
	UpfrontPrice     uint64    `sql:",notnull"`
}

// GetTableName returns table name
func (r *ReservationsTable) GetTableName() string {
	return "reservations"
}

// GetTableIndexes returns table indexes
func (r *ReservationsTable) GetTableIndexes() *map[string]string {
	return &reservationsIndexes
}

// GetTableChecks returns table check constraints
func (r *ReservationsTable) GetTableChecks() *map[string]string {
	return &reservationsChecks
}

// -------------------------------------------------------------
// --------------------- spot_prices table ---------------------
// -------------------------------------------------------------

var spotPricesIndexes = map[string]string{
	"az":            "(az)",
	"instance_type": "(instance_type)",
}

var spotPricesChecks = map[string]string{
	"product": `product IN ('Linux/UNIX', 'Linux/UNIX (Amazon VPC)', 'Windows',
				'Windows (Amazon VPC)', 'SUSE Linux', 'SUSE Linux (Amazon VPC)',
				'Red Hat Enterprise Linux', 'Red Hat Enterprise Linux (Amazon VPC)')`,
	"times":             `date_trunc('second', updated_at) = date_trunc('second', created_at)`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

// SpotPricesTable holds historical spots prices
type SpotPricesTable struct {
	TableName        struct{}  `sql:"spot_prices"`
	ID               int64     `sql:",pk"`
	Az               string    `sql:"type:varchar(15),notnull,unique:current_price"`
	CreatedAt        time.Time `sql:"default:now(),notnull,unique:current_price"`
	Family           string    `sql:"type:varchar(4),notnull"`
	InstanceType     string    `sql:"type:varchar(13),notnull,unique:current_price"`
	Product          string    `sql:"type:varchar(37),notnull,unique:current_price"`
	RecurringCharges uint64    `sql:",notnull"`
	UpdatedAt        time.Time `sql:"default:now(),notnull"`
	Units            float32   `sql:",notnull"`
}

// GetTableName returns table name
func (s *SpotPricesTable) GetTableName() string {
	return "spot_prices"
}

// GetTableIndexes returns table indexes
func (s *SpotPricesTable) GetTableIndexes() *map[string]string {
	return &spotPricesIndexes
}

// GetTableChecks returns table check constraints
func (s *SpotPricesTable) GetTableChecks() *map[string]string {
	return &spotPricesChecks
}
