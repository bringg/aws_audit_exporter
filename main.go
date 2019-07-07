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
	"strings"
	"time"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/urfave/cli"

	"github.com/EladDolev/aws_audit_exporter/billing"
	"github.com/EladDolev/aws_audit_exporter/debug"
	"github.com/EladDolev/aws_audit_exporter/postgres"
)

type options struct {
	addr           string
	dbURL          string
	duration       time.Duration
	instanceTags   string
	migrationsARGS string
	region         string
	spotOS         string
}

// We have to construct the set of tags for this based on the program
// args, so it is created in main
var instanceTags = map[string]string{}
var tagl = []string{}

// We'll cache the instance tag labels so that we can use them to separate
// out spot instance spend
var instanceLabelsCache = map[string]prometheus.Labels{}

// will hold the list of OS (products) for which spot prices should be fetched
var pList []*string

func main() {
	options := &options{}
	app := cli.NewApp()

	app.Flags = []cli.Flag{
		cli.StringFlag{
			Name:        "addr",
			Value:       ":9190",
			Usage:       "addr to listen on",
			EnvVar:      "ADDR",
			Destination: &options.addr,
		},
		cli.BoolTFlag{
			Name:        "debug",
			Usage:       "Whether to print debug logs and SQL statements",
			EnvVar:      "DEBUG",
			Destination: &debug.Enabled,
		},
		cli.StringFlag{
			Name:        "db-url",
			Usage:       "postgres connection url",
			EnvVar:      "DB_URL",
			Destination: &options.dbURL,
		},
		cli.DurationFlag{
			Name:        "duration",
			Value:       time.Minute * 4,
			Usage:       "How often to query the API",
			EnvVar:      "DURATION",
			Destination: &options.duration,
		},
		cli.StringFlag{
			Name:        "instance-tags",
			Usage:       "comma seperated list of tag keys to use as metric labels",
			EnvVar:      "INSTANCE_TAGS",
			Destination: &options.instanceTags,
		},
		cli.StringFlag{
			Name:        "migrations-args",
			Value:       "",
			Usage:       "args to github.com/go-pg/migrations",
			EnvVar:      "MIGRATIONS_ARGS",
			Destination: &options.migrationsARGS,
		},
		cli.StringFlag{
			Name:        "region",
			Value:       "us-east-1",
			Usage:       "the region to query",
			EnvVar:      "REGION",
			Destination: &options.region,
		},
		cli.StringFlag{
			Name:        "spot-os",
			Value:       "Linux",
			Usage:       "comma seperated list of operating systems to get spot price history for [Linux|SUSE|RHEL|Windows]",
			EnvVar:      "SPOT_OS",
			Destination: &options.spotOS,
		},
	}

	app.Action = func(c *cli.Context) error {

		if len(options.instanceTags) > 0 {
			for _, tstr := range strings.Split(options.instanceTags, ",") {
				ctag := billing.Tagname(tstr)
				instanceTags[tstr] = ctag
				tagl = append(tagl, ctag)
			}
		}

		sess, err := session.NewSession()
		if err != nil {
			return fmt.Errorf("failed to create session: %v", err)
		}

		svc := ec2.New(sess, &aws.Config{Region: aws.String(options.region)})

		if pList, err = billing.GetProductDescriptions(options.spotOS, billing.IsClassicLink(svc)); err != nil {
			return err
		}
		if len(options.dbURL) > 0 {
			if err = postgres.ConnectPostgres(options.dbURL); err != nil {
				return err
			}
			defer postgres.DB.Close()
			if err = postgres.MaintainSchema(options.migrationsARGS); err != nil {
				return err
			}
			if len(options.migrationsARGS) > 0 {
				return nil
			}
		}

		go func() {
			billing.RegisterSpotsPricesMetrics()
			for {
				billing.GetSpotsCurrentPrices(svc, pList)
				<-time.After(time.Hour)
			}
		}()

		go func() {
			instances := &billing.Instances{
				Svc:                 svc,
				InstanceLabelsCache: &instanceLabelsCache,
				InstanceTags:        instanceTags,
			}
			spots := &billing.Spots{
				Svc:                 svc,
				InstanceLabelsCache: &instanceLabelsCache,
			}

			billing.RegisterInstancesMetrics(tagl)
			billing.RegisterReservationsMetrics()
			billing.RegisterSpotsMetrics(tagl)

			for {
				instances.GetInstancesInfo()
				go billing.GetReservationsInfo(svc)
				go spots.GetSpotsInfo()
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
