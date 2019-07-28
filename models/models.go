package models

import (
	"time"

	"github.com/google/uuid"
)

// BillingTable represents a table in the aws_audit_exporter RDBMS
type BillingTable interface {
	GetTableName() string
	GetTableIndexes() *map[string]string
	GetTableChecks() *map[string]string
	GetTableForeignKeys() *map[string]string
}

// Enums a map for enums used in database
// used in createEnums(migrations.DB) and destroyEnums(migrations.DB) functions
// TODO: use enums in go
var Enums = map[string][]string{
	"instance_lifecycle":         []string{"normal", "spot"},
	"instance_state":             []string{"pending", "running", "shutting-down", "rebooting", "terminated", "stopping", "stopped"},
	"reservation_listing_state":  []string{"available", "cancelled", "pending", "sold"},
	"reservation_listing_status": []string{"active", "cancelled", "closed", "pending"},
	"reservation_offer_class":    []string{"convertible", "scheduled", "standard"},
	"reservation_offer_type":     []string{"All Upfront", "No Upfront", "Partial Upfront"},
	"reservation_scope":          []string{"Availability Zone", "Region"},
	"reservation_state":          []string{"active", "payment-failed", "payment-pending", "retired"},
	"reservation_tenancy":        []string{"dedicated", "default"},
	"spot_product": []string{"Linux/UNIX", "Linux/UNIX (Amazon VPC)", "Windows",
		"Windows (Amazon VPC)", "SUSE Linux", "SUSE Linux (Amazon VPC)",
		"Red Hat Enterprise Linux", "Red Hat Enterprise Linux (Amazon VPC)"},
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
	"times": `launch_time >= '2006-08-25'
			  AND created_at >= launch_time
			  AND updated_at >= created_at`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

var instancesForeignKeys = map[string]string{}

// Instances hold information about ec2 instances
type Instances struct {
	InstanceID   string    `sql:"type:varchar(25),pk"`
	Az           string    `sql:"type:varchar(15),notnull"`
	CreatedAt    time.Time `sql:"default:now(),notnull"`
	Family       string    `sql:"type:varchar(4),notnull"`
	InstanceType string    `sql:"type:varchar(13),notnull"`
	LaunchTime   time.Time `sql:",notnull"`
	Lifecycle    string    `sql:"type:instance_lifecycle,notnull"`
	OwnerID      uint64    `sql:",notnull"`
	RequesterID  uint64    `sql:",notnull"`
	State        string    `sql:"type:instance_state,notnull"`
	Units        float32   `sql:",notnull"`
	UpdatedAt    time.Time `sql:"default:now(),notnull"`
	Groups       string
	Tags         map[string]string `sql:",hstore"`
}

// GetTableName returns table name
func (i *Instances) GetTableName() string {
	return "instances"
}

// GetTableIndexes returns table indexes
func (i *Instances) GetTableIndexes() *map[string]string {
	return &instancesIndexes
}

// GetTableChecks returns table check constraints
func (i *Instances) GetTableChecks() *map[string]string {
	return &instancesChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (i *Instances) GetTableForeignKeys() *map[string]string {
	return &instancesForeignKeys
}

// ------------------------------------------------------------
// ------------------ instances_uptime table ------------------
// ------------------------------------------------------------

var instancesUptimeIndexes = map[string]string{
	"launch_time": "(launch_time)",
	"updated_at":  "(updated_at)",
}

var instancesUptimeChecks = map[string]string{
	"times": `launch_time >= '2006-08-25'
			  AND created_at >= launch_time
			  AND updated_at >= created_at`,
}

var instancesUptimeForeignKeys = map[string]string{
	"instance_id": "instances(instance_id) ON DELETE RESTRICT",
}

// InstancesUptime holds information about instance state changes over time
type InstancesUptime struct {
	InstanceID string    `sql:"type:varchar(25),pk"`
	LaunchTime time.Time `sql:",notnull,pk"`
	State      string    `sql:"type:instance_state,pk"`
	TableName  struct{}  `sql:"instances_uptime"`
	CreatedAt  time.Time `sql:"default:now(),notnull"`
	UpdatedAt  time.Time `sql:"default:now(),notnull"`
}

// GetTableName returns table name
func (i *InstancesUptime) GetTableName() string {
	return "instances_uptime"
}

// GetTableIndexes returns table indexes
func (i *InstancesUptime) GetTableIndexes() *map[string]string {
	return &instancesUptimeIndexes
}

// GetTableChecks returns table check constraints
func (i *InstancesUptime) GetTableChecks() *map[string]string {
	return &instancesUptimeChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (i *InstancesUptime) GetTableForeignKeys() *map[string]string {
	return &instancesUptimeForeignKeys
}

// ------------------------------------------------------------
// -------------------- reservations table --------------------
// ------------------------------------------------------------

var reservationsIndexes = map[string]string{
	"az":         "(az)",
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
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

var reservationsForeignKeys = map[string]string{}

// Reservations holds information for reserved instances
type Reservations struct {
	ReservationID    uuid.UUID   `sql:"type:uuid,pk"`
	Az               string      `sql:"type:varchar(15)"`
	Canceled         bool        `sql:"default:false,notnull"`
	Converted        bool        `sql:"default:false,notnull"`
	Count            uint16      `sql:",notnull"`
	CreatedAt        time.Time   `sql:"default:now(),notnull"`
	Duration         int32       `sql:",notnull"`
	EffectivePrice   uint64      `sql:",notnull"`
	EndDate          time.Time   `sql:",notnull"`
	Family           string      `sql:"type:varchar(4),notnull"`
	InstanceType     string      `sql:"type:varchar(13),notnull"`
	ListedOn         []uuid.UUID `sql:"type:uuid[],array"`
	OfferClass       string      `sql:"type:reservation_offer_class,notnull"`
	OfferType        string      `sql:"type:reservation_offer_type,notnull"`
	OriginalEndDate  time.Time   `sql:",notnull"`
	Product          string      `sql:"type:varchar(37),notnull"`
	RecurringCharges uint64      `sql:",notnull"`
	Region           string      `sql:"type:varchar(14),notnull"`
	Scope            string      `sql:"type:reservation_scope,notnull"`
	SellSplitted     bool        `sql:"default:false,notnull"`
	Sold             bool        `sql:"default:false,notnull"`
	StartDate        time.Time   `sql:",notnull"`
	State            string      `sql:"type:reservation_state,notnull"`
	Tenancy          string      `sql:"type:reservation_tenancy,notnull"`
	Units            float32     `sql:",notnull"`
	UpdatedAt        time.Time   `sql:"default:now(),notnull"`
	UpfrontPrice     uint64      `sql:",notnull"`
}

// GetTableName returns table name
func (r *Reservations) GetTableName() string {
	return "reservations"
}

// GetTableIndexes returns table indexes
func (r *Reservations) GetTableIndexes() *map[string]string {
	return &reservationsIndexes
}

// GetTableChecks returns table check constraints
func (r *Reservations) GetTableChecks() *map[string]string {
	return &reservationsChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (r *Reservations) GetTableForeignKeys() *map[string]string {
	return &reservationsForeignKeys
}

// ------------------------------------------------------------
// --------------- reservations relations table ---------------
// ------------------------------------------------------------

var reservationsRelationsIndexes = map[string]string{}

var reservationselationsChecks = map[string]string{}

var reservationsRelationsForeignKeys = map[string]string{
	"reservation_id": "reservations(reservation_id) ON DELETE RESTRICT",
	"parent_id":      "reservations(reservation_id) ON DELETE RESTRICT",
}

// ReservationsRelations hold relations between reservations
type ReservationsRelations struct {
	ParentID      uuid.UUID `sql:"type:uuid,pk"`
	ReservationID uuid.UUID `sql:"type:uuid,pk"`
	CreatedAt     time.Time `sql:"default:now(),notnull"`
	UpdatedAt     time.Time `sql:"default:now(),notnull"`
}

// GetTableName returns table name
func (r *ReservationsRelations) GetTableName() string {
	return "reservations_relations"
}

// GetTableIndexes returns table indexes
func (r *ReservationsRelations) GetTableIndexes() *map[string]string {
	return &reservationsRelationsIndexes
}

// GetTableChecks returns table check constraints
func (r *ReservationsRelations) GetTableChecks() *map[string]string {
	return &reservationselationsChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (r *ReservationsRelations) GetTableForeignKeys() *map[string]string {
	return &reservationsRelationsForeignKeys
}

// -------------------------------------------------------------
// ---------------- reservations_listings table ----------------
// -------------------------------------------------------------

var reservationsListingIndexes = map[string]string{
	"az":             "(az)",
	"published_date": "(published_date)",
	"family":         "(family)",
	"region":         "(region)",
	"state":          "(state)",
	"status":         "(status)",
}

var reservationsListingsChecks = map[string]string{
	"dates": `published_date >= '2009-03-12'
			  AND updated_at >= created_at
			  AND created_at >= published_date`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

var reservationsListingsForeignKeys = map[string]string{}

// ReservationsListings holds historical and current reservations listings in the AWS marketplace
type ReservationsListings struct {
	ListingID     uuid.UUID `sql:"type:uuid,pk"`
	State         string    `sql:"type:reservation_listing_state,pk"`
	Az            string    `sql:"type:varchar(15)"`
	Count         uint16    `sql:",notnull"`
	CreatedAt     time.Time `sql:"default:now(),notnull"`
	Family        string    `sql:"type:varchar(4),notnull"`
	InstanceType  string    `sql:"type:varchar(13),notnull"`
	Product       string    `sql:"type:varchar(37),notnull"`
	PublishedDate time.Time `sql:",notnull"`
	Region        string    `sql:"type:varchar(14),notnull"`
	Scope         string    `sql:"type:reservation_scope,notnull"`
	Status        string    `sql:"type:reservation_listing_status,notnull"`
	StatusMessage string
	Units         float32   `sql:",notnull"`
	UpdatedAt     time.Time `sql:"default:now(),notnull"`
}

// GetTableName returns table name
func (r *ReservationsListings) GetTableName() string {
	return "reservations_listings"
}

// GetTableIndexes returns table indexes
func (r *ReservationsListings) GetTableIndexes() *map[string]string {
	return &reservationsListingIndexes
}

// GetTableChecks returns table check constraints
func (r *ReservationsListings) GetTableChecks() *map[string]string {
	return &reservationsListingsChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (r *ReservationsListings) GetTableForeignKeys() *map[string]string {
	return &reservationsListingsForeignKeys
}

// -------------------------------------------------------------
// ------------- reservations_listings_terms table -------------
// -------------------------------------------------------------

var reservationsListingTermsIndexes = map[string]string{
	"end_date": "(end_date)",
}

var reservationsListingsTermsChecks = map[string]string{
	"dates": `start_date >= '2012-09-12'
			  AND updated_at >= created_at
			  AND end_date > start_date
			  AND end_date <= now() + interval '3 years'`,
	"term_length": "end_date <= start_date + interval '30 days'",
}

var reservationsListingTermsForeignKeys = map[string]string{}

// ReservationsListingsTerms holds listing terms history
type ReservationsListingsTerms struct {
	ListingID    uuid.UUID `sql:"type:uuid,pk"`
	StartDate    time.Time `sql:",pk"`
	CreatedAt    time.Time `sql:"default:now(),notnull"`
	EndDate      time.Time `sql:",notnull"`
	UpdatedAt    time.Time `sql:"default:now(),notnull"`
	UpfrontPrice uint64    `sql:",notnull"`
}

// GetTableName returns table name
func (r *ReservationsListingsTerms) GetTableName() string {
	return "reservations_listings_terms"
}

// GetTableIndexes returns table indexes
func (r *ReservationsListingsTerms) GetTableIndexes() *map[string]string {
	return &reservationsListingTermsIndexes
}

// GetTableChecks returns table check constraints
func (r *ReservationsListingsTerms) GetTableChecks() *map[string]string {
	return &reservationsListingsTermsChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (r *ReservationsListingsTerms) GetTableForeignKeys() *map[string]string {
	return &reservationsListingTermsForeignKeys
}

// -------------------------------------------------------------
// --------------- reservations_sell_events table --------------
// -------------------------------------------------------------

var reservationsSellEventsIndexes = map[string]string{
	"listing_id": "(listing_id)",
	"sold_date":  "(sold_date)",
}

var reservationsSellEventsChecks = map[string]string{
	"dates": `sold_date >= '2012-09-12'
			  AND created_at > sold_date
			  AND updated_at >= created_at`,
	"units_sold": "units_sold > 0",
}

var reservationsSellEventsForeignKeys = map[string]string{
	"reservation_id": "reservations(reservation_id) ON DELETE RESTRICT",
}

// ReservationsSellEvents holds dates and numbers of sold RIs
type ReservationsSellEvents struct {
	ReservationID uuid.UUID `sql:"type:uuid,pk"`
	CreatedAt     time.Time `sql:"default:now(),notnull"`
	ListingID     uuid.UUID `sql:"type:uuid"`
	SoldDate      time.Time `sql:",notnull"`
	UnitsSold     uint16    `sql:",notnull"`
	UpdatedAt     time.Time `sql:"default:now(),notnull"`
}

// GetTableName returns table name
func (r *ReservationsSellEvents) GetTableName() string {
	return "reservations_sell_events"
}

// GetTableIndexes returns table indexes
func (r *ReservationsSellEvents) GetTableIndexes() *map[string]string {
	return &reservationsSellEventsIndexes
}

// GetTableChecks returns table check constraints
func (r *ReservationsSellEvents) GetTableChecks() *map[string]string {
	return &reservationsSellEventsChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (r *ReservationsSellEvents) GetTableForeignKeys() *map[string]string {
	return &reservationsSellEventsForeignKeys
}

// -------------------------------------------------------------
// --------------------- spot_prices table ---------------------
// -------------------------------------------------------------

var spotPricesIndexes = map[string]string{
	"az":            "(az)",
	"instance_type": "(instance_type)",
}

var spotPricesChecks = map[string]string{
	"times":             `date_trunc('second', updated_at) = date_trunc('second', created_at)`,
	"type_match_family": `substring(instance_type from '(.+)\..+') = family`,
}

var spotPricesForeignKeys = map[string]string{}

// SpotPrices holds historical spots prices
type SpotPrices struct {
	Az               string    `sql:"type:varchar(15),pk"`
	CreatedAt        time.Time `sql:"default:now(),pk"`
	InstanceType     string    `sql:"type:varchar(13),pk"`
	Product          string    `sql:"type:spot_product,pk"`
	TableName        struct{}  `sql:"spot_prices"`
	Family           string    `sql:"type:varchar(4),notnull"`
	RecurringCharges uint64    `sql:",notnull"`
	UpdatedAt        time.Time `sql:"default:now(),notnull"`
	Units            float32   `sql:",notnull"`
}

// GetTableName returns table name
func (s *SpotPrices) GetTableName() string {
	return "spot_prices"
}

// GetTableIndexes returns table indexes
func (s *SpotPrices) GetTableIndexes() *map[string]string {
	return &spotPricesIndexes
}

// GetTableChecks returns table check constraints
func (s *SpotPrices) GetTableChecks() *map[string]string {
	return &spotPricesChecks
}

// GetTableForeignKeys returns table foreign keys constraints
func (s *SpotPrices) GetTableForeignKeys() *map[string]string {
	return &spotPricesForeignKeys
}
