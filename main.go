package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/briandowns/spinner"
)

var version = "undefined (dev?)"

func usage() {
	fmt.Printf("Usage: %s -from /musicsource -to /musicdest [OPTIONS]\n", filepath.Base(os.Args[0]))
	fmt.Printf("Sync a music library from a source to dest, re-encoding files with bitrates over -max-kbps and copying or making symlinks for other files.\n")
	fmt.Printf("Symbolic links in both the source and destination directories are followed.\n\n")
	fmt.Printf("Options:\n")
	flag.PrintDefaults()
	fmt.Printf("\nVersion:\n  msync version %s\n", version)
	fmt.Printf("\nIssues:\n  https://github.com/cdzombak/msync/issues\n")
	fmt.Printf("\nAuthor: Chris Dzombak <https://www.dzombak.com>\n")
}

var (
	fromFlag                     = flag.String("from", "", "Source directory with music library. (Required)")
	toFlag                       = flag.String("to", "", "Destination directory for mirrored/re-encoded music library. (Required)")
	maxBitrateKbpsFlag           = flag.Int("max-kbps", 192, "Maximum bitrate, in Kbps, for destination music library.")
	dryRunFlag                   = flag.Bool("dry-run", false, "If true, do not modify anything on the filesystem.")
	removeOtherFilesFromDestFlag = flag.Bool("remove-nonmusic-from-dest", false, "If true, remove any non-music files from the destination.")
	makeSymlinksFlag             = flag.Bool("symlink", false, "If true, make symlinks from the destination to the source for music files below the maximum bitrate. (If not set, make a proper copy of the file.)")
	verboseFlag                  = flag.Bool("verbose", false, "Log detailed output to stderr. Suppresses progress bars.")
	printVersion                 = flag.Bool("version", false, "Print version and exit.")
	fileCreateModeFlag           = flag.String("file-mode", "0644", "Octal value specifying mode for copied music files. Must begin with '0' or '0o'.")
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
		fmt.Printf("Error: %s", err.Error())
		os.Exit(1)
	}
}

func useProgressIndicators() bool {
	return !*verboseFlag && IsStdoutInteractive()
}

