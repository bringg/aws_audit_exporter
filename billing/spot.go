package billing

import (
	"fmt"
	"log"
	"strconv"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EladDolev/aws_audit_exporter/postgres"
)

var (
	siLabels = []string{
		"az",
		"block_duration",
		"family",
		"instance_profile",
		"instance_type",
		"launch_group",
		"persistence",
		"product",
		"request_id",
		"short_status",
		"state",
		"status",
		"units",
	}

	sphLabels = []string{
		"az",
		"family",
		"instance_type",
		"product",
		"units",
	}

	siBidPrice         *prometheus.GaugeVec
	siBlockHourlyPrice *prometheus.GaugeVec
	siCount            *prometheus.GaugeVec
	sphPrice           *prometheus.GaugeVec
)

// RegisterSpotsMetrics constructs and registers Prometheus metrics
func RegisterSpotsMetrics(tagList []string) {

	siBidPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_spot_request_bid_price_hourly_dollars",
		Help: "cost of spot instances hourly usage in dollars",
	},
		append(siLabels, tagList...))

	siBlockHourlyPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_spot_request_actual_block_price_hourly_dollars",
		Help: "fixed hourly cost of limited duration spot instances in dollars",
	},
		append(siLabels, tagList...))

	siCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_spot_request_count",
		Help: "Number of active/fullfilled spot requests",
	},
		append(siLabels, tagList...))

	prometheus.Register(siBidPrice)
	prometheus.Register(siBlockHourlyPrice)
	prometheus.Register(siCount)
}

// RegisterSpotsPricesMetrics constructs and registers Prometheus metrics
func RegisterSpotsPricesMetrics() {

	sphPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_spot_price_per_hour_dollars",
		Help: "Current market price of a spot instance, per hour,  in dollars",
	},
		sphLabels)

	prometheus.Register(sphPrice)
}

// Spots parameters to be passed from main
type Spots struct {
	Svc                 *ec2.EC2
	InstanceLabelsCache *map[string]prometheus.Labels
	InstanceTags        map[string]string
}

// GetSpotsInfo gets spot instances information
func (s *Spots) GetSpotsInfo() {

	resp, err := s.Svc.DescribeSpotInstanceRequests(&ec2.DescribeSpotInstanceRequestsInput{})
	if err != nil {
		fmt.Println("there was an error listing spot requests")
		log.Fatal(err.Error())
	}

	labels := prometheus.Labels{}
	siBidPrice.Reset()
	siBlockHourlyPrice.Reset()
	siCount.Reset()

	for _, r := range resp.SpotInstanceRequests {
		if r.InstanceId != nil {
			if ilabels, ok := (*s.InstanceLabelsCache)[*r.InstanceId]; ok {
				for k, v := range ilabels {
					labels[k] = v
				}
			} else {
				for _, label := range s.InstanceTags {
					labels[label] = "unknown"
				}
			}
		}

		labels["az"] = *r.LaunchedAvailabilityZone
		labels["request_id"] = *r.SpotInstanceRequestId
		labels["state"] = *r.State
		labels["status"] = *r.Status.Message
		labels["short_status"] = getShortenedSpotMessage(*r.Status.Message)
		labels["product"] = *r.ProductDescription

		labels["persistence"] = "one-time"
		if r.Type != nil {
			labels["persistence"] = *r.Type
		}

		labels["launch_group"] = "none"
		if r.LaunchGroup != nil {
			labels["launch_group"] = *r.LaunchGroup
		}

		labels["instance_type"] = "unknown"
		labels["family"] = "unknown"
		labels["units"] = "unknown"
		if r.LaunchSpecification != nil && r.LaunchSpecification.InstanceType != nil {
			labels["instance_type"] = *r.LaunchSpecification.InstanceType
			labels["family"], labels["units"] = getInstanceTypeDetails(*r.LaunchSpecification.InstanceType)
		}

		labels["instance_profile"] = "unknown"
		if r.LaunchSpecification != nil && r.LaunchSpecification.IamInstanceProfile != nil {
			labels["instance_profile"] = *r.LaunchSpecification.IamInstanceProfile.Name
		}

		labels["block_duration"] = "none"
		if r.ActualBlockHourlyPrice != nil {
			labels["block_duration"] = strconv.FormatInt(*r.BlockDurationMinutes, 10)
			if price, err := strconv.ParseFloat(*r.ActualBlockHourlyPrice, 64); err == nil {
				siBlockHourlyPrice.With(labels).Add(price)
			}
		}

		if r.SpotPrice != nil {
			if price, err := strconv.ParseFloat(*r.SpotPrice, 64); err == nil {
				siBidPrice.With(labels).Add(price)
			}
		}

		siCount.With(labels).Inc()
	}
}

// GetSpotsCurrentPrices gets spot current prices
func GetSpotsCurrentPrices(svc *ec2.EC2, pList []*string) {
	phParams := &ec2.DescribeSpotPriceHistoryInput{
		StartTime:           aws.Time(time.Now()),
		EndTime:             aws.Time(time.Now()),
		ProductDescriptions: pList,
	}
	err := svc.DescribeSpotPriceHistoryPages(phParams,
		func(page *ec2.DescribeSpotPriceHistoryOutput, lastPage bool) bool {
			spLabels := prometheus.Labels{}
			for _, sp := range page.SpotPriceHistory {
				spLabels["az"] = *sp.AvailabilityZone
				spLabels["product"] = *sp.ProductDescription
				spLabels["instance_type"] = *sp.InstanceType
				spLabels["family"], spLabels["units"] = getInstanceTypeDetails(*sp.InstanceType)
				if sp.SpotPrice != nil {
					if f, err := strconv.ParseFloat(*sp.SpotPrice, 64); err == nil {
						sphPrice.With(spLabels).Set(f)
						// write to db
						if err = postgres.InsertIntoPGSpotPrices(&spLabels, f); err != nil {
							log.Println("There was an error calling insertIntoPGSpotPrices")
							log.Fatal(err.Error())
						}
					}
				}
			}
			return !lastPage
		})

	if err != nil {
		fmt.Println("there was an error listing spot requests")
		log.Fatal(err.Error())
	}
}
