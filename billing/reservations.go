package billing

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EladDolev/aws_audit_exporter/postgres"
)

var (
	riLabels = []string{
		"az",
		"count",
		"duration",
		"end_date",
		"family",
		"instance_type",
		"offer_class",
		"offer_type",
		"product",
		"region",
		"ri_id",
		"scope",
		"start_date",
		"state",
		"tenancy",
		"units",
	}

	rilLabels = []string{
		"az",
		"created_date",
		"family",
		"instance_type",
		"months_left",
		"product",
		"region",
		"source_ri_id",
		"ril_id",
		"scope",
		"state",
		"status",
		"status_message",
		"units",
	}

	riEffectiveHourlyPrice    *prometheus.GaugeVec
	riFixedPrice              *prometheus.GaugeVec
	riHourlyPrice             *prometheus.GaugeVec
	riInstanceCount           *prometheus.GaugeVec
	rilInstanceCount          *prometheus.GaugeVec
	rilInstancePrice          *prometheus.GaugeVec
	riTotalNormalizationUnits *prometheus.GaugeVec
)

// RegisterReservationsMetrics constructs and registers Prometheus metrics
func RegisterReservationsMetrics() {

	riEffectiveHourlyPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_effective_unit_price",
		Help: "The effective price of the reservation per normalization unit",
	},
		riLabels)

	riFixedPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_fixed_unit_price",
		Help: "The purchase price of the reservation per normalization unit",
	},
		riLabels)

	riHourlyPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_hourly_unit_price",
		Help: "Hourly reservation reccuring charges per normalization unit",
	},
		riLabels)

	riInstanceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_count",
		Help: "Number of reserved instances in this reservation",
	},
		riLabels)

	rilInstanceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_listing_count",
		Help: "Number of reserved instances listed on the market for a reservation",
	},
		rilLabels)

	rilInstancePrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_listing_price",
		Help: "Current upfront price for which reserved instances are listed on the market",
	},
		rilLabels)

	riTotalNormalizationUnits = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_normalization_units_total",
		Help: "Number of total normalization units in this reservation",
	},
		riLabels)

	prometheus.Register(riEffectiveHourlyPrice)
	prometheus.Register(riFixedPrice)
	prometheus.Register(riHourlyPrice)
	prometheus.Register(riInstanceCount)
	prometheus.Register(rilInstanceCount)
	prometheus.Register(rilInstancePrice)
	prometheus.Register(riTotalNormalizationUnits)
}

// getReservedInstancesListings returns RIs listed on the AWS marketplace
// gets an RI id as an input to act upon, or nil to return all listings
func getReservedInstancesListings(svc *ec2.EC2, reservation *ec2.ReservedInstances) ([]*ec2.ReservedInstancesListing, error) {

	rilparams := &ec2.DescribeReservedInstancesListingsInput{}
	// if won't be set, will return all listings
	if reservation != nil {
		rilparams.SetReservedInstancesId(*reservation.ReservedInstancesId)
	}
	rilresp, err := svc.DescribeReservedInstancesListings(rilparams)
	if err != nil {
		if strings.Contains(err.Error(), "You cannot list your Reserved Instance") {
			return nil, nil
		}
		return nil, fmt.Errorf("there was an error listing reserved instances listings: %v", err)
	}
	return rilresp.ReservedInstancesListings, nil
}