func msyncMain() error {
	mode, err := strconv.ParseInt(*fileCreateModeFlag, 8, 64)
	if err != nil {
		return errors.New("-file-mode must be an octal value parsable by strconv.ParseInt")
	}
	fileCreateMode := os.FileMode(mode)

	sourceRootPath, err := filepath.Abs(*fromFlag)
	if err != nil {
		return err
	}
	destRootPath, err := filepath.Abs(*toFlag)
	if err != nil {
		return err
	}

	var spin *spinner.Spinner
	spinFreq := 50 * time.Millisecond

	fmt.Printf("Scanning source directory (%s) ...\n", sourceRootPath)
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	sourceTree, warnings, err := MakeMusicTree(sourceRootPath, func(currentPath string) {
		if spin == nil {
			return
		}
		suffix := strings.TrimPrefix(currentPath, sourceRootPath)
		suffix = strings.TrimPrefix(suffix, string(os.PathSeparator))
		suffix = strings.SplitN(suffix, string(os.PathSeparator), 2)[0]
		spin.Suffix = " " + suffix
	})
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	LogWarnings(warnings)
	fmt.Printf("Source tree (%s) size is %s\n", sourceRootPath, ByteCountBothStyles(sourceTree.CalculateSize()))

	fmt.Printf("Scanning destination directory (%s) ...\n", destRootPath)
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	destTree, warnings, err := MakeMusicTree(destRootPath, func(currentPath string) {
		if spin == nil {
			return
		}
		suffix := strings.TrimPrefix(currentPath, destRootPath)
		suffix = strings.TrimPrefix(suffix, string(os.PathSeparator))
		suffix = strings.SplitN(suffix, string(os.PathSeparator), 2)[0]
		spin.Suffix = " " + suffix
	})
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	LogWarnings(warnings)
	fmt.Printf("Destination tree (%s) size is %s\n", destRootPath, ByteCountBothStyles(destTree.CalculateSize()))

	targetBitrate := *maxBitrateKbpsFlag * 1000 // target bitrate for encoding
	maxBitrateForDestFiles := targetBitrate + 1000 // ffmpeg's aac encoder produces files a little bit above the target bitrate

	// we could do this more efficiently by eg. combining remove passes, but I don't care.
	// this makes the program logic easier to follow, and a separate count pass makes reporting progress easier.

	// remove anything from dest that isn't in source:
	fmt.Println("Removing files/directories from the destination directory tree that are missing in source directory tree ...")
	destI := 0
	destCount := destTree.CountNodes()
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	removeCount, err := destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		if spin != nil {
			spin.Suffix = fmt.Sprintf(" checking %d / %d (%.f%%)", destI, destCount, math.Round(100*float64(destI)/float64(destCount)))
		}
		return !sourceTree.HasNodeAtTreePath(n.TreePath)
	}, "item is gone from source directory")
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			fmt.Printf("[dry run] Would remove %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)\n", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath)
		} else {
			fmt.Printf("Removed %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)\n", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath)
		}
	} else {
		fmt.Println("0 files/directories affected.")
	}

	if *removeOtherFilesFromDestFlag {
		// remove anything from dest that isn't a music file:
		fmt.Println("Removing non-music files from the destination directory tree ...")
		destI = 0
		destCount = destTree.CountNodes()
		if useProgressIndicators() {
			spin = spinner.New(spinner.CharSets[14], spinFreq)
			spin.HideCursor = true
			spin.Start()
		}
		removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
			destI++
			if spin != nil {
				spin.Suffix = fmt.Sprintf(" checking %d / %d (%.f%%)", destI, destCount, math.Round(100*float64(destI)/float64(destCount)))
			}
			return !(n.IsDirectory || n.IsMusicFile)
		}, "file is not a music file")
		if spin != nil {
			spin.HideCursor = false
			spin.Stop()
			spin = nil
		}
		if err != nil {
			return err
		}
		if removeCount > 0 {
			if *dryRunFlag {
				fmt.Printf("[dry run] Would remove %d non-music files from destination (%s)\n", removeCount, destTree.FilesystemPath)
			} else {
				fmt.Printf("Removed %d non-music files from destination (%s)\n", removeCount, destTree.FilesystemPath)
			}
		} else {
			fmt.Println("0 files affected.")
		}
	}

	// remove anything from dest that has too-high bitrate:
	fmt.Printf("Removing music files that exceed %d Kbps from the destination directory tree ...\n", *maxBitrateKbpsFlag)
	destI = 0
	destCount = destTree.CountNodes()
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		if spin != nil {
			spin.Suffix = fmt.Sprintf(" checking %d / %d (%.f%%)", destI, destCount, math.Round(100*float64(destI)/float64(destCount)))
		}
		return n.IsMusicFile && n.FileBitrate > maxBitrateForDestFiles
	}, fmt.Sprintf("its bitrate exceeds %d Kbps", *maxBitrateKbpsFlag))
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			fmt.Printf("[dry run] Would remove %d files from destination (%s) because their bitrate exceeded %d Kbps\n", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag)
		} else {
			fmt.Printf("Removed %d files from destination (%s) because their bitrate exceeded %d Kbps\n", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag)
		}
	} else {
		fmt.Println("0 files affected.")
	}

	ffmpegBitrateStr := strconv.Itoa(*maxBitrateKbpsFlag) + "k"
	didMkdir := make(map[string]bool)
	filesSyncedCount := 0

	// either copy/link or re-encode all music files & directories from source that aren't in dest:
	if *makeSymlinksFlag {
		fmt.Printf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be symlinked.\n", *maxBitrateKbpsFlag)
	} else {
		fmt.Printf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be copied.\n", *maxBitrateKbpsFlag)
	}
	sourceI := 0
	sourceCount := sourceTree.CountNodes()
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	err = sourceTree.Walk(func(n *MusicTreeNode) error {
		sourceI++
		if spin != nil {
			spin.Suffix = fmt.Sprintf(" syncing %d / %d (%.f%%)", sourceI, sourceCount, math.Round(100*float64(sourceI)/float64(sourceCount)))
		}
		if n.IsFile && !n.IsMusicFile {
			return nil
		}
		if !destTree.HasNodeAtTreePath(n.TreePath) {
			destPath := strings.Replace(n.FilesystemPath, sourceRootPath, destRootPath, 1)

			// file dest path may be different if re-encoding.
			needsTranscode := false
			if n.IsFile && n.IsMusicFile && n.FileBitrate > maxBitrateForDestFiles {
				needsTranscode = true
				destPath = removeExt(destPath) + transcodedFileExt
				if *verboseFlag {
					log.Printf("%s is missing from destination; will be transcoded to %s", n.FilesystemPath, destPath)
				}
			} else if *verboseFlag {
				if *makeSymlinksFlag {
					log.Printf("%s is missing from destination; will be symlinked to %s", n.FilesystemPath, destPath)
				} else {
					log.Printf("%s is missing from destination; will be copied to %s", n.FilesystemPath, destPath)
				}
			}

			destDirPath := destPath
			if n.IsFile {
				destDirPath = filepath.Dir(destDirPath)
			}
			if _, ok := didMkdir[destDirPath]; !ok { // cut down on logs in verbose mode
				if !*dryRunFlag {
					if *verboseFlag {
						log.Printf("mkdir -p '%s'", destDirPath)
					}
					err := os.MkdirAll(destDirPath, destTree.Mode)
					if err != nil {
						return err
					}
				} else if *verboseFlag {
					log.Printf("[dry run] Would mkdir -p '%s'", destDirPath)
				}
				didMkdir[destDirPath] = true
			}

			// insert new dir node(s) in dest tree as needed:
			relativeDirPathUnderDestRoot := strings.TrimPrefix(destDirPath, destRootPath)
			relativeDirPathUnderDestRoot = strings.TrimPrefix(relativeDirPathUnderDestRoot, "/")
			destDirParts := strings.Split(relativeDirPathUnderDestRoot, string(os.PathSeparator))
			destDirNode := destTree
			for _, part := range destDirParts {
				if destDirNode.IsFile || destDirNode.Children == nil {
					panic("file node cannot have children")
				}
				normalizedPart := normalizeFileNameForComparing(part)
				if !destDirNode.HasNodeAtTreePath([]string{normalizedPart}) {
					destDirNode.Children[normalizedPart] = &MusicTreeNode{
						TreePath:           append(destDirNode.TreePath, normalizedPart),
						FilesystemPath:     filepath.Join(destDirNode.FilesystemPath, part),
						IsDirectory:        true,
						BaseName:           part,
						BaseNameNormalized: normalizedPart,
						Mode:               destTree.Mode,
						Children:           make(map[string]*MusicTreeNode),
					}
					destDirNode = destDirNode.Children[normalizedPart]
				} else {
					destDirNode = destDirNode.NodeAtTreePath([]string{normalizedPart})
				}
			}

			if n.IsDirectory {
				return nil
			}

			var newFileSize int64
			var newFileMode os.FileMode
			newFileBitrate := 0
			if needsTranscode {
				newFileBitrate = targetBitrate
				if !*dryRunFlag {
					if *verboseFlag {
						log.Printf("Transcoding '%s' to '%s' at %s ...", n.FilesystemPath, destPath, ffmpegBitrateStr)
					}
					// try without discarding album art; and if that fails try once more discarding video entirely:
					// TODO(cdzombak): this is insanely ugly. refactor: https://github.com/cdzombak/msync/issues/5
					out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-c:v", "copy", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
					if err != nil {
						_ = os.Remove(destPath)
						if *verboseFlag {
							log.Printf("Transcoding of '%s' failed. Trying again without video. Error was: %s %s", n.FilesystemPath, out, err)
						}
						out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-vn", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
						if err != nil {
							_ = os.Remove(destPath)
							return fmt.Errorf("transcode '%s' failed: %w: %s", n.FilesystemPath, err, out)
						}
					}
				} else if *verboseFlag {
					log.Printf("[dry run] Would transcode '%s' to '%s' at %s", n.FilesystemPath, destPath, ffmpegBitrateStr)
				}
			} else {
				newFileBitrate = n.FileBitrate
				if *makeSymlinksFlag {
					if !*dryRunFlag {
						if *verboseFlag {
							log.Printf("Symlinking '%s' to '%s'", destPath, n.FilesystemPath)
						}
						err := os.Symlink(n.FilesystemPath, destPath)
						if err != nil {
							return fmt.Errorf("failed to symlink '%s' to '%s': %w", destPath, n.FilesystemPath, err)
						}
					} else if *verboseFlag {
						log.Printf("[dry run] Would symlink '%s' to '%s'", destPath, n.FilesystemPath)
					}
				} else {
					if !*dryRunFlag {
						if *verboseFlag {
							log.Printf("Copying '%s' to '%s'", n.FilesystemPath, destPath)
						}
						err := CopyFile(n.FilesystemPath, destPath, fileCreateMode)
						if err != nil {
							return fmt.Errorf("failed to copy '%s' to '%s': %w", n.FilesystemPath, destPath, err)
						}
					} else if *verboseFlag {
						log.Printf("[dry run] Would copy '%s' to '%s'", n.FilesystemPath, destPath)
					}
				}
			}

			if !*dryRunFlag {
				info, err := os.Stat(destPath)
				if err != nil {
					return err
				}
				newFileSize = info.Size()
				newFileMode = info.Mode()
			} else {
				newFileSize = n.FileSize
				newFileMode = n.Mode
			}

			var destDirPartsNormalized []string
			for _, v := range destDirParts {
				destDirPartsNormalized = append(destDirPartsNormalized, normalizeFileNameForComparing(v))
			}
			destFileName := filepath.Base(destPath)
			destFileNameNormalized := normalizeFileNameForComparing(destFileName)
			destDirNode.Children[destFileNameNormalized] = &MusicTreeNode{
				TreePath:           append(destDirPartsNormalized, destFileNameNormalized),
				FilesystemPath:     destPath,
				IsFile:             true,
				IsMusicFile:        true,
				BaseName:           destFileName,
				BaseNameNormalized: destFileNameNormalized,
				FileSize:           newFileSize,
				FileBitrate:        newFileBitrate,
				Mode:               newFileMode,
			}

			filesSyncedCount++
		}
		return nil
	})
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	if *dryRunFlag {
		fmt.Printf("[dry run] Would synchronize %d music files to destination.\n", filesSyncedCount)
	} else {
		fmt.Printf("Synchronized %d music files to destination.\n", filesSyncedCount)
	}

	fmt.Println("Removing empty directories from the destination directory tree ...")
	destI = 0
	destCount = destTree.CountNodes()
	if useProgressIndicators() {
		spin = spinner.New(spinner.CharSets[14], spinFreq)
		spin.HideCursor = true
		spin.Start()
	}
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		if spin != nil {
			spin.Suffix = fmt.Sprintf(" checking %d / %d (%.f%%)", destI, destCount, math.Round(100*float64(destI)/float64(destCount)))
		}
		return n.IsDirectory && len(n.Children) == 0
	}, "directory is empty")
	if spin != nil {
		spin.HideCursor = false
		spin.Stop()
		spin = nil
	}
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			fmt.Printf("[dry run] Would remove %d empty directories from destination (%s)\n", removeCount, destTree.FilesystemPath)
		} else {
			fmt.Printf("Removed %d empty directories from destination (%s)\n", removeCount, destTree.FilesystemPath)
		}
	} else {
		fmt.Println("0 directories affected.")
	}

	fmt.Printf("Destination library size is now %s.\n", ByteCountBothStyles(destTree.CalculateSize()))
	fmt.Println("Completed!")

	return nil
}
