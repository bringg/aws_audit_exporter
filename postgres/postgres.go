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
// also updates reservation lifecycle, and original expiration (end)) date
func InsertIntoPGReservationsRelations(modifications *[]*ec2.ReservedInstancesModification) error {
	// exist silently if database was not initialized or there are no modifications
	if DB == nil || modifications == nil || len(*modifications) == 0 {
		return nil
	}

	var relations []models.ReservationsRelations
	var reservationsUUID []uuid.UUID
	return DB.RunInTransaction(func(tx *pg.Tx) error {
		// taking care of midifications
		for _, modification := range *modifications {
			if *modification.Status != "fulfilled" {
				continue
			}
			for _, parent := range modification.ReservedInstancesIds {
				parentUUID, err := uuid.Parse(*parent.ReservedInstancesId)
				if err != nil {
					return fmt.Errorf("Failed parsing parentUUID: %s", err.Error())
				}
				if len(modification.ReservedInstancesIds) == 12 {
					fmt.Println(parentUUID)
				}
				// saving parent reservation uuid
				reservationsUUID = append(reservationsUUID, parentUUID)
				for _, child := range modification.ModificationResults {
					// updating child converted status
					childUUID, err := uuid.Parse(*child.ReservedInstancesId)
					if err != nil {
						return fmt.Errorf("Failed parsing childUUID: %s", err.Error())
					}
					if len(modification.ReservedInstancesIds) == 12 {
						fmt.Println(childUUID)
					}
					// saving child reservation uuid
					reservationsUUID = append(reservationsUUID, childUUID)
					// updating relation
					relation := models.ReservationsRelations{
						ParentID:      parentUUID,
						ReservationID: childUUID,
					}
					relations = append(relations, relation)
				}
			}
		}
		reservationsUUID = funk.Uniq(reservationsUUID).([]uuid.UUID)
		reservations := funk.Map(reservationsUUID, func(u uuid.UUID) models.Reservations {
			return models.Reservations{
				ReservationID: u,
				Canceled:      false,
				Converted:     true,
			}
		}).([]models.Reservations)

		if _, err := DB.Model(&reservations).Column("converted").WherePK().Update(); err != nil {
			return err
		}
		// TODO: taking care of reservations that were splitted after some were sold

		return upsert(&relations, &[]string{"parent_id", "reservation_id"}, &[]string{"updated_at"})
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

	// assuming canceled instances always lives for one second (once aws processing finished)
	// will be overridden if instance was converted
	if reservation.StartDate.Add(time.Second).Equal(reservation.EndDate) {
		reservation.Canceled = true
	}

	reservation.OriginalEndDate, err = getOriginalReservationExpirationDate(&reservation)
	if err != nil {
		return fmt.Errorf("Failed calling getOriginalReservationExpirationDate: %s", err.Error())
	}

	return upsert(&reservation, &[]string{"reservation_id"}, &[]string{
		"state", "updated_at", "original_end_date"})
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
func getOriginalReservationExpirationDate(r *models.Reservations) (time.Time, error) {
	// all members in the dinesty share the same duration
	duration, err := time.ParseDuration(fmt.Sprintf("%ds", r.Duration))
	if err != nil {
		return time.Time{}, fmt.Errorf("Failed parsing duration: %s", err.Error())
	}
	// if true, always accurate
	if r.State != "retired" || r.StartDate.Add(duration).Add(-time.Second).Equal(r.EndDate) {
		return r.EndDate, nil
	}
	// if not the first time seeing this reservation, update with info saved in database
	err = DB.Model(r).WherePK().Select()
	if err != nil {
		// first time. will wait for next iteration to get more accurate information
		if err.Error() == "pg: no rows in result set" {
			return r.EndDate, nil
		}
		return time.Time{}, fmt.Errorf("Failed fetching reservation info from database: %s",
			err.Error())
	}

	// look for oldest parent
	oldestParent := r
	for {
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
		oldestParent = &temp
	}
	if oldestParent == r {
		// no parents, result will be accurate
		return r.StartDate.Add(duration).Add(-time.Second), nil
	}

	// search all siblings and descendants for latest expiration date
	youngestDescendnt := r
	for {
		temp := models.Reservations{}
		err = DB.Model(&temp).Join(
			"JOIN reservations_relations r ON reservations.reservation_id = r.reservation_id").Where(
			"r.parent_id = ?", youngestDescendnt.ReservationID).Order("original_end_date ASC").Limit(1).Select()
		if err != nil {
			if err.Error() != "pg: no rows in result set" {
				return time.Time{}, fmt.Errorf("Failed fetching youngest descendant: %s", err.Error())
			}
			break
		}
		youngestDescendnt = &temp
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
func InsertIntoPGReservationsListingsSales(values *prometheus.Labels, unitsSold uint16,
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
	reservationOriginalExpirationDate := listedRI.OriginalEndDate

	// calculating sell events
	sellEvents := []models.ReservationsSellEvents{}
	sellEvent := models.ReservationsSellEvents{ListingID: listingID}
	var reservations []models.Reservations
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
		default:
			youngestDescendntIndex := numResults - 1
			for i := 0; i < youngestDescendntIndex; i++ {
				sold := reservationsInListing[i].Count - reservationsInListing[i+1].Count
				sellEvent.ReservationID = reservationsInListing[i].ReservationID
				sellEvent.UnitsSold = sold
				sellEvent.SoldDate = reservationsInListing[i].EndDate
				sellEvents = append(sellEvents, sellEvent)
				// updating reservation sell_splitted status
				reservation := models.Reservations{
					ReservationID: reservationsInListing[i].ReservationID,
					SellSplitted:  true,
					Sold:          false,
				}
				reservations = append(reservations, reservation)
				unitsSoldAssertion += sold
			}
			youngestSold := reservationsInListing[youngestDescendntIndex].EndDate.Add(
				time.Second).Before(reservationOriginalExpirationDate)
			if youngestSold {
				r := reservationsInListing[youngestDescendntIndex]
				sold := r.Count
				sellEvent.ReservationID = r.ReservationID
				sellEvent.UnitsSold = sold
				sellEvent.SoldDate = r.EndDate
				sellEvents = append(sellEvents, sellEvent)
				unitsSoldAssertion += sold
				// this is the only place "sold" lifecycle status is being set
				// updating reservation sold status
				reservation := models.Reservations{
					ReservationID: r.ReservationID,
					SellSplitted:  false,
					Sold:          true,
				}
				reservations = append(reservations, reservation)
			}
		}

		if unitsSoldAssertion != unitsSold {
			debug.Printf("Failed assertion for sell events on listing %s. Should happen only in first iteration\n", listingID)
			return nil
		}
	}

	listingPublishDate := parseDate((*values)["created_date"])
	const oneMonthDuration = time.Hour * 24 * 30
	listingTotalTerms := int64(len(priceSchedules))
	// check if total terms duration covers remaining reservation life
	expectedDurationTerms := reservationOriginalExpirationDate.Sub(
		listingPublishDate).Truncate(oneMonthDuration)
	expectedTerms := int64(expectedDurationTerms / time.Hour / 24 / 30)
	if expectedTerms != listingTotalTerms {
		// TODO: fix this
		debug.Println("Failed assertion. Got more terms than expected. Should happen only in first iteration")
		return nil
		//return fmt.Errorf("Failed assertion. Got more terms than expected")
	}

	return DB.RunInTransaction(func(tx *pg.Tx) error {
		for _, priceSchedule := range priceSchedules {
			hoursTillExpiration := *priceSchedule.Term * 24 * 30
			duration, _ := time.ParseDuration(fmt.Sprintf("%dh", hoursTillExpiration))
			termEndDate := reservationOriginalExpirationDate.Add(-duration)
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
		if unitsSold > 0 {
			if _, err := DB.Model(&reservations).Column("sell_splitted").Column(
				"sold").WherePK().Update(); err != nil {
				return err
			}
			return upsert(&sellEvents, &[]string{"reservation_id"}, &[]string{"updated_at"})
		}
		return nil
	})
} // TODO: make sure everything is UTC
