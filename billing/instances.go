package billing

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/prometheus/client_golang/prometheus"

	"github.com/EladDolev/aws_audit_exporter/postgres"
)

var (
	instancesLabels = []string{
		"az",
		"family",
		"groups",
		"instance_id",
		"instance_type",
		"launch_time",
		"lifecycle",
		"owner_id",
		"requester_id",
		"state",
		"units",
	}

	instancesCount              *prometheus.GaugeVec
	instancesNormalizationUnits *prometheus.GaugeVec
)

// RegisterInstancesMetrics constructs and registers Prometheus metrics
func RegisterInstancesMetrics(tagList []string) {
	instancesCount = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_instances_count",
		Help: "Running EC2 instances count",
	},
		append(instancesLabels, tagList...))

	instancesNormalizationUnits = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "aws_ec2_instances_normalization_units_total",
		Help: "Running EC2 instances total normalization units",
	},
		append(instancesLabels, tagList...))

	prometheus.Register(instancesCount)
	prometheus.Register(instancesNormalizationUnits)
}

// Instances parameters to be passed from main
type Instances struct {
	Svc                 *ec2.EC2
	InstanceLabelsCache *map[string]prometheus.Labels
	InstanceTags        map[string]string
}

// GetInstancesInfo gets instances information
func (s *Instances) GetInstancesInfo() {

	resp, err := s.Svc.DescribeInstances(&ec2.DescribeInstancesInput{})
	if err != nil {
		fmt.Println("There was an error listing instances")
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
			labels["state"] = *(*ins.State).Name
			labels["family"], labels["units"] = getInstanceTypeDetails(*ins.InstanceType)
			labels["instance_id"] = *ins.InstanceId
			labels["instance_type"] = *ins.InstanceType
			labels["launch_time"] = (*ins.LaunchTime).Format("2006-01-02 15:04:05")
			labels["lifecycle"] = "normal"
			if ins.InstanceLifecycle != nil {
				labels["lifecycle"] = *ins.InstanceLifecycle
			}
			// TODO: bring back the mutex
			(*s.InstanceLabelsCache)[*ins.InstanceId] = prometheus.Labels{}
			for _, label := range s.InstanceTags {
				labels[label] = "none"
				(*s.InstanceLabelsCache)[*ins.InstanceId][label] = "none"
			}
			tags := make(map[string]string)
			for _, tag := range ins.Tags {
				label, ok := s.InstanceTags[*tag.Key]
				if ok {
					tags[*tag.Key] = *tag.Value
					labels[label] = *tag.Value
					(*s.InstanceLabelsCache)[*ins.InstanceId][label] = *tag.Value
				}
			}

			instancesCount.With(labels).Inc()

			units, err := strconv.ParseFloat(labels["units"], 64)
			if err != nil {
				log.Println("There was an error converting normalization units from string to float64")
				log.Fatal(err.Error())
			}

			instancesNormalizationUnits.With(labels).Add(units)

			// write to db
			if err := postgres.InsertIntoPGInstances(&labels, tags); err != nil {
				log.Println("There was an error calling insertIntoPGInstances for:", labels["instance_id"])
				log.Fatal(err.Error())
			}
		}
	}
}
