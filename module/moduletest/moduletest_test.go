package moduletest_test

import (
	"testing"

	"github.com/hollis-labs/surface/module"
	"github.com/hollis-labs/surface/module/moduletest"
)

var _ module.Host = (*moduletest.Host)(nil)

func TestHostSatisfiesContract(t *testing.T) {}
