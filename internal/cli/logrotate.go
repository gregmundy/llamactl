package cli

import (
	"fmt"
	"os"
)

// RotateIfLarge rotates path → path.1 → path.2 → ... → path.<keep> when
// path's size exceeds maxBytes. Older numbered files past `keep` are
// removed. Returns true if rotation happened. A missing path is not an
// error and returns (false, nil).
func RotateIfLarge(path string, maxBytes int64, keep int) (bool, error) {
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, fmt.Errorf("stat %s: %w", path, err)
	}
	if fi.Size() < maxBytes {
		return false, nil
	}
	// Drop the oldest if present.
	_ = os.Remove(fmt.Sprintf("%s.%d", path, keep))
	// Shift path.(N-1) → path.N, descending so we don't clobber.
	for i := keep - 1; i >= 1; i-- {
		src := fmt.Sprintf("%s.%d", path, i)
		dst := fmt.Sprintf("%s.%d", path, i+1)
		if _, err := os.Stat(src); err == nil {
			if err := os.Rename(src, dst); err != nil {
				return false, fmt.Errorf("rename %s -> %s: %w", src, dst, err)
			}
		}
	}
	if err := os.Rename(path, path+".1"); err != nil {
		return false, fmt.Errorf("rename %s -> %s.1: %w", path, path, err)
	}
	return true, nil
}