// GetReservationsInfo gets RIs information
func GetReservationsInfo(svc *ec2.EC2) {

	labels := prometheus.Labels{}

	riEffectiveHourlyPrice.Reset()
	riFixedPrice.Reset()
	riHourlyPrice.Reset()
	riInstanceCount.Reset()
	riTotalNormalizationUnits.Reset()

	resp, err := svc.DescribeReservedInstances(&ec2.DescribeReservedInstancesInput{})
	if err != nil {
		log.Fatal(errors.Wrap(err, "there was an error listing instances"))
	}

	ris := map[string]*ec2.ReservedInstances{}
	labels = prometheus.Labels{}
	reservedInstances := resp.ReservedInstances
	// oldest reservation in the sysetm will be first to get processed
	sort.Slice(reservedInstances, func(i, j int) bool {
		return reservedInstances[i].Start.Before(*reservedInstances[j].Start)
	})
	for _, r := range reservedInstances {
		labels["scope"] = *r.Scope
		if *r.Scope == "Region" {
			labels["az"] = "none"
		} else {
			labels["az"] = *r.AvailabilityZone
		}
		labels["count"] = strconv.FormatInt(*r.InstanceCount, 10)
		labels["duration"] = strconv.FormatInt(*r.Duration, 10)
		labels["end_date"] = (*r.End).Format("2006-01-02 15:04:05")
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["ri_id"] = *r.ReservedInstancesId
		labels["instance_type"] = *r.InstanceType
		labels["offer_class"] = *r.OfferingClass
		labels["offer_type"] = *r.OfferingType
		labels["product"] = *r.ProductDescription
		labels["region"] = svc.SigningRegion
		labels["start_date"] = (*r.Start).Format("2006-01-02 15:04:05")
		labels["state"] = *r.State
		labels["tenancy"] = *r.InstanceTenancy
		ris[*r.ReservedInstancesId] = r

		riInstanceCount.With(labels).Add(float64(*r.InstanceCount))

		units, err := strconv.ParseFloat(labels["units"], 64)
		if err != nil {
			log.Fatal(errors.Wrap(err, "There was an error converting normalization units from string to float64"))
		}
		riTotalNormalizationUnits.With(labels).Add(float64(*r.InstanceCount * int64(units)))
		// TODO: validate this is hourly !!
		RC := 0.0
		if len(r.RecurringCharges) > 0 {
			RC = *r.RecurringCharges[0].Amount
		}
		FP := *r.FixedPrice
		// TODO: fix this
		effectivePrice := RC + FP/float64(*r.Duration)*3600
		riEffectiveHourlyPrice.With(labels).Add(effectivePrice / float64(units))
		riHourlyPrice.With(labels).Add(RC / float64(units))
		riFixedPrice.With(labels).Add(FP / float64(units))

		// 'listings' exists only for RIs that had listings created for
		// RI that was created from a parent RI that had some of it's instances sold,
		// will have a ReservedInstancesId pointing to that parent RI, otherwise will point to itself
		// there can be maximum two different RI ids in the array, one of which always point to itself
		listings, err := getReservedInstancesListings(svc, r)
		if err != nil {
			log.Fatal(errors.Wrap(err, "there was an error calling getReservedInstancesListings"))
		}
		// write to db
		if err := postgres.InsertIntoPGReservations(&labels, RC, FP, effectivePrice, &listings); err != nil {
			log.Fatal(errors.Wrapf(err, "There was an error calling InsertIntoPGReservations for: %s", labels["ri_id"]))
		}
	}
	// looking for reservations modifications
	modresp, err := svc.DescribeReservedInstancesModifications(&ec2.DescribeReservedInstancesModificationsInput{})
	if err != nil {
		log.Fatal(errors.Wrap(err, "There was an error calling DescribeReservedInstancesModifications"))
	}
	modificationEvents := modresp.ReservedInstancesModifications
	// getting all listings
	listings, err := getReservedInstancesListings(svc, nil)
	if err != nil {
		log.Fatal(errors.Wrap(err, "there was an error calling getReservedInstancesListings"))
	}

	// write to db
	if err := postgres.InsertIntoPGReservationsRelations(&modificationEvents, &listings, &reservedInstances); err != nil {
		log.Fatal(errors.Wrap(err, "There was an error calling InsertIntoPGReservationsRelations"))
	}

	rilInstanceCount.Reset()
	rilInstancePrice.Reset()
	labels = prometheus.Labels{}
	for _, ril := range listings {
		r, ok := ris[*ril.ReservedInstancesId]
		if !ok {
			log.Println("Reservations listing for unknown reservation")
			continue
		}
		labels["scope"] = *r.Scope
		if *r.Scope == "Region" {
			labels["az"] = "none"
		} else {
			labels["az"] = *r.AvailabilityZone
		}
		labels["source_ri_id"] = *r.ReservedInstancesId
		labels["ril_id"] = *ril.ReservedInstancesListingId
		labels["created_date"] = (*ril.CreateDate).Format("2006-01-02 15:04:05")
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["instance_type"] = *r.InstanceType
		labels["product"] = *r.ProductDescription
		labels["region"] = svc.SigningRegion
		labels["status"] = *ril.Status
		labels["status_message"] = *ril.StatusMessage

		for _, ic := range ril.InstanceCounts {
			labels["state"] = *ic.State
			labels["months_left"] = "0"
			for _, priceSchedule := range ril.PriceSchedules {
				if *priceSchedule.Active {
					labels["months_left"] = strconv.FormatInt(*priceSchedule.Term, 10)
					rilInstanceCount.With(labels).Add(float64(*ic.InstanceCount))
					rilInstancePrice.With(labels).Add(*priceSchedule.Price)
					break
				}
			}
			// write to db
			if err := postgres.InsertIntoPGReservationsListings(&labels, uint16(*ic.InstanceCount)); err != nil {
				log.Fatal(errors.Wrapf(err, "There was an error calling InsertIntoPGReservationsListings for: %s", labels["ril_id"]))
			}
			if labels["state"] == "sold" {
				// write to db
				if err := postgres.InsertIntoPGReservationsListingsSales(&labels,
					uint16(*ic.InstanceCount), ril.PriceSchedules); err != nil {
					log.Fatal(errors.Wrapf(err, "There was an error calling InsertIntoPGReservationsListingsSales for:", labels["ril_id"]))
				}
			}
		}
	}
}
