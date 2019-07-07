package billing

import (
	"fmt"
	"log"
	"strconv"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
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
		"id",
		"instance_type",
		"offer_class",
		"offer_type",
		"product",
		"region",
		"scope",
		"start_date",
		"state",
		"tenancy",
		"units",
	}

	rilLabels = []string{
		"az",
		"family",
		"instance_type",
		"product",
		"scope",
		"state",
		"units",
	}

	riEffectiveHourlyPrice    *prometheus.GaugeVec
	riFixedPrice              *prometheus.GaugeVec
	riHourlyPrice             *prometheus.GaugeVec
	riInstanceCount           *prometheus.GaugeVec
	rilInstanceCount          *prometheus.GaugeVec
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
	prometheus.Register(riTotalNormalizationUnits)
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
		fmt.Println("there was an error listing instances", err.Error())
	}

	ris := map[string]*ec2.ReservedInstances{}
	labels = prometheus.Labels{}
	for _, r := range resp.ReservedInstances {
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
		labels["id"] = *r.ReservedInstancesId
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
			log.Println("There was an error converting normalization units from string to float64")
			log.Fatal(err.Error())
		}
		riTotalNormalizationUnits.With(labels).Add(float64(*r.InstanceCount * int64(units)))
		// TODO: validate this is hourly !!
		RC := 0.0
		if len(r.RecurringCharges) > 0 {
			RC = *r.RecurringCharges[0].Amount
		}
		FP := *r.FixedPrice
		effectivePrice := RC + FP/float64(*r.Duration)*3600
		riEffectiveHourlyPrice.With(labels).Add(effectivePrice / float64(units))
		riHourlyPrice.With(labels).Add(RC / float64(units))
		riFixedPrice.With(labels).Add(FP / float64(units))

		// write to db
		if err := postgres.InsertIntoPGReservations(&labels, RC, FP, effectivePrice); err != nil {
			log.Println("There was an error calling InsertIntoPGReservations for:", labels["id"])
			log.Fatal(err.Error())
		}
	}

	rilparams := &ec2.DescribeReservedInstancesListingsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("status"),
				Values: []*string{aws.String("active")},
			},
		},
	}
	rilresp, err := svc.DescribeReservedInstancesListings(rilparams)
	if err != nil {
		fmt.Println("there was an error listing reserved instances listings", err.Error())
		return
	}
	rilInstanceCount.Reset()

	labels = prometheus.Labels{}
	for _, r := range ris {
		labels["scope"] = *r.Scope
		if *r.Scope == "Region" {
			labels["az"] = "none"
		} else {
			labels["az"] = *r.AvailabilityZone
		}
		labels["instance_type"] = *r.InstanceType
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["product"] = *r.ProductDescription

		for _, s := range []string{"available", "sold", "cancelled", "pending"} {
			labels["state"] = s
			rilInstanceCount.With(labels).Set(0)
		}
	}

	labels = prometheus.Labels{}
	for _, ril := range rilresp.ReservedInstancesListings {
		r, ok := ris[*ril.ReservedInstancesId]
		if !ok {
			fmt.Printf("Reservations listing for unknown reservation")
			continue
		}
		labels["scope"] = *r.Scope
		if *r.Scope == "Region" {
			labels["az"] = "none"
		} else {
			labels["az"] = *r.AvailabilityZone
		}
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["instance_type"] = *r.InstanceType
		labels["product"] = *r.ProductDescription

		for _, ic := range ril.InstanceCounts {
			labels["state"] = *ic.State
			rilInstanceCount.With(labels).Add(float64(*ic.InstanceCount))
		}
	}
}
