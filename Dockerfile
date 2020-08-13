FROM golang:1.15-alpine as builder

WORKDIR /tmp/aws_audit_exporter

COPY go.mod go.sum ./
RUN apk add --no-cache git \
    && go mod download

COPY . .
RUN go install

FROM alpine:3.12
LABEL maintainer "Bringg Devops <devops@bringg.com>"

EXPOSE 9190
ENTRYPOINT ["aws_audit_exporter"]

RUN apk add --no-cache ca-certificates
COPY --from=builder /go/bin/aws_audit_exporter /usr/local/bin/
