// Copyright 2016 Qubit Group
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/cli"
)

var (
	riLabels = []string{
		"az",
		"scope",
		"tenancy",
		"instance_type",
		"offer_type",
		"product",
		"family",
		"state",
		"units",
		"end_date",
		"id",
		"duration",
	}

	riFixedPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_fixed_unit_price",
		Help: "The purchase price of the reservation per normalization unit",
	},
		riLabels)

	riHourlyPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_hourly_unit_price",
		Help: "The reccuring charges per hour of the reservation per normalization unit",
	},
		riLabels)

	riTotalNormalizationUnits = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_normalization_units_total",
		Help: "Number of total normalization units in this reservation",
	},
		riLabels)

	riInstanceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_count",
		Help: "Number of reserved instances in this reservation",
	},
		riLabels)

	rilLabels = []string{
		"az",
		"scope",
		"instance_type",
		"product",
		"state",
		"family",
		"units",
	}

	rilInstanceCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_reserved_instances_listing_count",
		Help: "Number of reserved instances listed on the market for a reservation",
	},
		rilLabels)

	instancesLabels = []string{
		"groups",
		"owner_id",
		"requester_id",
		"az",
		"instance_type",
		"lifecycle",
		"family",
		"units",
	}

	siLabels = []string{
		"az",
		"product",
		"persistence",
		"instance_type",
		"launch_group",
		"instance_profile",
		"family",
		"units",
	}

	sphLabels = []string{
		"az",
		"product",
		"instance_type",
		"family",
		"units",
	}

	sphPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_spot_price_per_hour_dollars",
		Help: "Current market price of a spot instance, per hour,  in dollars",
	},
		sphLabels)
)

type options struct {
	region       string
	addr         string
	instanceTags string
	duration     time.Duration
}

func getInstanceTypeDetails(instanceType string) (string, string) {
	if instanceType == "" {
		return "", ""
	}
	arr := regexp.MustCompile(`\.`).Split(instanceType, 2)
	family, size := arr[0], arr[1]
	var units string
	switch size {
	case "metal":
		units = "192"
	case "nano":
		units = "0.25"
	case "micro":
		units = "0.5"
	case "small":
		units = "1"
	case "medium":
		units = "2"
	case "large":
		units = "4"
	case "xlarge":
		units = "8"
	default:
		multiplierString := regexp.MustCompile(`xlarge`).Split(size, 2)[0]
		multiplier, err := strconv.Atoi(multiplierString)
		if err != nil {
			fmt.Println("there was an error in breaking instance type into family and units", err.Error())
			log.Fatal(err.Error())
		}
		units = strconv.Itoa(8 * multiplier)
	}

	return family, units
}

// We have to construct the set of tags for this based on the program
// args, so it is created in main
var instancesCount *prometheus.GaugeVec
var instancesNormalizationUnits *prometheus.GaugeVec
var instanceTags = map[string]string{}

// Similarly, we want to use the instance labels in the spot instance
// metrics
var siCount *prometheus.GaugeVec
var siBidPrice *prometheus.GaugeVec
var siBlockHourlyPrice *prometheus.GaugeVec

// We'll cache the instance tag labels so that we can use them to separate
// out spot instance spend
var instanceLabelsCacheMutex = sync.RWMutex{}
var instanceLabelsCache = map[string]prometheus.Labels{}
var instanceLabelsCacheIsVPC = map[string]bool{}

