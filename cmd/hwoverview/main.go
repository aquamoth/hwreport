package main

import (
	"flag"
	"fmt"
	"os"
	"time"

	"specreport/internal/overview"
	versioninfo "specreport/internal/version"
)

func main() {
	flag.CommandLine.SetOutput(os.Stdout)
	flag.Usage = func() {
		info := versioninfo.Get()
		fmt.Fprintf(flag.CommandLine.Output(), "hwoverview %s\n\n", info.String())
		fmt.Fprintln(flag.CommandLine.Output(), "Aggregate hwreport JSON files into an HTML overview table.")
		fmt.Fprintln(flag.CommandLine.Output())
		fmt.Fprintln(flag.CommandLine.Output(), "Usage:")
		fmt.Fprintln(flag.CommandLine.Output(), "  hwoverview [options]")
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

	inDir := flag.String("in", ".", "Directory containing hwreport JSON files")
	outPath := flag.String("out", "", "Output HTML file path")
	versionFlag := flag.Bool("version", false, "Print version information and exit")
	flag.Parse()

	if *versionFlag {
		fmt.Println(versioninfo.Get().String())
		return
	}

	options := overview.Options{
		InputDir:   *inDir,
		OutputPath: *outPath,
		Now:        time.Now(),
		Version:    versioninfo.Get().String(),
	}

	result, err := overview.Generate(options)
	if err != nil {
		fmt.Fprintf(os.Stderr, "generate overview: %v\n", err)
		os.Exit(1)
	}

	fmt.Println(result.OutputPath)
}
