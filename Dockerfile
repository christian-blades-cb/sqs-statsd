FROM centurylink/ca-certs

ADD sqs-statsd /

ENTRYPOINT ["/sqs-statsd"]