func main() {
	options := &options{}
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "region",
			Value:       "us-east-1",
			Usage:       "the region to query",
			EnvVar:      "REGION",
			Destination: &options.region,
		},
		cli.StringFlag{
			Name:        "instance-tags",
			Usage:       "comma seperated list of tag keys to use as metric labels",
			EnvVar:      "INSTANCE_TAGS",
			Destination: &options.instanceTags,
		},
		cli.DurationFlag{
			Name:        "duration",
			Value:       time.Minute * 4,
			Usage:       "How often to query the API",
			EnvVar:      "DURATION",
			Destination: &options.duration,
		},
		cli.StringFlag{
			Name:        "addr",
			Value:       ":9190",
			Usage:       "addr to listen on",
			EnvVar:      "ADDR",
			Destination: &options.addr,
		},
	}

	app.Action = func(c *cli.Context) error {

		tagl := []string{}

		for _, tstr := range strings.Split(options.instanceTags, ",") {
			ctag := tagname(tstr)
			instanceTags[tstr] = ctag
			tagl = append(tagl, ctag)
		}

		instancesCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aws_ec2_instances_count",
			Help: "Running EC2 instances count",
		},
			append(instancesLabels, tagl...))

		instancesNormalizationUnits = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aws_ec2_instances_normalization_units_total",
			Help: "Running EC2 instances total normalization units",
		},
			append(instancesLabels, tagl...))

		siCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aws_ec2_spot_request_count",
			Help: "Number of active/fullfilled spot requests",
		},
			append(siLabels, tagl...))

		siBidPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aws_ec2_spot_request_bid_price_hourly_dollars",
			Help: "cost of spot instances hourly usage in dollars",
		},
			append(siLabels, tagl...))

		siBlockHourlyPrice = prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "aws_ec2_spot_request_actual_block_price_hourly_dollars",
			Help: "fixed hourly cost of limited duration spot instances in dollars",
		},
			append(siLabels, tagl...))

		prometheus.Register(instancesCount)
		prometheus.Register(instancesNormalizationUnits)
		prometheus.Register(riTotalNormalizationUnits)
		prometheus.Register(riInstanceCount)
		prometheus.Register(rilInstanceCount)
		prometheus.Register(riHourlyPrice)
		prometheus.Register(riFixedPrice)
		prometheus.Register(siCount)
		prometheus.Register(siBidPrice)
		prometheus.Register(siBlockHourlyPrice)
		prometheus.Register(sphPrice)

		sess, err := session.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create session %v", err)
		}

		svc := ec2.New(sess, &aws.Config{Region: aws.String(options.region)})

		go func() {
			for {
				instances(svc, options.region)
				go reservations(svc, options.region)
				go spots(svc, options.region)
				<-time.After(options.duration)
			}
		}()

		http.Handle("/metrics", prometheus.Handler())

		return http.ListenAndServe(options.addr, nil)
	}

	if err := app.Run(os.Args); err != nil {
		log.Fatal(err)
	}
}
func instances(svc *ec2.EC2, awsRegion string) {
	instanceLabelsCacheMutex.Lock()
	defer instanceLabelsCacheMutex.Unlock()

	//Clear the cache
	instanceLabelsCache = map[string]prometheus.Labels{}
	instanceLabelsCacheIsVPC = map[string]bool{}

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance-state-code"),
				Values: []*string{aws.String("16")},
			},
		},
	}
	resp, err := svc.DescribeInstances(params)
	if err != nil {
		fmt.Println("there was an error listing instances in", awsRegion, err.Error())
		log.Fatal(err.Error())
	}

	instancesCount.Reset()
	instancesNormalizationUnits.Reset()
	labels := prometheus.Labels{}
	for _, r := range resp.Reservations {
		groups := []string{}
		for _, g := range r.Groups {
			groups = append(groups, *g.GroupName)
		}
		sort.Strings(groups)
		labels["groups"] = strings.Join(groups, ",")
		labels["owner_id"] = *r.OwnerId
		labels["requester_id"] = *r.OwnerId
		if r.RequesterId != nil {
			labels["requester_id"] = *r.RequesterId
		}
		for _, ins := range r.Instances {
			labels["az"] = *ins.Placement.AvailabilityZone
			labels["instance_type"] = *ins.InstanceType
			labels["family"], labels["units"] = getInstanceTypeDetails(*ins.InstanceType)
			labels["lifecycle"] = "normal"
			if ins.InstanceLifecycle != nil {
				labels["lifecycle"] = *ins.InstanceLifecycle
			}
			instanceLabelsCache[*ins.InstanceId] = prometheus.Labels{}
			for _, label := range instanceTags {
				labels[label] = "none"
				instanceLabelsCache[*ins.InstanceId][label] = ""
			}
			for _, tag := range ins.Tags {
				label, ok := instanceTags[*tag.Key]
				if ok {
					labels[label] = *tag.Value
					instanceLabelsCache[*ins.InstanceId][label] = *tag.Value
				}
			}
			if ins.VpcId != nil {
				instanceLabelsCacheIsVPC[*ins.InstanceId] = true
			}
			instancesCount.With(labels).Inc()
			units, err := strconv.Atoi(labels["units"])
			if err != nil {
				fmt.Println("there was an error converting normalization units from string to an int")
				return
			}
			instancesNormalizationUnits.With(labels).Add(float64(units))
		}
	}
}

