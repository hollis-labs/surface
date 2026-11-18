package agent

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// SetupProblem says, in a sentence for the owner, why the agent cannot answer, or "" when
// nothing is wrong that can be seen from here. It is worked out at each call: the key file
// is read at every request, so fixing it takes effect without a restart. The key itself is
// never read into the answer; only whether the file can be read.
func (s *Service) SetupProblem() string {
	if s.custom {
		return ""
	}
	if s.cfgErr != nil {
		return fmt.Sprintf("The agent's config could not be read: %v.", s.cfgErr)
	}
	c := s.cfg
	switch {
	case c.Path == "" && c.Model == "":
		return "There is no agent config yet."
	case c.Model == "":
		return "The agent's config names no model."
	case c.GatewayURL == "":
		return "The agent's config names no gateway (gateway_url)."
	case c.KeyFile == "":
		return "The agent's config names no key file (key_file)."
	}
	return keyFileProblem(c.KeyFile)
}

// keyFileProblem says why the key file cannot be used, or "".
func keyFileProblem(path string) string {
	info, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Sprintf("The key file %s does not exist.", path)
	case errors.Is(err, fs.ErrPermission):
		return fmt.Sprintf("The key file %s cannot be read: permission denied.", path)
	case err != nil:
		return fmt.Sprintf("The key file %s cannot be read: %v.", path, err)
	case info.IsDir():
		return fmt.Sprintf("The key file %s is a directory.", path)
	case info.Size() == 0:
		return fmt.Sprintf("The key file %s is empty.", path)
	}
	f, err := os.Open(path) //nolint:gosec // the operator's own key file, named in their config; only its readability is checked
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			return fmt.Sprintf("The key file %s cannot be read: permission denied.", path)
		}
		return fmt.Sprintf("The key file %s cannot be read: %v.", path, err)
	}
	_ = f.Close()
	return ""
}
