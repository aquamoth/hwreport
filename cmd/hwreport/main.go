package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"time"

	"specreport/internal/collector"
	"specreport/internal/output"
	versioninfo "specreport/internal/version"
)

func main() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = func() {
		info := versioninfo.Get()
		fmt.Fprintf(flag.CommandLine.Output(), "hwreport %s\n\n", info.String())
		fmt.Fprintln(flag.CommandLine.Output(), "Collect a hardware inventory report from a Windows computer and write it to JSON.")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Usage:")
		fmt.Fprintln(flag.CommandLine.Output(), "  hwreport [options]")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Options:")
		flag.PrintDefaults()
		fmt.Fprintln(flag.CommandLine.Output(), "  -h, -help")
		fmt.Fprintln(flag.CommandLine.Output(), "    \tShow this help and exit")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Developed by Trustfall AB")
		fmt.Fprintln(flag.CommandLine.Output(), "https://trustfall.se")
		fmt.Fprintln(flag.CommandLine.Output(), "Copyright (c) Trustfall AB")
	}

	outFlag := flag.String("out", "", "Output JSON file path")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(versioninfo.Get().String())
		return
	}

	report, err := collector.Collect()
	if err != nil {
		fmt.Fprintf(os.Stderr, "collect report: %v\n", err)
		os.Exit(1)
	}

	outputPath, err := output.ResolvePath(*outFlag, report.Hostname, time.Now())
	if err != nil {
		fmt.Fprintf(os.Stderr, "resolve output path: %v\n", err)
		os.Exit(1)
	}

	if err := output.WriteJSON(outputPath, report); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(outputPath)
}