func reservations(svc *ec2.EC2, awsRegion string) {
	instanceLabelsCacheMutex.RLock()
	defer instanceLabelsCacheMutex.RUnlock()

	labels := prometheus.Labels{}
	riInstanceCount.Reset()
	riTotalNormalizationUnits.Reset()
	riHourlyPrice.Reset()
	riFixedPrice.Reset()

	params := &ec2.DescribeReservedInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("state"),
				Values: []*string{aws.String("active")},
			},
		},
	}
	resp, err := svc.DescribeReservedInstances(params)
	if err != nil {
		fmt.Println("there was an error listing instances in", awsRegion, err.Error())
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
		labels["instance_type"] = *r.InstanceType
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["tenancy"] = *r.InstanceTenancy
		labels["offer_type"] = *r.OfferingType
		labels["product"] = *r.ProductDescription
		labels["state"] = *r.State
		labels["duration"] = strconv.FormatInt(*r.Duration, 10)
		labels["id"] = *r.ReservedInstancesId
		labels["end_date"] = (*r.End).Format("2006-01-02 15:04:05")
		ris[*r.ReservedInstancesId] = r

		riInstanceCount.With(labels).Add(float64(*r.InstanceCount))

		units, err := strconv.Atoi(labels["units"])
		if err != nil {
			fmt.Println("there was an error converting normalization units from string to an int")
			return
		}
		riTotalNormalizationUnits.With(labels).Add(float64(*r.InstanceCount * int64(units)))
		// TODO: validate this is hourly !!
		riHourlyPrice.With(labels).Add(*r.RecurringCharges[0].Amount / float64(units))
		riFixedPrice.With(labels).Add(*r.FixedPrice / float64(units))
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
		fmt.Println("there was an error listing reserved instances listingsin", awsRegion, err.Error())
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
		labels["instance_type"] = *r.InstanceType
		labels["family"], labels["units"] = getInstanceTypeDetails(*r.InstanceType)
		labels["product"] = *r.ProductDescription

		for _, ic := range ril.InstanceCounts {
			labels["state"] = *ic.State
			rilInstanceCount.With(labels).Add(float64(*ic.InstanceCount))
		}
	}
}

func spots(svc *ec2.EC2, awsRegion string) {
	instanceLabelsCacheMutex.RLock()
	defer instanceLabelsCacheMutex.RUnlock()

	params := &ec2.DescribeSpotInstanceRequestsInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("state"),
				Values: []*string{aws.String("active")},
			},
		},
	}
	resp, err := svc.DescribeSpotInstanceRequests(params)
	if err != nil {
		fmt.Println("there was an error listing spot requests", awsRegion, err.Error())
		log.Fatal(err.Error())
	}

	productSeen := map[string]bool{}

	labels := prometheus.Labels{}
	siCount.Reset()
	siBlockHourlyPrice.Reset()
	siBidPrice.Reset()
	for _, r := range resp.SpotInstanceRequests {
		for _, label := range instanceTags {
			labels[label] = ""
		}
		if r.InstanceId != nil {
			if ilabels, ok := instanceLabelsCache[*r.InstanceId]; ok {
				for k, v := range ilabels {
					labels[k] = v
				}
			}
		}

		labels["az"] = *r.LaunchedAvailabilityZone

		product := *r.ProductDescription
		if isVpc, ok := instanceLabelsCacheIsVPC[*r.InstanceId]; ok && isVpc {
			product += " (Amazon VPC)"
		}
		labels["product"] = product
		productSeen[product] = true

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

		price := 0.0
		if r.ActualBlockHourlyPrice != nil {
			if f, err := strconv.ParseFloat(*r.ActualBlockHourlyPrice, 64); err == nil {
				price = f
			}
		}
		siBlockHourlyPrice.With(labels).Add(price)

		price = 0
		if r.SpotPrice != nil {
			if f, err := strconv.ParseFloat(*r.SpotPrice, 64); err == nil {
				price = f
			}
		}
		siBidPrice.With(labels).Add(price)

		siCount.With(labels).Inc()
	}

	// This is silly, but spot instances requests don't seem to include the vpc case
	pList := []*string{}
	for p := range productSeen {
		pp := p
		pList = append(pList, &pp)
	}

	phParams := &ec2.DescribeSpotPriceHistoryInput{
		StartTime: aws.Time(time.Now()),
		EndTime:   aws.Time(time.Now()),
		//		ProductDescriptions: pList,
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("product-description"),
				Values: pList,
			},
		},
	}
	err = svc.DescribeSpotPriceHistoryPages(phParams,
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
					}
				}
			}
			return !lastPage
		})

	if err != nil {
		fmt.Println("there was an error listing spot requests", awsRegion, err.Error())
		log.Fatal(err.Error())
	}
}

var cleanre = regexp.MustCompile("[^A-Za-z0-9]")

func tagname(n string) string {
	c := cleanre.ReplaceAllString(n, "_")
	c = strings.ToLower(strings.Trim(c, "_"))
	return "aws_tag_" + c
}
