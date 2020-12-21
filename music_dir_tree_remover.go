package main

import (
	"fmt"
	"log"

	"github.com/Bios-Marcel/wastebasket"
)

// RemoveChildrenMatching will remove any child nodes _and the filesystem objects they represent_ for which
// the given removeMatchFunc returns true.
// Returns the number of nodes removed, and an error if one is encountered.
func (n *MusicTreeNode) RemoveChildrenMatching(removeMatchFunc func(n *MusicTreeNode) bool, logReason string) (int, error) {
	removeCount := 0
	if n.Children != nil {
		for childKey, childNode := range n.Children {
			count, err := childNode.RemoveChildrenMatching(removeMatchFunc, logReason)
			removeCount += count
			if err != nil {
				return removeCount, err
			}

			if removeMatchFunc(childNode) {
				delete(n.Children, childKey)

				if !*dryRunFlag {
					if *verboseFlag {
						log.Printf("Removing '%s' because %s.", childNode.FilesystemPath, logReason)
					}
					if err := wastebasket.Trash(childNode.FilesystemPath); err != nil {
						return removeCount, fmt.Errorf("failed to trash '%s': %w", childNode.FilesystemPath, err)
					}
					removeCount++
				} else {
					removeCount++
					if *verboseFlag {
						log.Printf("[dry run] Would remove '%s' because %s.", childNode.FilesystemPath, logReason)
					}
				}
			}
		}
	}
	return removeCount, nil
}
