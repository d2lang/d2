//go:build !unix

package localfile

import "os"

// There is no portable os.OpenFile flag corresponding to Unix O_NONBLOCK.
// requireRegular still rejects special files after these calls return.
func openFile(name string) (*os.File, error) {
	return os.Open(name)
}

func openFileInRoot(root *os.Root, name string) (*os.File, error) {
	return root.Open(name)
}
