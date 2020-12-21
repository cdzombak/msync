package main

import (
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

var version = "undefined (dev?)"

// TODO(cdzombak): add a progress bar https://github.com/schollz/progressbar

func usage() {
	fmt.Printf("Usage: %s -from /musicsource -to /musicdest [OPTIONS]\n", filepath.Base(os.Args[0]))
	fmt.Printf("Sync a music library from a source to dest, reencoding files with bitrates over -max-kbps and copying or making symlinks for other files.\n")
	fmt.Printf("Symbolic links in both the source and destination directories are followed.\n\n")
	fmt.Printf("Options:\n")
	flag.PrintDefaults()
	fmt.Printf("\nVersion:\n  msync version %s\n", version)
	fmt.Printf("\nIssues:\n  https://github.com/cdzombak/msync/issues\n")
	fmt.Printf("\nAuthor: Chris Dzombak <https://www.dzombak.com>\n")
}

var (
	fromFlag                     = flag.String("from", "", "Source directory with music library. (Required)")
	toFlag                       = flag.String("to", "", "Destination directory for mirrored/reencoded music library. (Required)")
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
		fmt.Println(err)
		os.Exit(1)
	}
}

func msyncMain() error {
	var fileCreateMode os.FileMode
	if mode, err := strconv.ParseInt(*fileCreateModeFlag, 8, 64); err != nil {
		return errors.New("-file-mode must be an octal value parsable by strconv.ParseInt")
	} else {
		fileCreateMode = os.FileMode(mode)
	}

	sourceRootPath, err := filepath.Abs(*fromFlag)
	if err != nil {
		return err
	}
	destRootPath, err := filepath.Abs(*toFlag)
	if err != nil {
		return err
	}

	fmt.Printf("Scanning source directory (%s) ...\n", sourceRootPath)
	sourceTree, err := MakeMusicTree(sourceRootPath)
	if err != nil {
		return err
	}
	fmt.Printf("Source tree (%s) size is %s\n", sourceRootPath, ByteCountBothStyles(sourceTree.CalculateSize()))

	fmt.Printf("Scanning destination directory (%s) ...\n", destRootPath)
	destTree, err := MakeMusicTree(destRootPath)
	if err != nil {
		return err
	}
	fmt.Printf("Destination tree (%s) size is %s\n", destRootPath, ByteCountBothStyles(destTree.CalculateSize()))

	maxBitrate := *maxBitrateKbpsFlag * 1000

	// we could do this more efficiently by eg. combining remove passes, but I don't care.
	// this makes the program logic easier to follow, and a separate count pass makes reporting progress easier.

	// remove anything from dest that isn't in source:
	fmt.Println("Removing files/directories from the destination directory tree that are missing in source directory tree ...")
	removeCount, err := destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return !sourceTree.HasNodeAtTreePath(n.TreePath)
	}, "item is gone from source directory")
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
		removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
			return !(n.IsDirectory || n.IsMusicFile)
		}, "file is not a music file")
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
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return n.IsMusicFile && n.FileBitrate > maxBitrate
	}, fmt.Sprintf("its bitrate exceeds %d bps", maxBitrate))
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

	// either copy/link or reencode all music files & directories from source that aren't in dest:
	if *makeSymlinksFlag {
		fmt.Printf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be symlinked.\n", *maxBitrateKbpsFlag)
	} else {
		fmt.Printf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be copied.\n", *maxBitrateKbpsFlag)
	}
	err = sourceTree.Walk(func(n *MusicTreeNode) error {
		if n.IsFile && !n.IsMusicFile {
			return nil
		}
		if !destTree.HasNodeAtTreePath(n.TreePath) {
			destPath := strings.Replace(n.FilesystemPath, sourceRootPath, destRootPath, 1)

			// file dest path may be different if reencoding.
			needsTranscode := false
			if n.IsFile && n.IsMusicFile && n.FileBitrate > maxBitrate {
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
				newFileBitrate = maxBitrate
				if !*dryRunFlag {
					if *verboseFlag {
						log.Printf("Transcoding '%s' to '%s' at %s ...", n.FilesystemPath, destPath, ffmpegBitrateStr)
					}
					out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-c:v", "copy", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
					if err != nil {
						_ = os.Remove(destPath)
						return fmt.Errorf("transcode '%s' failed: %w: %s", n.FilesystemPath, err, out)
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
	if err != nil {
		return err
	}
	if *dryRunFlag {
		fmt.Printf("[dry run] Would synchronize %d music files to destination.\n", filesSyncedCount)
	} else {
		fmt.Printf("Synchronized %d music files to destination.\n", filesSyncedCount)
	}

	fmt.Println("Removing empty directories from the destination directory tree ...")
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return n.IsDirectory && len(n.Children) == 0
	}, "directory is empty")
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
