package main

import (
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

var version = "undefined"

// TODO(cdzombak): CLI best practices: stdout vs stderr
// TODO(cdzombak): CLI best practices: progress!

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
	verboseFlag                  = flag.Bool("verbose", false, "Log detailed output to stderr.")
	printVersion                 = flag.Bool("version", false, "Print version and exit.")
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
	sourceRootPath, err := filepath.Abs(*fromFlag)
	if err != nil {
		return err
	}
	destRootPath, err := filepath.Abs(*toFlag)
	if err != nil {
		return err
	}

	started := time.Now()
	sourceTree, err := MakeMusicTree(sourceRootPath)
	if err != nil {
		return err
	}
	elapsed := time.Since(started).Round(time.Second)
	fmt.Printf("built tree for source (%s) in %s; size is %s\n", sourceRootPath, elapsed.String(), ByteCountBothStyles(sourceTree.CalculateSize()))

	started = time.Now()
	destTree, err := MakeMusicTree(destRootPath)
	if err != nil {
		return err
	}
	elapsed = time.Since(started).Round(time.Second)
	fmt.Printf("built tree for dest (%s) in %s; size is %s\n", destRootPath, elapsed.String(), ByteCountBothStyles(destTree.CalculateSize()))

	maxBitrate := *maxBitrateKbpsFlag * 1000

	// we could do this more efficiently by eg. combining remove passes, but I don't care.
	// this makes the program logic easier to follow, and a separate count pass makes reporting progress easier.

	// remove anything from dest that isn't in source:
	removeCount, err := destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return !sourceTree.HasNodeAtTreePath(n.TreePath)
	}, "file is gone from source directory")
	if removeCount > 0 {
		if *dryRunFlag {
			log.Printf("[dry run] would remove %d files from destination (%s) because the equivalent file is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath)
		} else {
			log.Printf("removed %d files from destination (%s) because the equivalent file is gone from source directory (%s)", removeCount, destTree.FilesystemPath, sourceTree.FilesystemPath)
		}
	}

	if *removeOtherFilesFromDestFlag {
		// remove anything from dest that isn't a music file:
		removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
			return !(n.IsDirectory || n.IsMusicFile)
		}, "file is not a music file")
		if removeCount > 0 {
			if *dryRunFlag {
				log.Printf("[dry run] would remove %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath)
			} else {
				log.Printf("removed %d non-music files from destination (%s)", removeCount, destTree.FilesystemPath)
			}
		}
	}

	// remove anything from dest that has too-high bitrate:
	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return n.IsMusicFile && n.FileBitrate > maxBitrate
	}, fmt.Sprintf("its bitrate exceeds %d bps", maxBitrate))
	if removeCount > 0 {
		if *dryRunFlag {
			log.Printf("[dry run] would remove %d files from destination (%s) because their bitrate exceeded %d", removeCount, destTree.FilesystemPath, maxBitrate)
		} else {
			log.Printf("removed %d files from destination (%s) because their bitrate exceeded %d", removeCount, destTree.FilesystemPath, maxBitrate)
		}
	}

	ffmpegBitrateStr := strconv.Itoa(*maxBitrateKbpsFlag) + "k"

	// TODO(cdzombak): walk is just planning; then execute plan with progress
	// either copy/link or reencode all music files & directories from source that aren't in dest:
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
					log.Printf("%s is missing from dest; will be transcoded to %s", n.FilesystemPath, destPath)
				}
			} else if *verboseFlag {
				log.Printf("%s is missing from dest; will be synced to %s", n.FilesystemPath, destPath)
			}

			destDirPath := destPath
			if n.IsFile {
				destDirPath = filepath.Dir(destDirPath)
			}
			if !*dryRunFlag {
				if *verboseFlag {
					log.Printf("mkdir -p '%s'", destDirPath)
				}
				err := os.MkdirAll(destDirPath, destTree.Mode)
				if err != nil {
					return err
				}
			} else if *verboseFlag {
				log.Printf("[dry run] would mkdir -p '%s'", destDirPath)
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
						log.Printf("transcoding '%s' to '%s' at %s ...", n.FilesystemPath, destPath, ffmpegBitrateStr)
					}
					out, err := Exec("ffmpeg", []string{"-loglevel", "warning", "-hide_banner", "-i", n.FilesystemPath, "-c:v", "copy", "-c:a", "aac", "-b:a", ffmpegBitrateStr, destPath})
					if err != nil {
						_ = os.Remove(destPath)
						return fmt.Errorf("transcode '%s' failed: %w: %s", n.FilesystemPath, err, out)
					}
				} else if *verboseFlag {
					log.Printf("[dry run] would transcode '%s' to '%s' at %s", n.FilesystemPath, destPath, ffmpegBitrateStr)
				}
			} else {
				newFileBitrate = n.FileBitrate
				// TODO(cdzombak): support copy instead
				if !*dryRunFlag {
					if *verboseFlag {
						log.Printf("symlinking '%s' to '%s'", destPath, n.FilesystemPath)
					}
					err := os.Symlink(n.FilesystemPath, destPath)
					if err != nil {
						return fmt.Errorf("failed to symlink '%s' to '%s': %w", destPath, n.FilesystemPath, err)
					}
				} else if *verboseFlag {
					log.Printf("[dry run] would symlink '%s' to '%s'", destPath, n.FilesystemPath)
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
		}
		return nil
	})
	if err != nil {
		return err
	}

	removeCount, err = destTree.RemoveChildrenMatching(func(n *MusicTreeNode) bool {
		return n.IsDirectory && len(n.Children) == 0
	}, "directory is empty")
	if removeCount > 0 {
		if *dryRunFlag {
			log.Printf("[dry run] would remove %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath)
		} else {
			log.Printf("removed %d empty directories from destination (%s)", removeCount, destTree.FilesystemPath)
		}
	}

	fmt.Println()
	fmt.Printf("destination library size is now %s\n", ByteCountBothStyles(destTree.CalculateSize()))

	return nil
}
