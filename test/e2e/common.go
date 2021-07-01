package e2e

import (
	"fmt"

	. "github.com/onsi/ginkgo" // nolint:golint,stylecheck
)

func Byf(format string, a ...interface{}) {
	By(fmt.Sprintf(format, a...))
}
