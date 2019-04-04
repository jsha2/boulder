package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io/ioutil"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/letsencrypt/boulder/cmd"
)

type Config struct {
	// Execution plan parameters
	Plan struct {
		Actions   []string // things to do
		Rate      int64    // requests / s
		RateDelta string   // requests / s^2
		Runtime   string   // how long to run for
	}
	ExternalState   string // path to file to load/save registrations etc to/from
	DontSaveState   bool   // don't save changes to external state
	DirectoryURL    string // ACME server directory URL
	DomainBase      string // base domain name to create authorizations for
	HTTPOneAddr     string // address to listen for http-01 validation requests on
	RealIP          string // value of the Real-IP header to use when bypassing CDN
	CertKeySize     int    // size of the key to use when creating CSRs
	RegEmail        string // email to use in registrations
	Results         string // path to save metrics to
	MaxRegs         int    // maximum number of registrations to create
	MaxNamesPerCert int    // maximum number of names on one certificate/order
}

func main() {
	configPath := flag.String("config", "", "Path to configuration file for load-generator")
	resultsPath := flag.String("results", "", "Path to latency results file")
	rateArg := flag.Int("rate", 0, "")
	runtimeArg := flag.String("runtime", "", "")
	deltaArg := flag.String("delta", "", "")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintf(os.Stderr, "-config argument must not be empty\n")
		os.Exit(1)
	}

	configBytes, err := ioutil.ReadFile(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to read load-generator config file %q: %s\n", *configPath, err)
		os.Exit(1)
	}
	var config Config
	err = json.Unmarshal(configBytes, &config)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to parse load-generator config file: %s\n", err)
		os.Exit(1)
	}

	if *resultsPath != "" {
		config.Results = *resultsPath
	}
	if *rateArg != 0 {
		config.Plan.Rate = int64(*rateArg)
	}
	if *runtimeArg != "" {
		config.Plan.Runtime = *runtimeArg
	}
	if *deltaArg != "" {
		config.Plan.RateDelta = *deltaArg
	}

	s, err := New(
		config.DirectoryURL,
		config.CertKeySize,
		config.DomainBase,
		config.RealIP,
		config.MaxRegs,
		config.MaxNamesPerCert,
		config.Results,
		config.RegEmail,
		config.Plan.Actions,
	)
	cmd.FailOnError(err, "Failed to create load generator")

	if config.ExternalState != "" {
		err = s.Restore(config.ExternalState)
		cmd.FailOnError(err, "Failed to load registration snapshot")
	}

	runtime, err := time.ParseDuration(config.Plan.Runtime)
	cmd.FailOnError(err, "Failed to parse plan runtime")

	var delta *RateDelta
	if config.Plan.RateDelta != "" {
		parts := strings.Split(config.Plan.RateDelta, "/")
		if len(parts) != 2 {
			fmt.Fprintf(os.Stderr, "RateDelta is malformed")
			os.Exit(1)
		}
		rate, err := strconv.Atoi(parts[0])
		cmd.FailOnError(err, "Failed to parse increase portion of RateDelta")
		period, err := time.ParseDuration(parts[1])
		cmd.FailOnError(err, "Failed to parse period portion of RateDelta")
		delta = &RateDelta{Inc: int64(rate), Period: period}
	}

	go cmd.CatchSignals(nil, nil)

	err = s.Run(config.HTTPOneAddr, Plan{
		Runtime: runtime,
		Rate:    config.Plan.Rate,
		Delta:   delta,
	})
	cmd.FailOnError(err, "Failed to run load generator")

	if config.ExternalState != "" && !config.DontSaveState {
		err = s.Snapshot(config.ExternalState)
		cmd.FailOnError(err, "Failed to save registration snapshot")
	}

	fmt.Println("[+] All done, bye bye ^_^")
}
