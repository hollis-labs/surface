//go:build !unix

package wardentoken

import "errors"

// lockFile is unix-only: without a cross-process lock two processes could replay a parent.
func lockFile(string) (func(), error) {
	return nil, errors.New("wardentoken: parent credential locking needs a unix system")
}
