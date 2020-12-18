package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

// TODO(cdzombak): consider whether to normalize BaseNameForComparing to lowercase only if destination is a case insensitive filesystem

type MusicTreeNode struct {
	IsDirectory          bool                      // whether this represents a directory
	IsFile               bool                      // whether this represents a file
	BaseName             string                    // base name of this entity on disk
	BaseNameForComparing string                    // base name of this entity, normalized to lowercase and with music file extensions removed
	FileSize             int64                     // size of this entity, iff it's a file
	FileBitrate          int                       // bitrate of this entity, iff it's a music file
	Mode                 os.FileMode               // file mode of this entity
	Children             map[string]*MusicTreeNode // map of BaseNameForComparing -> *MusicTreeNode, iff it's a directory. nil if it's a file.
}

// MakeMusicTreeNode returns nil if the path does not point to a directory, regular file, or symlink.
func MakeMusicTreeNode(path string) (*MusicTreeNode, error) {
	if *verboseFlag {
		log.Printf("building node for '%s'", path)
	}
	rootInfo, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat '%s': %w", path, err)
	}
	n := &MusicTreeNode{
		BaseName:             rootInfo.Name(),
		BaseNameForComparing: normalizeFileNameForComparing(rootInfo.Name()),
		Mode:                 rootInfo.Mode(),
	}
	if rootInfo.IsDir() {
		n.IsDirectory = true
	} else if n.Mode.IsRegular() || n.Mode&os.ModeSymlink != 0 {
		n.IsFile = true
	} else {
		log.Printf("[warning] skipping '%s' as it is not a regular file", path)
		return nil, nil
	}
	if n.IsDirectory {
		n.Children = make(map[string]*MusicTreeNode)
		children, err := ioutil.ReadDir(path)
		if err != nil {
			return nil, fmt.Errorf("failed to list '%s': %w", path, err)
		}
		for _, child := range children {
			childNode, err := MakeMusicTreeNode(filepath.Join(path, child.Name()))
			if err != nil {
				return nil, err
			}
			if childNode != nil {
				if existingNode, ok := n.Children[childNode.BaseNameForComparing]; ok {
					log.Printf("[warning] normalized name collision in '%s': '%s' and '%s'", path, existingNode.BaseName, childNode.BaseName)
				}
				n.Children[childNode.BaseNameForComparing] = childNode
			}
		}
	} else if n.IsFile {
		n.FileSize = rootInfo.Size()
		if isMusicFile(path) {
			bitrate, err := fileBitrate(path)
			if err != nil {
				return nil, err
			}
			n.FileBitrate = bitrate
		} else {
			// skip anything that's not music (eg. booklet PDFs)
			log.Printf("[info] skipping '%s' as it is not a music file", path)
			return nil, nil
		}
	}
	return n, nil
}

func (n *MusicTreeNode) CalculateSizeRecursive() int64 {
	if n.IsFile {
		return n.FileSize
	}
	if n.Children == nil {
		return 0
	}
	totalSize := int64(0)
	for _, v := range n.Children {
		totalSize += v.CalculateSizeRecursive()
	}
	return totalSize
}
