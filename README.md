# Prometheus AWS audit exporter

This program is primarily intended to assist with billing.
Collects various AWS statistics, exports them as prometheus metrics, and saves them in RDBMS.

Run help or check main.go for program parameters
Debug messages are enabled by default

Currently the following metrics are exported:

## EC2 Instances

- *aws_ec2_instances_count*: Count of istances
- *instancesNormalizationUnits*: Normalization units of istances

The following labels are exposed:

- *aws_tag_*: Any tags passed in with the -instance-tags flag are added as labels
- *az*: Availability zone
- *family*: Instance family
- *groups*: [EC2-Classic only] sorted comma separated list of security groups
- *instance_id*: Id of instance
- *instance_type*: Type of instance
- *launch_time*: Time on which instance was started
- *lifecycle*: spot, scheduled or normal instance
- *owner_id*: The AWS account ID of the instance owner
- *requester_id*: The ID of the entity that launched the instance on your behalf (default to owner id if none is present)
- *state*: State of instance (pending | running | shutting-down | rebooting | terminated | stopping | stopped)
- *units*: The normalization units of the instance

## EC2 Reserved Instances

Every set of instance reservations gets its own time series, this is intended to allow
the end time of reserved intances to be tracked and potentially alerted upon.

- *aws_ec2_reserved_instances_count*: Number of Reserved instances in this reservation
- *aws_ec2_reserved_instances_effective_unit_price*: Hourly reservation effective charges per normalization unit in dollars
- *aws_ec2_reserved_instances_fixed_unit_price*: Purchase price of the reservation per normalization unit in dollars
- *aws_ec2_reserved_instances_hourly_unit_price*: Hourly reservation reccuring charges per normalization unit in dollars
- *aws_ec2_reserved_instances_normalization_units_total*: Number of total normalization units in this reservation

The following labels are exposed:

- *az*: Availability zone
- *count*: The count of instances in the reservation
- *duration*: Duration of the reservation in seconds
- *end_date*: Date on which the reservation is retired
- *family*: Instance family
- *instance_type*: Type of instance
- *offer_class*: The Reserved Instance class type (Standard | Convertible)
- *offer_type*: The Reserved Instance offering type (No Upfront | Partial Upfront | All Upfront)
- *product*: The product description
- *region*: The region in which the reservation exists
- *ri_id*: The reservation id
- *scope*: Region or Availability Zone
- *start_date*: Date on which the reservation started
- *state*: The state of the Reserved Instance (payment-pending | active | payment-failed)
- *tenancy*: The tenancy of the instance (default | dedicated)
- *units*: The normalization units of the instance

## EC2 Reserved Instances listed on the marketplace

- *aws_ec2_reserved_instances_listing_count*: Reserved instances listed on the market for a reservation
- *aws_ec2_reserved_instances_listing_price*: Current upfront price for listing

The following labels are exposed:

- *az*: Availability zone
- *created_date*: Date on which listing was created
- *family*: Instance family
- *instance_type*: Type of instance
- *months_left*: Months left till Reserved Instances are retired
- *product*: The product description
- *region*: The region in which the Reserved Instances exists
- *ril_id*: The reservation listing id
- *scope*: Region or Availability Zone
- *source_ri_id*: The original reservation id for which listing was created
- *state*: The state of the listed Reserved Instances (available, cancelled, pending, sold)
- *status_message*: The reason for the current status of the Reserved Instance listing. The response can be blank
- *status*: The status of the listed Reserved Instances (active, cancelled, closed, pending)
- *units*: The normalization units of the Reserved Instances

## EC2 Spot Instance Request

Only fullfilled active spot instances requests are currently tracke

- *aws_ec2_spot_request_actual_block_price_hourly_dollars*: The price paid for limited duration spot instances
- *aws_ec2_spot_request_bid_price_hourly_dollars*: Your maximum bid price
- *aws_ec2_spot_request_count*: How active spot instances of a given type you have running

The following labels are exposed:

- *az*: Availability zone
- *block_duration*: Spot block duration (1 to 6 hours)
- *family*: Instance family
- *instance_profile*: The IAM instance profile
- *instance_type*: Type of instance
- *launch_group*: The Spot Instance launch group
- *persistence*: The type of Spot Instance request (one-time | persistent)
- *product*: The product description
- *request_id*: The unique identifier of the spot request
- *short_status*: Shortened status of the instance
- *state*: State of the instance (open | active | closed | cancelled | failed)
- *status*: Status of the instance
- *units*: The normalization units of the instance

## EC2 Spot Instance Pricing

Only prices for products that have been seen in spot instance requests are tracked.

- *aws_ec2_spot_price_per_hour_dollars*: The current market price for a spot instance

The following labels are exposed:

- *az*: availability zone
- *family*: Instance family
- *instance_type*: Type of instance
- *product*: The product description
- *units*: The normalization units of the instance

## Usage

  Your aws credentials should either be in $HOME/.aws/credentials , or set via AWS\_ACCESS\_KEY and AWS\_SECRET\_ACCESS\_KEY

  Usage of /go/bin/aws_audit_exporter:
  -addr string
        port to listen on (default ":9190")
  -duration duration
        How often to query the API (default 4m0s)
  -instance-tags string
        comma seperated list of tag keys to use as metric labels
  -region string
        the region to query (default "us-east-1")

## IAM Role

Below is an IAM role with the required permissions

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Action": [
        "ec2:DescribeInstances",
        "ec2:DescribeReservedInstances*",
        "ec2:DescribeSpot*",
        "ec2:DescribeVpcClassicLink"
      ],
      "Resource": [
        "*"
      ],
      "Effect": "Allow"
    }
  ]
}
```

## Write data to Postgres

Writes data to postgres to allow longer retention, and data aggregations
Was tested on postgresql 9.6 in RDS

Information in the DB is eventual consistent, i.e. it will take a full iteration for data to be updated.
Sell events are being pulled from CloudTrail, so if not enabled or if data is beign collected for events older than 90 days,
history might not be correct. Once data was collected from CloudTrail, it won't be changed after time.

hstore extention needs to enable on the database in use
Run with a privelleged user:

```shell
aws_audit=> CREATE EXTENSION hstore;
```
