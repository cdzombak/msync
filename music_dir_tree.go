package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
)

// TODO(cdzombak): normalize BaseNameNormalized to lowercase only if destination is a case insensitive filesystem, or allow customizing

type MusicTreeNode struct {
	TreePath           []string                  // path to this node (normalized base names, as in keys of the Children dict) from the root of its MusicNodeTree. when walking a tree we could keep track of this dynamically, but this just makes comparing trees easier.
	FilesystemPath     string                    // path to this node on the filesystem, relative to whatever the root path for the tree is. (in the msync app, these are always absolute paths.)
	IsDirectory        bool                      // whether this represents a directory
	IsFile             bool                      // whether this represents a file
	IsMusicFile        bool                      // whether this represents a music file
	BaseName           string                    // base name of this entity on disk
	BaseNameNormalized string                    // base name of this entity, normalized to lowercase and with music file extensions removed
	FileSize           int64                     // size of this entity, iff it's a file
	FileBitrate        int                       // bitrate of this entity, iff it's a music file
	Mode               os.FileMode               // file mode of this entity
	Children           map[string]*MusicTreeNode // map of BaseNameNormalized -> *MusicTreeNode, iff it's a directory. nil if it's a file.
}

func MakeMusicTree(filePath string) (*MusicTreeNode, error) {
	return MakeMusicTreeNode(filePath, nil, true)
}

// MakeMusicTreeNode returns nil if the path does not point to a directory, regular file, or symlink.
func MakeMusicTreeNode(filePath string, parentNodePath []string, isRootNode bool) (*MusicTreeNode, error) {
	if *verboseFlag {
		log.Printf("Scanning '%s' ...", filePath)
	}
	rootInfo, err := os.Stat(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to stat '%s': %w", filePath, err)
	}
	n := &MusicTreeNode{
		BaseName:           rootInfo.Name(),
		BaseNameNormalized: normalizeFileNameForComparing(rootInfo.Name()),
		FilesystemPath:     filePath,
		Mode:               rootInfo.Mode(),
	}
	if !isRootNode {
		n.TreePath = append(parentNodePath, n.BaseNameNormalized)
	}
	if rootInfo.IsDir() {
		n.IsDirectory = true
	} else if n.Mode.IsRegular() || n.Mode&os.ModeSymlink != 0 {
		n.IsFile = true
	} else {
		log.Printf("[warning] Skipping '%s': it is not a regular file.", filePath)
		return nil, nil
	}
	if n.IsDirectory {
		n.Children = make(map[string]*MusicTreeNode)
		children, err := ioutil.ReadDir(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to list '%s': %w", filePath, err)
		}
		for _, child := range children {
			childNode, err := MakeMusicTreeNode(filepath.Join(filePath, child.Name()), n.TreePath, false)
			if err != nil {
				return nil, err
			}
			if childNode != nil {
				if existingNode, ok := n.Children[childNode.BaseNameNormalized]; ok {
					log.Printf("[warning] Normalized name collision in '%s': '%s' and '%s'.", filePath, existingNode.BaseName, childNode.BaseName)
				}
				n.Children[childNode.BaseNameNormalized] = childNode
			}
		}
	} else if n.IsFile {
		n.FileSize = rootInfo.Size()
		if isMusicFile(filePath) {
			n.IsMusicFile = true
			bitrate, err := fileBitrate(filePath)
			if err != nil {
				return nil, err
			}
			n.FileBitrate = bitrate
		}
	}
	return n, nil
}

// CalculateSize calculates the size on disk of this node and all its children.
// It returns bytes.
func (n *MusicTreeNode) CalculateSize() int64 {
	if n.IsFile {
		return n.FileSize
	}
	if n.Children == nil {
		return 0
	}
	totalSize := int64(0)
	for _, v := range n.Children {
		totalSize += v.CalculateSize()
	}
	return totalSize
}

// NodeAtTreePath returns true iff a node exists at the specified path down the tree from this node.
// The given path must be normalized.
func (n *MusicTreeNode) HasNodeAtTreePath(normalizedTreePath []string) bool {
	return n.NodeAtTreePath(normalizedTreePath) != nil
}

// NodeAtTreePath returns the node at the specified path down the tree from this node.
// The given path must be normalized. If no child node exists at this path, nil (not an error) is returned.
func (n *MusicTreeNode) NodeAtTreePath(normalizedTreePath []string) *MusicTreeNode {
	if len(normalizedTreePath) == 0 {
		return n
	}
	if n.Children == nil {
		return nil
	}
	if node, ok := n.Children[normalizedTreePath[0]]; ok {
		return node.NodeAtTreePath(normalizedTreePath[1:])
	}
	return nil
}

// Walk walks every node in the given tree, calling the given callback for every node.
func (n *MusicTreeNode) Walk(callback func(n *MusicTreeNode) error) error {
	for _, childNode := range n.Children {
		if err := childNode.Walk(callback); err != nil {
			return err
		}
	}
	return callback(n)
}
