package postgres

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/go-pg/pg"
	"github.com/google/uuid"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/thoas/go-funk"

	"github.com/EladDolev/aws_audit_exporter/debug"
	"github.com/EladDolev/aws_audit_exporter/models"
)

// DB global variable for postgres connection
var DB *pg.DB

type dbLogger struct{}

func (d dbLogger) BeforeQuery(q *pg.QueryEvent) {
	//func (d dbLogger) BeforeQuery(c context.Context, q *pg.QueryEvent) (context.Context, error) {
	debug.Println(q.FormattedQuery())
	//return c, nil
}

func (d dbLogger) AfterQuery(q *pg.QueryEvent) {}

//func (d dbLogger) AfterQuery(c context.Context, q *pg.QueryEvent) (context.Context, error) {
//	return c, nil
//}

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
func upsert(model interface{}, onConflictTuple *[]string, columnsToUpdate *[]string) error {

	onConflict := fmt.Sprintf("(%s)", strings.Join(*onConflictTuple, ",")) + " DO UPDATE"

	var setStatement string
	for _, column := range *columnsToUpdate {
		setStatement += fmt.Sprintf("%s = EXCLUDED.%s, ", column, column)
	}
	setStatement = setStatement[:len(setStatement)-2]

	_, err := DB.Model(model).OnConflict(onConflict).Set(setStatement).Insert()
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

	instance := models.Instances{
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

	instanceUpTime := models.InstancesUptime{
		InstanceID: (*values)["instance_id"],
		LaunchTime: parseDate((*values)["launch_time"]),
		State:      (*values)["state"],
	}

	return DB.RunInTransaction(func(tx *pg.Tx) error {
		if err := upsert(&([]models.Instances{instance}), &[]string{"instance_id"},
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

	spot := models.SpotPrices{
		Az:               (*values)["az"],
		Family:           (*values)["family"],
		InstanceType:     (*values)["instance_type"],
		Product:          (*values)["product"],
		RecurringCharges: uint64(RC * 1000000000),
		Units:            parseUnits((*values)["units"]),
	}
	_, err := DB.Model(&spot).Insert()
	return err
}

func updateReservationLifecycleStatus(r *models.Reservations, lifecycle string) {
	switch lifecycle {
	case "canceled":
		r.Lifecycle = []string{"canceled"}
	case "unchanged":
		r.Lifecycle = []string{"unchanged"}
	case "unknown":
		r.Lifecycle = []string{"unknown"}
	// making sure "unchanged" and "unknown" are not present anymore, and the new status exists only once
	default:
		r.Lifecycle = append(funk.Filter(r.Lifecycle, func(s string) bool {
			return !(s == "unchanged" || s == "unknown" || s == "canceled" || s == lifecycle)
		}).([]string), lifecycle)
	}
}

func findDirectDescendantsAndUpdateLifecycle(r *models.Reservations) (*[]models.Reservations, error) {
	duration, err := time.ParseDuration(fmt.Sprintf("%ds", r.Duration))
	if err != nil {
		return nil, fmt.Errorf("Failed parsing duration: %s", err.Error())
	}
	if r.StartDate.Add(duration).Add(-time.Second).Equal(r.EndDate) {
		updateReservationLifecycleStatus(r, "unchanged")
		return nil, nil
	}

	queryBase := fmt.Sprintf(`
		SELECT * FROM reservations
		WHERE duration = %d
		AND offer_class = '%s'
		AND region = '%s'
		AND reservation_id != '%s'`,
		r.Duration, r.OfferClass, r.Region, r.ReservationID,
	)
	if r.OfferClass != "convertible" {
		queryBase += fmt.Sprintf(`
		AND family = '%s'
		AND offer_type = '%s'
		AND product = '%s'
		AND tenancy = '%s'`,
			r.Family, r.OfferType, r.Product, r.Tenancy,
		)
	}
	// TODO: consider changing everything to ascendants, so the condition for state='retired' can be added
	var directDescendants []models.Reservations
	var foundDirectDescendantsSells bool
	var foundDirectDescendantsConversion bool
	// looking for descendants originated from a sell operation
	queryDecendants := queryBase + fmt.Sprintf(`
		AND upfront_price = %d
		AND start_date = '%v'`,
		r.UpfrontPrice, r.EndDate.Add(time.Second).Format("2006-01-02 15:04:05"),
	)
	ormResult, err := DB.Query(&directDescendants, queryDecendants)
	if err != nil {
		return nil, fmt.Errorf("Failed running query for finding decendants originated from a sell operation: %s",
			err.Error(),
		)
	}
	foundDirectDescendantsSells = ormResult.RowsReturned() > 0

	if !foundDirectDescendantsSells {
		// looking for descendants originated from a converion/exchange operation
		queryDecendants = queryBase + fmt.Sprintf(`
		AND start_date = '%v'`, r.EndDate.Format("2006-01-02 15:04:05"),
		)
		ormResult, err = DB.Query(&directDescendants, queryDecendants)
		if err != nil {
			return nil, fmt.Errorf(
				"Failed running query for finding decendants originated from a converion/exchange operation: %s",
				err.Error(),
			)
		}
		foundDirectDescendantsConversion = ormResult.RowsReturned() > 0
	}
	// descendants can originate either from a sell operation or conversion, not both
	if foundDirectDescendantsSells || foundDirectDescendantsConversion {
		newLifeCycle := "sell_splitted"
		if foundDirectDescendantsConversion {
			newLifeCycle = "converted"
		}
		// updating original reservation lifecycle status
		updateReservationLifecycleStatus(r, newLifeCycle)
		// updating direct descendants lifecycle status
		for i := 0; i < len(directDescendants); i++ {
			updateReservationLifecycleStatus(&directDescendants[i], newLifeCycle)
		}
		return &directDescendants, nil
	}

	// assuming canceled instances always lives for one second (once aws processing finished)
	if r.StartDate.Add(time.Second).Equal(r.EndDate) {
		updateReservationLifecycleStatus(r, "canceled")
		return nil, nil
	}

	// a changed reservation without descendats gets it's lifecycle updated from it's ascendants
	var currentLifecycle []string
	err = DB.Model(r).Column("lifecycle").Where("reservation_id = ?", r.ReservationID).Select(pg.Array(&currentLifecycle))
	if err != nil {
		if err.Error() == "pg: no rows in result set" {
			// getting here means some information is still missing in the tables. will be converged eventually
			updateReservationLifecycleStatus(r, "unknown")
			return nil, nil
		}
		return nil, fmt.Errorf(
			"Failed fetching reservation %s from reservations table: %s", r.ReservationID, err.Error())
	}
	r.Lifecycle = funk.FlattenDeep(currentLifecycle).([]string)
	return nil, nil
}

// InsertIntoPGReservations responsible for updating reservations information
func InsertIntoPGReservations(values *prometheus.Labels, RC float64, FP float64, EP float64,
	listings *[]*ec2.ReservedInstancesListing) error {
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
	reservationID, err := uuid.Parse((*values)["ri_id"])
	if err != nil {
		return fmt.Errorf("Failed parsing reservationID: %v", err)
	}
	var listingsUUIDs []uuid.UUID
	for _, listing := range *listings {
		listingUUID, err := uuid.Parse(*listing.ReservedInstancesListingId)
		if err != nil {
			return fmt.Errorf("Failed parsing reservation listing uuid %s: %s",
				*listing.ReservedInstancesListingId, err.Error())
		}
		listingsUUIDs = append(listingsUUIDs, listingUUID)
	}

	reservation := models.Reservations{
		Az:               (*values)["az"],
		Count:            uint16(count),
		Duration:         int32(duration),
		EffectivePrice:   uint64(EP * 1000000000),
		EndDate:          parseDate((*values)["end_date"]),
		Family:           (*values)["family"],
		InstanceType:     (*values)["instance_type"],
		ListedOn:         listingsUUIDs,
		OfferClass:       (*values)["offer_class"],
		OfferType:        (*values)["offer_type"],
		Product:          (*values)["product"],
		RecurringCharges: uint64(RC * 1000000000),
		Region:           (*values)["region"],
		ReservationID:    reservationID,
		Scope:            (*values)["scope"],
		StartDate:        parseDate((*values)["start_date"]),
		State:            (*values)["state"],
		Tenancy:          (*values)["tenancy"],
		Units:            parseUnits((*values)["units"]),
		UpfrontPrice:     uint64(FP * 1000000000),
	}

	var directDescendants *[]models.Reservations
	directDescendants, err = findDirectDescendantsAndUpdateLifecycle(&reservation)
	if err != nil {
		return fmt.Errorf("Failed calling findDirectDescendantsAndUpdateLifecycle: %s", err.Error())
	}

	if err = upsert(&reservation, &[]string{"reservation_id"}, &[]string{
		"state", "updated_at", "lifecycle"}); err != nil {
		return fmt.Errorf("Failed upserting new reservation %s: %s", reservationID, err.Error())
	}
	// nil means no direct descendants were found
	if directDescendants == nil {
		return nil
	}

	directDescendantsUUID := funk.Map(*directDescendants, func(d models.Reservations) uuid.UUID {
		return d.ReservationID
	}).([]uuid.UUID)

	debug.Printf("Found direct descendants %v with status '%s' for reservation %s\n",
		directDescendantsUUID, (*directDescendants)[0].Lifecycle, reservationID)

	// updating direct descendants lifecycle
	if err = upsert(directDescendants, &[]string{"reservation_id"},
		&[]string{"lifecycle", "updated_at"}); err != nil {
		return fmt.Errorf("Failed upserting direct descendants for %s: %s", reservationID, err.Error())
	}

	// now for updating family relations
	relations := funk.Map(directDescendantsUUID,
		func(descendant uuid.UUID) models.ReservationsRelations {
			return models.ReservationsRelations{
				ParentID:      reservationID,
				ReservationID: descendant,
			}
		}).([]models.ReservationsRelations)

	// looking for rows where direct descendats are the parents
	var farDescendants []models.ReservationsRelations
	var farDescendantsUUID []uuid.UUID
	if err = DB.Model(&farDescendants).Where("parent_id in (?)", pg.In(directDescendantsUUID)).Column(
		"reservation_id").Select(&farDescendantsUUID); err != nil {
		return fmt.Errorf("Failed quering for far descendants: %s", err.Error())
	}
	// if more distant descendants were found
	if len(farDescendantsUUID) > 0 {
		relations = append(relations, funk.Map(farDescendantsUUID,
			func(descendant uuid.UUID) models.ReservationsRelations {
				return models.ReservationsRelations{
					ParentID:      reservationID,
					ReservationID: descendant,
				}
			}).([]models.ReservationsRelations)...)
	}
	relations = funk.Uniq(relations).([]models.ReservationsRelations)

	if err = upsert(&relations, &[]string{
		"parent_id", "reservation_id"}, &[]string{"updated_at"}); err != nil {
		return fmt.Errorf("Failed updating ReservationsRelationsTable: %s", err.Error())
	}

	return nil
}

// InsertIntoPGReservationsListings responsible for updating reservations listings table
func InsertIntoPGReservationsListings(values *prometheus.Labels, count uint16) error {
	// exist silently if database was not initialized
	if DB == nil {
		return nil
	}

	listingID, err := uuid.Parse((*values)["ril_id"])
	if err != nil {
		return fmt.Errorf("Failed parsing reservationListingID: %v", err)
	}

	reservationListing := models.ReservationsListings{
		Az:            (*values)["az"],
		Count:         count,
		Family:        (*values)["family"],
		InstanceType:  (*values)["instance_type"],
		Product:       (*values)["product"],
		PublishedDate: parseDate((*values)["created_date"]),
		Region:        (*values)["region"],
		ListingID:     listingID,
		Scope:         (*values)["scope"],
		State:         (*values)["state"],
		Status:        (*values)["status"],
		StatusMessage: (*values)["status_message"],
		Units:         parseUnits((*values)["units"]),
	}

	return upsert(&reservationListing, &[]string{"listing_id", "state"},
		&[]string{"count", "status", "status_message", "updated_at"})
}

// TODO: should be optimized
func getOriginalReservationExpirationDate(r uuid.UUID) (time.Time, error) {
	// first find the oldest ancestor
	var startDate time.Time
	var durationSeconds int32
	err := DB.Model(new(models.Reservations)).ColumnExpr("start_date").ColumnExpr("duration").Join(
		"JOIN reservations_relations relation ON reservations.reservation_id = relation.parent_id").Where(
		"relation.reservation_id = ?", r).Order("start_date").Limit(1).Select(&startDate, &durationSeconds)
	if err != nil {
		// couldn't find any ancestors. will get the original reservation parameters
		if err.Error() == "pg: no rows in result set" {
			err = DB.Model(new(models.Reservations)).Column("start_date").Column("duration").Where(
				"reservation_id = ?", r).Select(&startDate, &durationSeconds)
			if err != nil {
				return time.Time{}, fmt.Errorf("Failed fetching original reservation parameters: %s", err.Error())
			}
		} else {
			return time.Time{}, fmt.Errorf("Failed looking for ancestors: %s", err.Error())
		}
	}
	// all members in the dinesty share the same duration
	duration, err := time.ParseDuration(fmt.Sprintf("%ds", durationSeconds))
	if err != nil {
		return time.Time{}, fmt.Errorf("Failed parsing duration: %s", err.Error())
	}
	// now find the youngest descendant
	var endDate time.Time
	err = DB.Model(new(models.Reservations)).ColumnExpr("end_date").Join(
		"JOIN reservations_relations relation ON reservations.reservation_id = relation.reservation_id").Where(
		"relation.parent_id = ?", r).Order("end_date ASC").Limit(1).Select(&endDate)
	if err != nil {
		// couldn't find any descendants. will get the original reservation parameters
		if err.Error() == "pg: no rows in result set" {
			err = DB.Model(new(models.Reservations)).Column("end_date").Where(
				"reservation_id = ?", r).Select(&endDate)
			if err != nil {
				return time.Time{}, fmt.Errorf("Failed fetching original reservation end date: %s", err.Error())
			}
		} else {
			return time.Time{}, fmt.Errorf("Failed looking for descendants: %s", err.Error())
		}
	}
	originalExpirationDate := startDate.Add(duration).Add(-time.Second)
	if !originalExpirationDate.Equal(endDate) {
		debug.Printf("reservation %s expired before end date. either sold, or information missing in the tables", r)
	}
	return originalExpirationDate, nil
}

// InsertIntoPGReservationsListingsTerms responsible for updating reservations listings prices table
func InsertIntoPGReservationsListingsTerms(values *prometheus.Labels, unitsSold uint16, priceSchedules []*ec2.PriceSchedule) error {
	// exist silently if database was not initialized
	if DB == nil {
		return nil
	}

	listingID, err := uuid.Parse((*values)["ril_id"])
	if err != nil {
		return fmt.Errorf("Failed parsing listingID: %v", err)
	}
	listedRIID, err := uuid.Parse((*values)["source_ri_id"])
	if err != nil {
		return fmt.Errorf("Failed parsing ListedRIID: %v", err)
	}
	listingPublishDate := parseDate((*values)["created_date"])
	listingTotalTerms := int64(len(priceSchedules))
	const oneMonthDuration = time.Hour * 24 * 30

	reservationOriginalExpirationDate, err := getOriginalReservationExpirationDate(listedRIID)
	if err != nil {
		return fmt.Errorf("Failed calling getOriginalReservationExpirationDate: %s", err.Error())
	}

	var listedRI models.Reservations
	if err = DB.Model(&listedRI).Where("reservation_id = ?", listedRIID).Select(); err != nil {
		return fmt.Errorf("Failed fetching listed reservation %s: %s", listedRIID, err.Error())
	}

	// calculating sell events
	sellEvents := map[time.Time]uint16{}
	if unitsSold > 0 {
		var reservationsInListing []models.Reservations
		numResults, err := DB.Model(&reservationsInListing).Where(
			"? = ANY (listed_on)", listingID).Order("end_date").SelectAndCount()
		if err != nil {
			return fmt.Errorf("Failed getting reservations that belongs to this listing: %s", err.Error())
		}
		// calculating sold events
		var unitsSoldAssertion uint16
		switch numResults {
		case 0:
			return fmt.Errorf("Did not find any reservations that belongs to this listing: %s", err.Error())
		case 1:
			r := reservationsInListing[0]
			if r.ReservationID != listedRIID {
				return fmt.Errorf("Failed assertion on single sold listedRIID: %s", err.Error())
			}
			sold := r.Count
			sellEvents[r.EndDate] = sold
			unitsSoldAssertion = sold
			updateReservationLifecycleStatus(&r, "sold")
			// TODO: should be changed to update
			if err = upsert(&r, &[]string{"reservation_id"}, &[]string{"updated_at", "lifecycle"}); err != nil {
				return fmt.Errorf("Failed updating sold lifecycle for %s: %s", listedRIID, err.Error())
			}
		default:
			youngestDescendntIndex := numResults - 1
			for i := 0; i < youngestDescendntIndex; i++ {
				sold := reservationsInListing[i].Count - reservationsInListing[i+1].Count
				sellEvents[reservationsInListing[i].EndDate] = sold
				unitsSoldAssertion += sold
			}
			youngestSold := reservationsInListing[youngestDescendntIndex].EndDate.Add(time.Second).Before(reservationOriginalExpirationDate)
			if youngestSold && youngestDescendntIndex > 0 {
				r := reservationsInListing[youngestDescendntIndex]
				sold := r.Count
				sellEvents[r.EndDate] = sold
				unitsSoldAssertion += sold
				updateReservationLifecycleStatus(&r, "sold")
				// TODO: should be changed to update
				if err = upsert(&r, &[]string{"reservation_id"}, &[]string{"updated_at", "lifecycle"}); err != nil {
					return fmt.Errorf("Failed updating sold lifecycle for %s: %s", listedRIID, err.Error())
				}
			}
		}

		if unitsSoldAssertion != unitsSold {
			debug.Println("Failed assertion for sell events on listing:", listingID)
			return nil
		}
	}

	// TODO: check rolling back actually works
	return DB.RunInTransaction(func(tx *pg.Tx) error {
		for _, priceSchedule := range priceSchedules {
			hoursTillExpiration := *priceSchedule.Term * 24 * 30
			duration, _ := time.ParseDuration(fmt.Sprintf("%dh", hoursTillExpiration))
			termEndDate := reservationOriginalExpirationDate.Add(-duration)
			termStartDate := termEndDate.Add(-oneMonthDuration)
			if *priceSchedule.Term == listingTotalTerms {
				termStartDate = listingPublishDate
			}

			var unitsSold uint16
			for event, count := range sellEvents {
				if termStartDate.Before(event) && termEndDate.After(event) {
					unitsSold += count
				}
			}

			listingPrices := models.ReservationsListingsTerms{
				ListingID:    listingID,
				StartDate:    termStartDate,
				EndDate:      termEndDate,
				UnitsSold:    unitsSold,
				UpfrontPrice: uint64(*priceSchedule.Price * 1000000000),
			}
			if err = upsert(&listingPrices, &[]string{"listing_id", "start_date"},
				&[]string{"units_sold", "updated_at"}); err != nil {
				debug.Printf(
					"failed updating terms for listing %s. probably because of missing information in the tables: %s\n",
					listingID, err.Error())
				return nil
			}
		}
		return nil
	})
} // TODO: make sure everything is UTC
