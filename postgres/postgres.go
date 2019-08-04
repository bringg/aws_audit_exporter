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

// InsertIntoPGReservationsRelations responsible for updating reservations relations information.
// also sets "converted" and "canceled" statuses, and original expiration (end) date
func InsertIntoPGReservationsRelations(modifications *[]*ec2.ReservedInstancesModification,
	listings *[]*ec2.ReservedInstancesListing, reservedInstances *[]*ec2.ReservedInstances) error {
	// exist silently if database was not initialized or there are no modifications
	if DB == nil || modifications == nil || len(*modifications) == 0 {
		return nil
	}

	var err error
	var relations []models.ReservationsRelations
	// a map that hold "converted" status for reservations
	reservationsConvertedStatus := funk.Map(*reservedInstances, func(r *ec2.ReservedInstances) (uuid.UUID, bool) {
		// not checking for error, since validity was checked already in InsertIntoPGReservations
		reservationUUID, _ := uuid.Parse(*r.ReservedInstancesId)
		return reservationUUID, false
	}).(map[uuid.UUID]bool)
	return DB.RunInTransaction(func(tx *pg.Tx) error {
		// taking care of midifications
		for _, modification := range *modifications {
			if *modification.Status != "fulfilled" {
				continue
			}
			for _, parent := range modification.ReservedInstancesIds {
				parentUUID, _ := uuid.Parse(*parent.ReservedInstancesId)
				// updating parent reservation "converted" status
				reservationsConvertedStatus[parentUUID] = true
				for _, child := range modification.ModificationResults {
					// updating child converted status
					childUUID, _ := uuid.Parse(*child.ReservedInstancesId)
					// updating child reservation "converted" status
					reservationsConvertedStatus[childUUID] = true
					// updating relation
					relation := models.ReservationsRelations{
						ParentID:      parentUUID,
						ReservationID: childUUID,
					}
					relations = append(relations, relation)
				}
			}
		}
		// taking care of reservations that were splitted after some were sold
		var seenListings []uuid.UUID
		for _, listing := range *listings {
			listingUUID, err := uuid.Parse(*listing.ReservedInstancesListingId)
			if err != nil {
				return fmt.Errorf("Failed parsing listing %s UUID: %s",
					*listing.ReservedInstancesListingId, err.Error())
			}
			if funk.Contains(seenListings, listingUUID) {
				continue
			}
			var listedReservations []models.Reservations
			if err = DB.Model(&listedReservations).Where(
				"? = ANY (listed_on)", listing.ReservedInstancesListingId).Order("start_date").Select(); err != nil {
				return fmt.Errorf("Failed fetching reservations for listing %s: %s",
					*listing.ReservedInstancesListingId, err.Error())
			}
			seenListings = append(seenListings, listedReservations[0].ListedOn...)
			for i := 0; i < len(listedReservations)-1; i++ {
				// updating relation
				relation := models.ReservationsRelations{
					ParentID:      listedReservations[i].ReservationID,
					ReservationID: listedReservations[i+1].ReservationID,
				}
				relations = append(relations, relation)
			}
		}
		if err = upsert(&relations, &[]string{"parent_id", "reservation_id"}, &[]string{"updated_at"}); err != nil {
			return fmt.Errorf("Failed updating reservations relations: %s", err.Error())
		}
		// updating reservations "converted" and "canceled" statuses and original expiration (end) date
		var reservations []models.Reservations
		for _, r := range *reservedInstances {
			// not checking for error, since validity was checked already in InsertIntoPGReservations
			reservationUUID, _ := uuid.Parse(*r.ReservedInstancesId)
			reservation := models.Reservations{ReservationID: reservationUUID}
			reservation.UpdatedAt = time.Now()
			reservation.OriginalEndDate, err = getOriginalReservationExpirationDate(r)
			if err != nil {
				return fmt.Errorf("Failed calling getOriginalReservationExpirationDate for %s: %s",
					reservationUUID, err.Error())
			}
			if reservationsConvertedStatus[reservationUUID] {
				reservation.Converted = true
				reservation.Canceled = false
			} else if r.Start.Add(time.Second).Equal(*r.End) {
				// assuming canceled instances always lives for one second (once aws processing finished)
				reservation.Canceled = true
			}
			reservations = append(reservations, reservation)
		}
		_, err = DB.Model(&reservations).Column("canceled").Column("converted").Column(
			"original_end_date").Column("updated_at").WherePK().Update()
		return err
	})
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
		return fmt.Errorf("Failed parsing count: %s", err.Error())
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
	endDate := parseDate((*values)["end_date"])

	reservation := models.Reservations{
		Az:               (*values)["az"],
		Count:            uint16(count),
		Duration:         int32(duration),
		EffectivePrice:   uint64(EP * 1000000000),
		EndDate:          endDate,
		Family:           (*values)["family"],
		InstanceType:     (*values)["instance_type"],
		ListedOn:         listingsUUIDs,
		OfferClass:       (*values)["offer_class"],
		OfferType:        (*values)["offer_type"],
		OriginalEndDate:  endDate,
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

	return upsert(&reservation, &[]string{"reservation_id"},
		&[]string{"end_date", "listed_on", "state", "updated_at"})
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

// getOriginalReservationExpirationDate returns original reservation expiration date
// might not be accurate for historical data, but should be accurate for new one
func getOriginalReservationExpirationDate(r *ec2.ReservedInstances) (time.Time, error) {
	// all members in the dinesty share the same duration
	duration, err := time.ParseDuration(fmt.Sprintf("%ds", *r.Duration))
	if err != nil {
		return time.Time{}, fmt.Errorf("Failed parsing duration: %s", err.Error())
	}
	// if true, always accurate
	if *r.State != "retired" || r.Start.Add(duration).Add(-time.Second).Equal(*r.End) {
		return *r.End, nil
	}

	// not checking for err since it was validated before in InsertIntoPGReservations
	reservationID, _ := uuid.Parse(*r.ReservedInstancesId)
	// look for oldest parent
	oldestParent := models.Reservations{ReservationID: reservationID}
	for i := 0; ; i++ {
		if i > 50 {
			return time.Time{}, fmt.Errorf("Too many iterations for finding oldest parent")
		}
		temp := models.Reservations{}
		err = DB.Model(&temp).Join(
			"JOIN reservations_relations r ON reservations.reservation_id = r.parent_id").Where(
			"r.reservation_id = ?", oldestParent.ReservationID).Order("start_date").Limit(1).Select()
		if err != nil {
			if err.Error() != "pg: no rows in result set" {
				return time.Time{}, fmt.Errorf("Failed fetching oldest parent information: %s", err.Error())
			}
			break
		}
		oldestParent = temp
	}
	if oldestParent.ReservationID == reservationID {
		// no parents, result will be accurate
		return r.Start.Add(duration).Add(-time.Second), nil
	}

	// search all siblings and descendants for latest expiration date
	youngestDescendnt := models.Reservations{ReservationID: reservationID}
	for i := 0; ; i++ {
		if i > 50 {
			return time.Time{}, fmt.Errorf("Too many iterations for finding youngest descendant")
		}
		temp := models.Reservations{}
		err = DB.Model(&temp).Join(
			"JOIN reservations_relations r ON reservations.reservation_id = r.reservation_id").Where(
			"r.parent_id = ?", youngestDescendnt.ReservationID).Order("start_date ASC").Limit(1).Select()
		if err != nil {
			if err.Error() != "pg: no rows in result set" {
				return time.Time{}, fmt.Errorf("Failed fetching youngest descendant: %s", err.Error())
			}
			break
		}
		youngestDescendnt = temp
	}

	// this result might not be accurate, but should not stray in more than an hour
	oldestParentOriginalEndDate := oldestParent.StartDate.Add(duration).Add(-time.Second)
	if youngestDescendnt.OriginalEndDate.After(oldestParentOriginalEndDate) {
		return youngestDescendnt.OriginalEndDate, nil
	}
	return oldestParentOriginalEndDate, nil
}

// InsertIntoPGReservationsListingsSales responsible for updating sales information
// writes to reservations_listings_terms and reservations_sell_events tables
func InsertIntoPGReservationsListingsSales(values *prometheus.Labels, totalUnitsSold uint16,
	priceSchedules []*ec2.PriceSchedule) error {
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
	var listedRI models.Reservations
	if err = DB.Model(&listedRI).Where("reservation_id = ?", listedRIID).Select(); err != nil {
		return fmt.Errorf("Failed fetching listed reservation %s: %s", listedRIID, err.Error())
	}
	listedReservationOriginalExpirationDate := listedRI.OriginalEndDate
	listedReservationStartDate := listedRI.StartDate

	// calculating sell events
	sellEvents := []models.ReservationsSellEvents{}
	sellEvent := models.ReservationsSellEvents{ListingID: listingID}
	var reservations []models.Reservations
	if totalUnitsSold > 0 {
		var reservationsInListing []models.Reservations
		numResults, err := DB.Model(&reservationsInListing).Where("? = ANY (listed_on)", listingID).Where(
			"start_date >= ?", listedReservationStartDate).Order("end_date").SelectAndCount()
		if err != nil {
			return fmt.Errorf("Failed getting reservations that belongs to this listing: %s", err.Error())
		}
		// calculating sold events
		var unitsSold uint16
		switch numResults {
		case 0:
			return fmt.Errorf("Did not find any reservations that belongs to this listing: %s", err.Error())
		default:
			youngestDescendntIndex := numResults - 1
			for i := 0; i < youngestDescendntIndex && unitsSold < totalUnitsSold; i++ {
				sold := reservationsInListing[i].Count - reservationsInListing[i+1].Count
				sellEvent.ReservationID = reservationsInListing[i].ReservationID
				sellEvent.UnitsSold = sold
				sellEvent.SoldDate = reservationsInListing[i+1].StartDate
				sellEvents = append(sellEvents, sellEvent)
				// this is the only place "sell_splitted" lifecycle status is being set
				reservation := models.Reservations{
					ReservationID: reservationsInListing[i].ReservationID,
					SellSplitted:  true,
					UpdatedAt:     time.Now(),
				}
				reservations = append(reservations, reservation)
				unitsSold += sold
			}
			youngestSold := reservationsInListing[youngestDescendntIndex].EndDate.Add(
				time.Second).Before(listedReservationOriginalExpirationDate)
			if youngestSold && unitsSold < totalUnitsSold {
				r := reservationsInListing[youngestDescendntIndex]
				sold := r.Count
				sellEvent.ReservationID = r.ReservationID
				sellEvent.UnitsSold = sold
				sellEvent.SoldDate = r.EndDate
				sellEvents = append(sellEvents, sellEvent)
				// this is the only place "sold" lifecycle status is being set
				reservation := models.Reservations{
					ReservationID: r.ReservationID,
					Sold:          true,
					UpdatedAt:     time.Now(),
				}
				reservations = append(reservations, reservation)
				unitsSold += sold
			}
			if totalUnitsSold != unitsSold {
				return fmt.Errorf("Failed assertion for sell events on listing %s", listingID)
			}
		}
	}

	// TODO: find out how this really works
	const oneMonthDuration = time.Hour * 24 * 365 / 12
	listingPublishDate := parseDate((*values)["created_date"])
	listingTotalTerms := int64(len(priceSchedules))

	return DB.RunInTransaction(func(tx *pg.Tx) error {
		for _, priceSchedule := range priceSchedules {
			hoursTillExpiration := *priceSchedule.Term * 24 * 365 / 12
			duration, _ := time.ParseDuration(fmt.Sprintf("%dh", hoursTillExpiration))
			termEndDate := listedReservationOriginalExpirationDate.Add(-duration)
			termStartDate := termEndDate.Add(-oneMonthDuration)
			if *priceSchedule.Term == listingTotalTerms {
				termStartDate = listingPublishDate
			}

			listingPrices := models.ReservationsListingsTerms{
				ListingID:    listingID,
				StartDate:    termStartDate,
				EndDate:      termEndDate,
				UpfrontPrice: uint64(*priceSchedule.Price * 1000000000),
			}
			if err = upsert(&listingPrices, &[]string{"listing_id", "start_date"},
				&[]string{"updated_at"}); err != nil {
				return err
			}
		}
		if totalUnitsSold > 0 {
			if _, err := DB.Model(&reservations).Column("sell_splitted").Column(
				"sold").Column("updated_at").WherePK().Update(); err != nil {
				return err
			}
			return upsert(&sellEvents, &[]string{"reservation_id"}, &[]string{"updated_at"})
		}
		return nil
	})
} // TODO: make sure everything is UTC
