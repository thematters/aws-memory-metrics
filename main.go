package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/ec2/imds"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch"
	"github.com/aws/aws-sdk-go-v2/service/cloudwatch/types"
	// "github.com/aws/aws-sdk-go-v2/service/s3"
	// "github.com/shirou/gopsutil/v3/mem"
	"golang.org/x/exp/constraints"
)

type pidPair struct {
	name string
	pid  int
}
type pidPairs []pidPair

var pids pidPairs

func (p *pidPairs) String() (value string) {
	return fmt.Sprint(*p)
}

func (p *pidPairs) Set(value string) error {
	fmt.Printf("got value: %#v\n", value)
	for _, ps := range strings.Split(value, ",") {
		fmt.Printf("  part: %#v\n", ps)
		words := strings.Split(ps, "=")
		if len(words) != 2 {
			return errors.New("format of Nextjs=<pid>")
		}
		name := words[0]
		pid, err := strconv.Atoi(words[1])
		if err != nil {
			return err
		}
		if !(pid > 0) {
			return errors.New("format of Nextjs=<pid>; pid must be a positive integer number")
		}
		if err != nil {
			return err
		}
		*p = append(*p, pidPair{name, pid})
	}

	return nil
}

func init() {
	flag.Var(&pids, "pids", "comma-separated list, to append extra NodeServer=<pid>,Nextjs=<pid>")
	flag.Var(&pids, "p", "comma-separated list, to append extra Nextjs=<pid> (shorthand)")
}

func main() {
	flag.Parse()
	fmt.Printf("calling with %d extra pids: %#v\n", len(pids), pids)
	for _, ps := range pids {
		readProcMemInfo(ps.pid)
	}

	svc := NewSvc()

	svc.PutLinuxMemMetrics()

OUTER:
	for {
		now := time.Now()

		select {
		case <-time.After(now.Truncate(1 * time.Minute).Add(1 * time.Minute).Sub(now)):
			break OUTER
		case <-time.After(now.Truncate(5 * time.Second).Add(5 * time.Second).Sub(now)):
			for _, ps := range pids {
				svc.PutProcMemMetrics(ps.name, ps.pid)
			}
		case <-time.After(now.Truncate(15 * time.Second).Add(15 * time.Second).Sub(now)):
			svc.PutLinuxMemMetrics()
		}
	}

	log.Println("runtime out")
}

type memdata struct {
	key   string
	value int
	unit  string
}

type svcClient struct {
	cfg        aws.Config
	imdsClient *imds.Client
	// s3Client   *s3.Client
	cwClient   *cloudwatch.Client
	idDoc      *imds.InstanceIdentityDocument
	dimensions []types.Dimension
	MemTotal   int
}

func NewSvc() *svcClient {
	// Load the Shared AWS Configuration (~/.aws/config)
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithEC2IMDSRegion(),
	)
	if err != nil {
		log.Fatal(err)
	}

	imdsClient := imds.NewFromConfig(cfg)
	response, err := imdsClient.GetRegion(context.TODO(), &imds.GetRegionInput{})
	if err != nil {
		log.Printf("Unable to retrieve the region from the EC2 instance %v\n", err)
	}

	fmt.Printf("region: %v\n", response.Region)
	idDoc, err := imdsClient.GetInstanceIdentityDocument(context.TODO(), &imds.GetInstanceIdentityDocumentInput{})
	if err != nil {
		log.Printf("Unable to retrieve the instance identity from the EC2 instance %v\n", err)
	}
	fmt.Printf("identity: %#v\n", idDoc)

	// Create an Amazon S3 service client
	// s3Client := s3.NewFromConfig(cfg)

	// Create an Amazon Cloudwatch service client
	cwClient := cloudwatch.NewFromConfig(cfg)

	dimensions := []types.Dimension{
		{Name: aws.String("InstanceID"), Value: &idDoc.InstanceID},
		{Name: aws.String("InstanceType"), Value: &idDoc.InstanceType},
		{Name: aws.String("ImageID"), Value: &idDoc.ImageID},
	}

	return &svcClient{cfg, imdsClient, cwClient, &idDoc.InstanceIdentityDocument, dimensions, 0}
}

