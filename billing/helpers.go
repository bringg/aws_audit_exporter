package billing

import (
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go/service/ec2"
)

// GetProductDescriptions maps program OS input to AWS format
func GetProductDescriptions(osList string, isVPC bool) ([]*string, error) {
	pList := []*string{}
	for _, p := range strings.Split(osList, ",") {
		var osFullName string
		switch p {
		case "Linux":
			osFullName = "Linux/UNIX"
		case "Windows":
			osFullName = "Windows"
		case "SUSE":
			osFullName = "SUSE Linux"
		case "RHEL":
			osFullName = "Red Hat Enterprise Linux"
		default:
			return nil, errors.New("supported OSs: Linux Windows SUSE RHEL")
		}
		if isVPC {
			osFullName += " (Amazon VPC)"
		}
		pList = append(pList, &osFullName)
	}
	return pList, nil
}

// IsClassicLink returns true if VPC Classic Link is enabled
func IsClassicLink(svc *ec2.EC2) bool {
	var resp *ec2.DescribeVpcClassicLinkOutput
	var err error
	if resp, err = svc.DescribeVpcClassicLink(&ec2.DescribeVpcClassicLinkInput{}); err != nil {
		fmt.Println("there was an error describing vpc")
		log.Fatal(err.Error())
	}

	for _, r := range resp.Vpcs {
		if *r.ClassicLinkEnabled == true {
			return true
		}
	}

	return false
}

func getShortenedSpotMessage(message string) string {
	x := "unknown"
	if strings.Contains(message, "Spot Instance terminated due to user-initiated termination") {
		x = "user-initiated termination"
		return x
	}
	if strings.Contains(message, "Your Spot request is canceled, but your instance") {
		return "canceled still running"
	}
	if message == "Your spot request is fulfilled." {
		return "request fulfilled"
	}
	if message == "Your Spot instance was terminated because there is no Spot capacity available that matches your request." {
		return "no capacity termination"
	}
	// never seen this message before
	if strings.Contains(message, "canceled") {
		return "canceled and terminated"
	}
	return x
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
		units = "192" //TODO: this is wrong
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

var cleanre = regexp.MustCompile("[^A-Za-z0-9]")

// Tagname converts to valid Prometheus format
func Tagname(n string) string {
	c := cleanre.ReplaceAllString(n, "_")
	c = strings.ToLower(strings.Trim(c, "_"))
	return "aws_tag_" + c
}
