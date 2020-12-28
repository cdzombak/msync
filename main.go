package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"msync/filesize"
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
	dryRunFlag                   = flag.Bool("dry-run", false, "If true, do not modify anything on the filesystem.")
	fileCreateModeFlag           = flag.String("file-mode", "0644", "Octal value specifying mode for copied music files. Must begin with '0' or '0o'.")
	fromFlag                     = flag.String("from", "", "Source directory with music library. (Required)")
	makeSymlinksFlag             = flag.Bool("symlink", false, "If true, make symlinks from the destination to the source for music files below the maximum bitrate. (If not set, make a proper copy of the file.)")
	maxBitrateKbpsFlag           = flag.Int("max-kbps", 192, "Maximum bitrate, in Kbps, for destination music library.")
	printVersion                 = flag.Bool("version", false, "Print version and exit.")
	removeOtherFilesFromDestFlag = flag.Bool("remove-nonmusic-from-dest", false, "If true, remove any non-music files from the destination.")
	toFlag                       = flag.String("to", "", "Destination directory for mirrored/re-encoded music library. (Required)")
	verboseFlag                  = flag.Bool("isVerbose", false, "Log detailed output to stderr. Suppresses progress indicators.")
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
		if *verboseFlag && EchoLogsToStdErr() {
			log.Println(err.Error())
		}
		fmt.Printf("Error: %s", err.Error())
		os.Exit(1)
	}
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

	ctx := WithCLIOut(context.Background())
	if *verboseFlag {
		ctx = WithVerboseCLIOut(ctx)
	}

	CLIOut(ctx).Log(fmt.Sprintf("Scanning source directory (%s) ...", sourceRootPath))
	spinCtx, spinMessage, spinStop := WithCLISpinner(ctx, "...")
	sourceTree, err := MakeMusicTree(spinCtx, sourceRootPath, func(currentPath string) {
		if !CLIOut(spinCtx).HasSpinner() {
			return
		}
		msg := strings.TrimPrefix(currentPath, sourceRootPath)
		msg = strings.TrimPrefix(msg, string(os.PathSeparator))
		msg = strings.SplitN(msg, string(os.PathSeparator), 2)[0]
		spinMessage(msg)
	})
	spinStop()
	if err != nil {
		return err
	}
	CLIOut(ctx).Log(fmt.Sprintf("Source tree (%s) size is %s", sourceRootPath, filesize.ByteCountBothStyles(sourceTree.CalculateSize())))

	CLIOut(ctx).Log(fmt.Sprintf("Scanning destination directory (%s) ...", destRootPath))
	spinCtx, spinMessage, spinStop = WithCLISpinner(ctx, "...")
	destTree, err := MakeMusicTree(spinCtx, destRootPath, func(currentPath string) {
		if !CLIOut(spinCtx).HasSpinner() {
			return
		}
		msg := strings.TrimPrefix(currentPath, destRootPath)
		msg = strings.TrimPrefix(msg, string(os.PathSeparator))
		msg = strings.SplitN(msg, string(os.PathSeparator), 2)[0]
		spinMessage(msg)
	})
	spinStop()
	if err != nil {
		return err
	}
	CLIOut(ctx).Log(fmt.Sprintf("Destination tree (%s) size is %s", destRootPath, filesize.ByteCountBothStyles(destTree.CalculateSize())))

	// ffmpeg's aac encoder produces files a little bit above the target bitrate. so, when transcoding,
	// we tell ffmpeg to target (max bitrate - 1Kbps), and we allow files in the destination dir to be
	// (max bitrate + 2 Kbps). This mostly avoids deleting & re-transcoding the same files over and
	// over across multiple runs with the same configuration.
	targetTranscodeBitrate := *maxBitrateKbpsFlag*1000 - 1000 // target bitrate for encoding
	maxBitrateForDestFiles := targetTranscodeBitrate + 3000
	const transcodeFileExt = ".m4a"

	// we could do this more efficiently by eg. combining remove passes, but I don't care.
	// this makes the program logic easier to follow, and a separate count pass makes reporting progress easier.

	// remove anything from dest that isn't in source:
	CLIOut(ctx).Log("Removing files/directories from the destination directory tree that are missing in source directory tree ...")
	destI := int64(0)
	spinCtx, spinProgress, spinStop := WithCLIProgress(ctx, "checking", destTree.CountNodes())
	removeCount, err := destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		spinProgress(destI)
		return !sourceTree.HasNodeAtTreePath(n.TreePath)
	}, "item is gone from source directory")
	spinStop()
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			CLIOut(ctx).Log(fmt.Sprintf("[dry run] Would remove %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath))
		} else {
			CLIOut(ctx).Log(fmt.Sprintf("Removed %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath))
		}
	} else {
		CLIOut(ctx).Log("0 files/directories affected.")
	}

	if *removeOtherFilesFromDestFlag {
		// remove anything from dest that isn't a music file:
		CLIOut(ctx).Log("Removing non-music files from the destination directory tree ...")
		destI = 0
		spinCtx, spinProgress, spinStop = WithCLIProgress(ctx, "checking", destTree.CountNodes())
		removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
			destI++
			spinProgress(destI)
			return !(n.IsDirectory || n.IsMusicFile)
		}, "file is not a music file")
		spinStop()
		if err != nil {
			return err
		}
		if removeCount > 0 {
			if *dryRunFlag {
				CLIOut(ctx).Log(fmt.Sprintf("[dry run] Would remove %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath))
			} else {
				CLIOut(ctx).Log(fmt.Sprintf("Removed %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath))
			}
		} else {
			CLIOut(ctx).Log("0 files affected.")
		}
	}

	// remove anything from dest that has too-high bitrate:
	CLIOut(ctx).Log(fmt.Sprintf("Removing music files that exceed %d Kbps from the destination directory tree ...", *maxBitrateKbpsFlag))
	destI = 0
	spinCtx, spinProgress, spinStop = WithCLIProgress(ctx, "checking", destTree.CountNodes())
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		spinProgress(destI)
		return n.IsMusicFile && n.FileBitrate > maxBitrateForDestFiles
	}, fmt.Sprintf("its bitrate exceeds %d Kbps", *maxBitrateKbpsFlag))
	spinStop()
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			CLIOut(ctx).Log(fmt.Sprintf("[dry run] Would remove %d files from destination (%s) because their bitrate exceeded %d Kbps", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag))
		} else {
			CLIOut(ctx).Log(fmt.Sprintf("Removed %d files from destination (%s) because their bitrate exceeded %d Kbps", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag))
		}
	} else {
		CLIOut(ctx).Log("0 files affected.")
	}

	ffmpegBitrateStr := strconv.Itoa(*maxBitrateKbpsFlag) + "k"
	didMkdir := make(map[string]bool)
	filesSyncedCount := 0

	// either copy/link or re-encode all music files & directories from source that aren't in dest:
	if *makeSymlinksFlag {
		CLIOut(ctx).Log(fmt.Sprintf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be symlinked.", *maxBitrateKbpsFlag))
	} else {
		CLIOut(ctx).Log(fmt.Sprintf("Syncing music files from source to destination. Files over %d Kbps will be transcoded; others will be copied.", *maxBitrateKbpsFlag))
	}
	sourceI := int64(0)
	spinCtx, spinProgress, spinStop = WithCLIProgress(ctx, "syncing", sourceTree.CountNodes())
	err = sourceTree.Walk(func(n *MusicTreeNode) error {
		sourceI++
		spinProgress(sourceI)
		if n.IsFile && !n.IsMusicFile {
			return nil
		}
		if !destTree.HasNodeAtTreePath(n.TreePath) {
			destPath := strings.Replace(n.FilesystemPath, sourceRootPath, destRootPath, 1)

			// file dest path may be different if re-encoding.
			needsTranscode := false
			if n.IsFile && n.IsMusicFile && n.FileBitrate > maxBitrateForDestFiles {
				needsTranscode = true
				destPath = RemoveExt(destPath) + transcodeFileExt
				CLIOut(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be transcoded to %s", n.FilesystemPath, destPath))
			} else {
				if *makeSymlinksFlag {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be symlinked to %s", n.FilesystemPath, destPath))
				} else {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be copied to %s", n.FilesystemPath, destPath))
				}
			}

			destDirPath := destPath
			if n.IsFile {
				destDirPath = filepath.Dir(destDirPath)
			}
			if _, ok := didMkdir[destDirPath]; !ok { // cut down on logs in isVerbose mode
				if !*dryRunFlag {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("mkdir -p '%s'", destDirPath))
					err := os.MkdirAll(destDirPath, destTree.Mode)
					if err != nil {
						return err
					}
				} else {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("[dry run] Would mkdir -p '%s'", destDirPath))
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
				newFileBitrate = targetTranscodeBitrate
				if !*dryRunFlag {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("Transcoding '%s' to '%s' at %s ...", n.FilesystemPath, destPath, ffmpegBitrateStr))
					// try without discarding album art; and if that fails try once more discarding video entirely:
					// TODO(cdzombak): this is insanely ugly. refactor: https://github.com/cdzombak/msync/issues/5
					out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-c:v", "copy", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
					if err != nil {
						_ = os.Remove(destPath)
						CLIOut(spinCtx).Verbose(fmt.Sprintf("Transcoding of '%s' failed. Trying again without video. Error was: %s %s", n.FilesystemPath, out, err))
						out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-vn", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
						if err != nil {
							_ = os.Remove(destPath)
							return fmt.Errorf("transcode '%s' failed: %w: %s", n.FilesystemPath, err, out)
						}
					}
				} else {
					CLIOut(spinCtx).Verbose(fmt.Sprintf("[dry run] Would transcode '%s' to '%s' at %s", n.FilesystemPath, destPath, ffmpegBitrateStr))
				}
			} else {
				newFileBitrate = n.FileBitrate
				if *makeSymlinksFlag {
					if !*dryRunFlag {
						CLIOut(spinCtx).Verbose(fmt.Sprintf("Symlinking '%s' to '%s'", destPath, n.FilesystemPath))
						err := os.Symlink(n.FilesystemPath, destPath)
						if err != nil {
							return fmt.Errorf("failed to symlink '%s' to '%s': %w", destPath, n.FilesystemPath, err)
						}
					} else {
						CLIOut(spinCtx).Verbose(fmt.Sprintf("[dry run] Would symlink '%s' to '%s'", destPath, n.FilesystemPath))
					}
				} else {
					if !*dryRunFlag {
						CLIOut(spinCtx).Verbose(fmt.Sprintf("Copying '%s' to '%s'", n.FilesystemPath, destPath))
						err := CopyFile(n.FilesystemPath, destPath, fileCreateMode)
						if err != nil {
							return fmt.Errorf("failed to copy '%s' to '%s': %w", n.FilesystemPath, destPath, err)
						}
					} else {
						CLIOut(spinCtx).Verbose(fmt.Sprintf("[dry run] Would copy '%s' to '%s'", n.FilesystemPath, destPath))
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
	spinStop()
	if err != nil {
		return err
	}
	if *dryRunFlag {
		CLIOut(ctx).Log(fmt.Sprintf("[dry run] Would synchronize %d music files to destination.", filesSyncedCount))
	} else {
		CLIOut(ctx).Log(fmt.Sprintf("Synchronized %d music files to destination.", filesSyncedCount))
	}

	CLIOut(ctx).Log("Removing empty directories from the destination directory tree ...")
	destI = 0
	spinCtx, spinProgress, spinStop = WithCLIProgress(ctx, "checking", destTree.CountNodes())
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		destI++
		spinProgress(destI)
		return n.IsDirectory && len(n.Children) == 0
	}, "directory is empty")
	spinStop()
	if err != nil {
		return err
	}
	if removeCount > 0 {
		if *dryRunFlag {
			CLIOut(ctx).Log(fmt.Sprintf("[dry run] Would remove %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath))
		} else {
			CLIOut(ctx).Log(fmt.Sprintf("Removed %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath))
		}
	} else {
		CLIOut(ctx).Log("0 directories affected.")
	}

	CLIOut(ctx).Log("")
	symlinkPart := ""
	if *makeSymlinksFlag {
		symlinkPart = " (after resolving symlinks created during sync)"
	}
	CLIOut(ctx).Log(fmt.Sprintf("Destination library size is now %s%s.", filesize.ByteCountBothStyles(destTree.CalculateSize()), symlinkPart))
	CLIOut(ctx).Log("Completed!")

	return nil
}
