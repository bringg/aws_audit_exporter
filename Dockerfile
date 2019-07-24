FROM golang:1.12-alpine as builder
LABEL maintainer "Elad Dolev <dolevelad@gmail.com>"

WORKDIR /tmp/aws_audit_exporter

COPY go.mod go.sum ./

RUN apk add --no-cache git \
    && go mod download

COPY . .

RUN go install \
   && ln -s /go/bin/aws_audit_exporter /usr/local/bin/aws_audit_exporter

#FROM alpine:3.9
#LABEL maintainer "Elad Dolev <dolevelad@gmail.com>"

EXPOSE 9190

ENTRYPOINT ["aws_audit_exporter"]

#RUN apk add --no-cache ca-certificates

#COPY --from=builder /go/bin/aws_audit_exporter /usr/local/bin/

