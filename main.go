package main // import "github.com/christian-blades-cb/sqs-statsd"

import (
	"time"

	"fmt"
	log "github.com/Sirupsen/logrus"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/cloudwatch"
	"github.com/jessevdk/go-flags"
	"github.com/joho/godotenv"
	"github.com/quipo/statsd"
	"strings"
)

func main() {
	var opts struct {
		AWSAccess  string `long:"aws-access" env:"ACCESS_KEY" required:"true"`
		AWSSecret  string `long:"aws-secret" env:"SECRET_KEY" required:"true"`
		AWSRegion  string `long:"aws-region" env:"AWS_REGION" default:"us-east-1"`
		StatsDHost string `long:"statsd-host" env:"STATSD_HOST" default:"localhost:8125"`
		Verbose    bool   `long:"verbose" short:"v" env:"DEBUG" default:"false"`
	}

	godotenv.Load(".env")
	if _, err := flags.Parse(&opts); err != nil {
		log.Fatal("cannot parse command line arguments")
	}

	if opts.Verbose {
		log.SetLevel(log.DebugLevel)
	}

	statsdbuffer, err := getStatsdBuffer(opts.StatsDHost)
	if err != nil {
		log.Fatal("could not initialize statsd client")
	}
	statsdbuffer.Logger = log.StandardLogger()

	auth, err := aws.GetAuth(opts.AWSAccess, opts.AWSSecret, "", time.Now())
	if err != nil {
		log.WithField("error", err).Fatal("could not authenticate to aws")
	}

	region := aws.Regions[opts.AWSRegion]
	cw, err := cloudwatch.NewCloudWatch(auth, region.CloudWatchServicepoint)
	if err != nil {
		log.WithFields(log.Fields{
			"error":  err,
			"region": opts.AWSRegion,
		}).Fatal("could not open cloudwatch")
	}

	requests, err := getMetricRequests(cw)
	if err != nil {
		log.WithField("error", err).Fatal("could not build requests")
	}
	log.WithField("num_metrics", len(requests)).Info("built stats requests")

	ticker := time.NewTicker(time.Minute)
	for now := range ticker.C {
		goStats(cw, &requests, now, statsdbuffer)
	}
}

func goStats(cw *cloudwatch.CloudWatch, requests *[]cloudwatch.GetMetricStatisticsRequest, now time.Time, statsCli *statsd.StatsdBuffer) {
	log.WithField("now", now).Debug("tick")
	for _, request := range *requests {
		request.EndTime = now
		request.StartTime = now.Add(time.Minute * -1) // 1 minute ago

		response, err := cw.GetMetricStatistics(&request)
		if err != nil {
			log.WithField("error", err).Warn("could not retrieve metric from cloudwatch")
		}

		for _, dp := range response.GetMetricStatisticsResult.Datapoints {
			statsCli.Absolute(fmt.Sprintf("%s.%s", strings.ToLower(request.Dimensions[0].Value), strings.ToLower(request.MetricName)), int64(dp.Sum))
		}
	}
}

func getMetricRequests(cw *cloudwatch.CloudWatch) ([]cloudwatch.GetMetricStatisticsRequest, error) {
	metricsRequest := &cloudwatch.ListMetricsRequest{Namespace: "AWS/SQS"}
	response, err := cw.ListMetrics(metricsRequest)
	if err != nil {
		return nil, err
	}

	outRequests := make([]cloudwatch.GetMetricStatisticsRequest, len(response.ListMetricsResult.Metrics))
	for i, metric := range response.ListMetricsResult.Metrics {
		outRequests[i] = cloudwatch.GetMetricStatisticsRequest{
			Dimensions: metric.Dimensions,
			MetricName: metric.MetricName,
			Namespace:  metric.Namespace,
			Period:     60,
			Statistics: []string{"Sum"},
		}
	}

	return outRequests, err
}

func getStatsdBuffer(host string) (*statsd.StatsdBuffer, error) {
	statsdclient := statsd.NewStatsdClient(host, "aws.sqs.")
	if err := statsdclient.CreateSocket(); err != nil {
		log.WithField("error", err).Warn("unable to open socket for statsd")
		return nil, err
	}

	return statsd.NewStatsdBuffer(time.Minute, statsdclient), nil
}
