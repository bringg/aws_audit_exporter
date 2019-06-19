package billing

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
)

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

var cleanre = regexp.MustCompile("[^A-Za-z0-9]")

// Tagname converts to valid Prometheus format
func Tagname(n string) string {
	c := cleanre.ReplaceAllString(n, "_")
	c = strings.ToLower(strings.Trim(c, "_"))
	return "aws_tag_" + c
}