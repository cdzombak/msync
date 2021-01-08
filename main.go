package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io/ioutil"
	"log"
	"math"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"

	"msync/cli"
	"msync/dzutil"
	"msync/filesize"

	"github.com/Bios-Marcel/wastebasket"
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
	verboseFlag                  = flag.Bool("verbose", false, "Log detailed output to stderr. Suppresses progress indicators.")
	askTrashPermissionFlag       = flag.Bool("ask-trash-permission", false, "Try to remove a temporary file to the Trash before starting the sync process. This will cause macOS to display the requisite automation permission dialog immediately.")
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
		if *verboseFlag && cli.EchoLogsToStdErr() {
			log.Println(err.Error())
		}
		fmt.Printf("Error: %s\n", err.Error())
		os.Exit(1)
	}
}

type transcodeOp struct {
	source *MusicTreeNode
	dest   *MusicTreeNode
}

func msyncMain() error {
	mode, err := strconv.ParseInt(*fileCreateModeFlag, 8, 64)
	if err != nil {
		return errors.New("-file-mode must be an octal value parsable by strconv.ParseInt")
	}
	fileCreateMode := os.FileMode(mode)

	if *askTrashPermissionFlag {
		file, err := ioutil.TempFile("/tmp", "msync")
		if err != nil {
			return err
		}
		file.Close()
		if err := wastebasket.Trash(file.Name()); err != nil {
			return err
		}
	}

	sourceRootPath, err := filepath.Abs(*fromFlag)
	if err != nil {
		return err
	}
	destRootPath, err := filepath.Abs(*toFlag)
	if err != nil {
		return err
	}

	ctx := cli.WithCLIOut(context.Background())
	if *verboseFlag {
		ctx = cli.WithVerboseOut(ctx)
	}

	quitSig := make(chan os.Signal, 1)
	signal.Notify(quitSig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	go func() {
		<-quitSig
		cli.ShowTerminalCursor()
		os.Exit(0)
	}()

	cli.Out(ctx).Log(fmt.Sprintf("Scanning source directory (%s) ...", sourceRootPath))
	spinCtx, _, spinStop := cli.WithSpinner(ctx, "scanning")
	sourceTree, err := MakeMusicTree(spinCtx, sourceRootPath)
	spinStop()
	if err != nil {
		return err
	}
	cli.Out(ctx).Log(fmt.Sprintf("Source tree (%s) size is %s", sourceRootPath, filesize.ByteCountBothStyles(sourceTree.CalculateSize())))

	cli.Out(ctx).Log(fmt.Sprintf("Scanning destination directory (%s) ...", destRootPath))
	spinCtx, _, spinStop = cli.WithSpinner(ctx, "scanning")
	destTree, err := MakeMusicTree(spinCtx, destRootPath)
	spinStop()
	if err != nil {
		return err
	}
	cli.Out(ctx).Log(fmt.Sprintf("Destination tree (%s) size is %s", destRootPath, filesize.ByteCountBothStyles(destTree.CalculateSize())))

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
	cli.Out(ctx).Log("Removing files/directories from the destination directory tree that are missing in source directory tree ...")
	destI := int64(0)
	spinCtx, spinProgress, spinStop := cli.WithProgress(ctx, "checking", destTree.CountNodes())
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
			cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would remove %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath))
		} else {
			cli.Out(ctx).Log(fmt.Sprintf("Removed %d files/directories from destination (%s) because the equivalent item is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath))
		}
	} else {
		cli.Out(ctx).Log("0 files/directories affected.")
	}

	if *removeOtherFilesFromDestFlag {
		// remove anything from dest that isn't a music file:
		cli.Out(ctx).Log("Removing non-music files from the destination directory tree ...")
		destI = 0
		spinCtx, spinProgress, spinStop = cli.WithProgress(ctx, "checking", destTree.CountNodes())
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
				cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would remove %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath))
			} else {
				cli.Out(ctx).Log(fmt.Sprintf("Removed %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath))
			}
		} else {
			cli.Out(ctx).Log("0 files affected.")
		}
	}

	// remove anything from dest that has too-high bitrate:
	cli.Out(ctx).Log(fmt.Sprintf("Removing music files that exceed %d Kbps from the destination directory tree ...", *maxBitrateKbpsFlag))
	destI = 0
	spinCtx, spinProgress, spinStop = cli.WithProgress(ctx, "checking", destTree.CountNodes())
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
			cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would remove %d files from destination (%s) because their bitrate exceeded %d Kbps", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag))
		} else {
			cli.Out(ctx).Log(fmt.Sprintf("Removed %d files from destination (%s) because their bitrate exceeded %d Kbps", removeCount, destTree.FilesystemPath, *maxBitrateKbpsFlag))
		}
	} else {
		cli.Out(ctx).Log("0 files affected.")
	}

	ffmpegBitrateStr := strconv.Itoa(*maxBitrateKbpsFlag) + "k"
	didMkdir := make(map[string]bool)
	filesSyncedCount := 0
	var transcodeQueue []transcodeOp

	// either copy/link or re-encode all music files & directories from source that aren't in dest:
	if *makeSymlinksFlag {
		cli.Out(ctx).Log(fmt.Sprintf("Syncing music files from source to destination. Files over %d Kbps will be queued for transcoding; others will be symlinked.", *maxBitrateKbpsFlag))
	} else {
		cli.Out(ctx).Log(fmt.Sprintf("Syncing music files from source to destination. Files over %d Kbps will be queued for transcoding; others will be copied.", *maxBitrateKbpsFlag))
	}
	sourceI := int64(0)
	spinCtx, spinProgress, spinStop = cli.WithProgress(ctx, "syncing", sourceTree.CountNodes())
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
				destPath = dzutil.RemoveExt(destPath) + transcodeFileExt
				cli.Out(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be transcoded to %s", n.FilesystemPath, destPath))
			} else {
				if *makeSymlinksFlag {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be symlinked to %s", n.FilesystemPath, destPath))
				} else {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("%s is missing from destination; will be copied to %s", n.FilesystemPath, destPath))
				}
			}

			destDirPath := destPath
			if n.IsFile {
				destDirPath = filepath.Dir(destDirPath)
			}
			if _, ok := didMkdir[destDirPath]; !ok { // cut down on logs in isVerbose mode
				if !*dryRunFlag {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("mkdir -p '%s'", destDirPath))
					err := os.MkdirAll(destDirPath, destTree.Mode)
					if err != nil {
						return err
					}
				} else {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("[dry run] Would mkdir -p '%s'", destDirPath))
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

			var destDirPartsNormalized []string
			for _, v := range destDirParts {
				destDirPartsNormalized = append(destDirPartsNormalized, normalizeFileNameForComparing(v))
			}
			destFileName := filepath.Base(destPath)
			destFileNameNormalized := normalizeFileNameForComparing(destFileName)

			if needsTranscode {
				cli.Out(spinCtx).Verbose(fmt.Sprintf("Queueing transcode of '%s' to '%s' at %s ...", n.FilesystemPath, destPath, ffmpegBitrateStr))
				destNode := &MusicTreeNode{
					TreePath:           append(destDirPartsNormalized, destFileNameNormalized),
					FilesystemPath:     destPath,
					IsFile:             true,
					IsMusicFile:        true,
					BaseName:           destFileName,
					BaseNameNormalized: destFileNameNormalized,
					FileBitrate:        targetTranscodeBitrate,
				}
				destDirNode.Children[destFileNameNormalized] = destNode
				transcodeQueue = append(transcodeQueue, transcodeOp{
					source: n,
					dest:   destNode,
				})
			} else {
				if *makeSymlinksFlag {
					if !*dryRunFlag {
						cli.Out(spinCtx).Verbose(fmt.Sprintf("Symlinking '%s' to '%s'", destPath, n.FilesystemPath))
						err := os.Symlink(n.FilesystemPath, destPath)
						if err != nil {
							return fmt.Errorf("failed to symlink '%s' to '%s': %w", destPath, n.FilesystemPath, err)
						}
					} else {
						cli.Out(spinCtx).Verbose(fmt.Sprintf("[dry run] Would symlink '%s' to '%s'", destPath, n.FilesystemPath))
					}
				} else {
					if !*dryRunFlag {
						cli.Out(spinCtx).Verbose(fmt.Sprintf("Copying '%s' to '%s'", n.FilesystemPath, destPath))
						err := dzutil.CopyFile(n.FilesystemPath, destPath, fileCreateMode)
						if err != nil {
							return fmt.Errorf("failed to copy '%s' to '%s': %w", n.FilesystemPath, destPath, err)
						}
					} else {
						cli.Out(spinCtx).Verbose(fmt.Sprintf("[dry run] Would copy '%s' to '%s'", n.FilesystemPath, destPath))
					}
				}
				var newFileSize int64
				var newFileMode os.FileMode
				if !*dryRunFlag {
					info, err := os.Stat(destPath)
					if err != nil {
						return err
					}
					newFileSize = info.Size()
					newFileMode = info.Mode()
				} else {
					newFileSize = n.FileSize
					newFileMode = fileCreateMode
				}
				destDirNode.Children[destFileNameNormalized] = &MusicTreeNode{
					TreePath:           append(destDirPartsNormalized, destFileNameNormalized),
					FilesystemPath:     destPath,
					IsFile:             true,
					IsMusicFile:        true,
					BaseName:           destFileName,
					BaseNameNormalized: destFileNameNormalized,
					FileSize:           newFileSize,
					FileBitrate:        n.FileBitrate,
					Mode:               newFileMode,
				}
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
		cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would synchronize or enqueue %d music files.", filesSyncedCount))
	} else {
		cli.Out(ctx).Log(fmt.Sprintf("Synchronized or enqueued %d music files.", filesSyncedCount))
	}

	cli.Out(ctx).Log(fmt.Sprintf("Transcoding %d music files from source to destination ...", len(transcodeQueue)))
	spinCtx, spinProgress, spinStop = cli.WithProgress(ctx, "transcoding", int64(len(transcodeQueue)))
	cpuCount := runtime.NumCPU()
	cli.Out(ctx).Verbose(fmt.Sprintf("using %d parallel transcode tasks", cpuCount))
	currentIdx := -1
	var transcodeQueueLock sync.Mutex
	var wg sync.WaitGroup
	for i := 0; i <= cpuCount; i++ {
		wg.Add(1)
		go func() {
			for {
				transcodeQueueLock.Lock()
				currentIdx++
				if currentIdx >= len(transcodeQueue) || err != nil {
					transcodeQueueLock.Unlock()
					wg.Done()
					return
				}
				op := transcodeQueue[currentIdx]
				spinProgress(int64(currentIdx))
				transcodeQueueLock.Unlock()

				if !*dryRunFlag {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("Transcoding '%s' to '%s' at %s ...", op.source.FilesystemPath, op.dest.FilesystemPath, ffmpegBitrateStr))
					// try without discarding album art; and if that fails try once more discarding video entirely:
					out, transErr := dzutil.Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", op.source.FilesystemPath, "-c:v", "copy", "-c:a", "aac", "-b:a", ffmpegBitrateStr, op.dest.FilesystemPath})
					if transErr != nil {
						_ = os.Remove(op.dest.FilesystemPath)
						cli.Out(spinCtx).Verbose(fmt.Sprintf("Transcoding of '%s' failed. Trying again without video. Error was: %s %s", op.source.FilesystemPath, out, transErr))
						out, transErr = dzutil.Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", op.source.FilesystemPath, "-vn", "-c:a", "aac", "-b:a", ffmpegBitrateStr, op.dest.FilesystemPath})
						if transErr != nil {
							_ = os.Remove(op.dest.FilesystemPath)
							transcodeQueueLock.Lock()
							err = fmt.Errorf("transcode '%s' failed: %w: %s", op.source.FilesystemPath, transErr, out) // it's possible that up to NumCPUs errors occur and we only see the most recent one, but we'll still exit, so whatever
							transcodeQueueLock.Unlock()
							wg.Done()
							return
						}
					}
					destInfo, transErr := os.Stat(op.dest.FilesystemPath)
					if transErr != nil {
						_ = os.Remove(op.dest.FilesystemPath)
						transcodeQueueLock.Lock()
						err = transErr
						transcodeQueueLock.Unlock()
						wg.Done()
						return
					}
					op.dest.Mode = destInfo.Mode()
					op.dest.FileSize = destInfo.Size()
				} else {
					cli.Out(spinCtx).Verbose(fmt.Sprintf("[dry run] Would transcode '%s' to '%s' at %s", op.source.FilesystemPath, op.dest.FilesystemPath, ffmpegBitrateStr))
					op.dest.Mode = fileCreateMode
					op.dest.FileSize = int64(math.Round(float64(op.source.FileSize) / float64(op.source.FileBitrate) * float64(targetTranscodeBitrate)))
				}
			}
		}()
	}
	wg.Wait()
	spinStop()
	if err != nil {
		return err
	}
	if len(transcodeQueue) > 0 {
		if *dryRunFlag {
			cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would transcode %d music files.", len(transcodeQueue)))
		} else {
			cli.Out(ctx).Log(fmt.Sprintf("Transcoded %d music files.", len(transcodeQueue)))
		}
	} else {
		cli.Out(ctx).Log("Nothing to transcode.")
	}

	cli.Out(ctx).Log("Removing empty directories from the destination directory tree ...")
	destI = 0
	spinCtx, spinProgress, spinStop = cli.WithProgress(ctx, "checking", destTree.CountNodes())
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
			cli.Out(ctx).Log(fmt.Sprintf("[dry run] Would remove %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath))
		} else {
			cli.Out(ctx).Log(fmt.Sprintf("Removed %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath))
		}
	} else {
		cli.Out(ctx).Log("0 directories affected.")
	}

	cli.Out(ctx).Log("")
	symlinkPart := ""
	if *makeSymlinksFlag {
		symlinkPart = " (after resolving symlinks created during sync)"
	}
	if !*dryRunFlag {
		cli.Out(ctx).Log(fmt.Sprintf("Destination library size is now %s%s.", filesize.ByteCountBothStyles(destTree.CalculateSize()), symlinkPart))
	} else {
		cli.Out(ctx).Log(fmt.Sprintf("[dry run] Destination library size is estimated to be %s%s.", filesize.ByteCountBothStyles(destTree.CalculateSize()), symlinkPart))
	}
	cli.Out(ctx).Log("Completed!")

	return nil
}
