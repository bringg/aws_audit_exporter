package billing

import (
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/prometheus/client_golang/prometheus"
)

var (
	instancesLabels = []string{
		"az",
		"family",
		"groups",
		"instance_type",
		"lifecycle",
		"owner_id",
		"requester_id",
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
	AwsRegion           string
	InstanceLabelsCache *map[string]prometheus.Labels
	InstanceTags        map[string]string
	IsVPC               *map[string]bool
}

// GetInstancesInfo gets instances information
func (s *Instances) GetInstancesInfo() {

	params := &ec2.DescribeInstancesInput{
		Filters: []*ec2.Filter{
			{
				Name:   aws.String("instance-state-code"),
				Values: []*string{aws.String("16")},
			},
		},
	}

	resp, err := s.Svc.DescribeInstances(params)
	if err != nil {
		fmt.Println("there was an error listing instances in", s.AwsRegion, err.Error())
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
			labels["family"], labels["units"] = getInstanceTypeDetails(*ins.InstanceType)
			labels["instance_type"] = *ins.InstanceType
			labels["lifecycle"] = "normal"
			if ins.InstanceLifecycle != nil {
				labels["lifecycle"] = *ins.InstanceLifecycle
			}
			(*s.InstanceLabelsCache)[*ins.InstanceId] = prometheus.Labels{}
			for _, label := range s.InstanceTags {
				labels[label] = "none"
				(*s.InstanceLabelsCache)[*ins.InstanceId][label] = "none"
			}
			for _, tag := range ins.Tags {
				label, ok := s.InstanceTags[*tag.Key]
				if ok {
					labels[label] = *tag.Value
					(*s.InstanceLabelsCache)[*ins.InstanceId][label] = *tag.Value
				}
			}
			if ins.VpcId != nil {
				(*s.IsVPC)[*ins.InstanceId] = true
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