func (svc *svcClient) PutLinuxMemMetrics() {
	memstat, ts := readMemInfo()
	fmt.Printf("mem stat: %v\n", memstat)

	MemTotal := memstat["MemTotal"].value
	svc.MemTotal = MemTotal

	MemFree := memstat["MemFree"].value
	MemBuffers := memstat["Buffers"].value
	MemCached := memstat["Cached"].value
	MemUsed := MemTotal - MemFree - MemBuffers - MemCached
	MemAvailable := memstat["MemAvailable"].value

	fmt.Printf("memstat: %#v\n", []int{
		MemTotal, MemFree, MemBuffers, MemCached, MemAvailable,
	})

	resMetricsOut, err := svc.cwClient.PutMetricData(context.TODO(), &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("System/Linux"),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String("MemTotal"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(MemTotal)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
			{
				MetricName: aws.String("MemFree"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(MemFree)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
			{
				MetricName: aws.String("MemUsed"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(MemUsed)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
			{
				MetricName: aws.String("MemUsedPercent"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(100.0 * float64(MemUsed) / float64(MemTotal)),
				Unit:       types.StandardUnitPercent,
			},
			{
				MetricName: aws.String("MemAvailable"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(MemAvailable)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("got metrics out: %#v\n", resMetricsOut)
}

func (svc *svcClient) PutProcMemMetrics(prefix string, pid int) {
	memstat, ts := readProcMemInfo(pid)
	fmt.Printf("got proc mem stat: %#v, %v\n", memstat, ts)
	for _, stat := range memstat {
		fmt.Println("  ", stat)
	}

	metricsinput := &cloudwatch.PutMetricDataInput{
		Namespace: aws.String("System/Nodejs"),
		MetricData: []types.MetricDatum{
			{
				MetricName: aws.String(prefix + "VmSize"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(memstat["VmSize"].value)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
			{
				MetricName: aws.String(prefix + "VmRss"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(float64(memstat["VmRSS"].value)),
				Unit:       types.StandardUnitKilobytes, // "Kilobytes",
			},
		},
	}
	if svc.MemTotal > 0 {
		metricsinput.MetricData = append(metricsinput.MetricData,
			types.MetricDatum{
				MetricName: aws.String(prefix + "MemUsedPercent"),
				Dimensions: svc.dimensions,
				Timestamp:  &ts,
				Value:      aws.Float64(100.0 * float64(memstat["VmRSS"].value) / float64(svc.MemTotal)),
				Unit:       types.StandardUnitPercent,
			},
		)
	}
	resMetricsOut, err := svc.cwClient.PutMetricData(context.TODO(), metricsinput)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("got metrics out: %#v\n", resMetricsOut)
}

func readMemInfo() (memstat map[string]memdata, ts time.Time) {
	meminfo, err := os.Open("/proc/meminfo")
	if err != nil {
		log.Fatal(err)
	}
	defer meminfo.Close()

	scanner := bufio.NewScanner(meminfo)

	memstat = map[string]memdata{}
	ts = time.Now()

	// optionally, resize scanner's capacity for lines over 64K, see next example
	for i := 0; scanner.Scan() && i <= 5; i++ { // the first 5 lines are enough
		name, value, unit, err := parseLine(scanner.Text())
		if err != nil {
			log.Println(err)
			continue
		}
		memstat[name] = memdata{name, value, unit}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%#v\n", memstat)

	return
}

var reProcStatusMatched = regexp.MustCompile(`^(VmSize|VmRSS)`)

func readProcMemInfo(pid int) (memstat map[string]memdata, ts time.Time) {
	pidmemfile, err := os.Open(fmt.Sprintf("/proc/%d/status", pid))
	if err != nil {
		log.Fatal(err)
	}
	defer pidmemfile.Close()

	scanner := bufio.NewScanner(pidmemfile)

	memstat = map[string]memdata{}
	ts = time.Now()

	for scanner.Scan() {
		if !reProcStatusMatched.MatchString(scanner.Text()) {
			continue
		}
		name, value, unit, err := parseLine(scanner.Text())
		if err != nil {
			log.Println(err)
			continue
		}
		memstat[name] = memdata{name, value, unit}
	}

	if err := scanner.Err(); err != nil {
		log.Fatal(err)
	}

	fmt.Printf("%#v\n", memstat)

	return
}

var splitter = regexp.MustCompile(`:?\s+`)

func parseLine(line string) (name string, value int, unit string, err error) {
	words := splitter.Split(line, -1)
	if len(words) < 2 {
		err = fmt.Errorf("wrong input line: %q %q\n", line, words)
		return
	}
	name = words[0]
	value, err = strconv.Atoi(words[1])
	if err != nil {
		return
	}
	if len(words) >= 3 {
		unit = words[2]
	}
	return
}

func min[T constraints.Ordered](a, b T) T {
	if a < b {
		return a
	} else {
		return b
	}
	// return b
}
