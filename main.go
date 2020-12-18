package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

var version = "undefined"

// TODO(cdzombak): support copying instead of symlinks

func usage() {
	fmt.Printf("Usage: %s -from /musicsource -to /musicdest [OPTIONS]\n", filepath.Base(os.Args[0]))
	fmt.Printf("Sync a music library from a source to dest, reencoding files with bitrates over -max-kbps and making symlinks for other files.\n")
	fmt.Printf("Symbolic links in both the source and destination directories are followed.\n\n")
	fmt.Printf("Options:\n")
	flag.PrintDefaults()
	fmt.Printf("\nVersion:\n  msync version %s\n", version)
	fmt.Printf("\nIssues:\n  https://github.com/cdzombak/msync/issues\n")
	fmt.Printf("\nAuthor: Chris Dzombak <https://www.dzombak.com>\n")
}

var (
	fromFlag       = flag.String("from", "", "Source directory with music library. (Required)")
	toFlag         = flag.String("to", "", "Destination directory for mirrored/reencoded music library. (Required)")
	maxBitrateKbpsFlag = flag.Int("max-kbps", 192, "Maximum bitrate, in Kbps, for destination music library.")
	dryRunFlag     = flag.Bool("dry-run", false, "If true, do not modify anything on the filesystem.")
	verboseFlag    = flag.Bool("verbose", false, "Log detailed output to stderr.")
	printVersion   = flag.Bool("version", false, "Print version and exit.")
)

func main() {
	flag.Usage = usage
	flag.Parse()

	if *printVersion {
		fmt.Println(version)
		os.Exit(0)
	}
	if *fromFlag == "" || *toFlag == "" {
		flag.Usage()
		os.Exit(1)
	}

	if err := msyncMain(); err != nil {
		fmt.Printf("failed: %s", err)
		os.Exit(1)
	}
}

func msyncMain() error {
	started := time.Now()
	sourceTree, err := MakeMusicTreeNode(*fromFlag)
	if err != nil {
		return err
	}
	elapsed := time.Now().Sub(started).Round(time.Second)
	fmt.Printf("built tree for source in %s; size is %d bytes\n", elapsed.String(), sourceTree.CalculateSizeRecursive())

	started = time.Now()
	destTree, err := MakeMusicTreeNode(*toFlag)
	if err != nil {
		return err
	}
	elapsed = time.Now().Sub(started).Round(time.Second)
	fmt.Printf("built tree for dest in %s; size is %d bytes\n", elapsed.String(), destTree.CalculateSizeRecursive())

	// TODO(cdzombak): plan: remove anything from dest that has too-large bitrate
	// TODO(cdzombak): plan: remove anything from dest that isn't in source
	// TODO(cdzombak): plan: either link or reencode everything from source that isn't in dest

	// TODO(cdzombak): print full plan iff -verbose

	// TODO(cdzombak): execute plan

	// TODO(cdzombak): print total dest library size

	return nil
}
