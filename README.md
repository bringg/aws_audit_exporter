# Prometheus AWS audit exporter

This program is intended to export various AWS statistics as prometheus
metrics. It is primarily intended to assist with billing. Currently the
following metrics are exported:

# EC2 Instances

 - *aws_ec2_instances_count*: Count of istances
 - *instancesNormalizationUnits*: Normalization units of istances

The following labels are exposed:

 - *az*: Availability zone
 - *family*: Instance family
 - *groups*: [EC2-Classic only] sorted comma separated list of security groups
 - *instance_type*: Type of instance
 - *lifecycle*: spot, scheduled or normal instance
 - *owner_id*: The owner id
 - *requester_id*: The requester id (default to owner id if none is present)
 - *units*: The normalization units of the instance
 - *aws_tag_*: Any tags passed in with the -instance-tags flag are added as labels

# EC2 Reserved Instances
Every set of instance reservations gets its own time series, this is intended to allow
the end time of reserved intances to be tracked and potentially alerted upon.

 - *aws_ec2_reserved_instances_fixed_unit_price*: Purchase price of the reservation per normalization unit in dollars
 - *aws_ec2_reserved_instances_hourly_unit_price*: Hourly reservation reccuring charges per normalization unit in dollars
 - *aws_ec2_reserved_instances_count*: Number of Reserved instances in this reservation
 - *aws_ec2_reserved_instances_normalization_units_total*: Number of total normalization units in this reservation

The following labels are exposed:

 - *az*: Availability zone
 - *duration*: Duration of the reservation in seconds
 - *end_date*: Date on which the reservation is retired
 - *family*: Instance family
 - *id*: The reservation id
 - *instance_type*: Type of instance
 - *offer_type*: The Reserved Instance offering type (No Upfront | Partial Upfront | All Upfront)
 - *product*: The product description
 - *scope*: Region or Availability Zone
 - *state*: The state of the Reserved Instance (payment-pending | active | payment-failed)
 - *tenancy*: The tenancy of the instance (default | dedicated)
 - *units*: The normalization units of the instance

# EC2 Reserved Instances listed on the marketplace

 - *aws_ec2_reserved_instances_listing_count*: Reserved instances listed on the market for a reservation

The following labels are exposed:

 - *az*: Availability zone
 - *family*: Instance family
 - *instance_type*: Type of instance
 - *product*: The product description
 - *scope*: Region or Availability Zone
 - *state*: The state of the listed Reserved Instances
 - *units*: The normalization units of the instance

# EC2 Spot Instance Request

Only fullfilled active spot instances requests are currently tracke

 - *aws_ec2_spot_request_count*: How active spot instances of a given type you have running
 - *aws_ec2_spot_request_bid_price_hourly_dollars*: Your maximum bid price
 - *aws_ec2_spot_request_actual_block_price_hourly_dollars*: The price paid for limited duration spot instances

The following labels are exposed:

 - *az*: availability zone
 - *family*: Instance family
 - *instance_profile*: The IAM instance profile
 - *instance_type*: type of instance
 - *product*: The product description
 - *launch_group*: The Spot Instance launch group
 - *persistence*: The type of Spot Instance request (one-time | persistent)
 - *units*: The normalization units of the instance

# EC2 Spot Instance Pricing

Only prices for products that have been seen in spot instance requests are tracked.

 - *aws_ec2_spot_price_per_hour_dollars*: The current market price for a spot instance

The following labels are exposed:

 - *az*: availability zone
 - *family*: Instance family
 - *instance_type*: type of instance
 - *product*: The product description
 - *units*: The normalization units of the instance

# Usage

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
