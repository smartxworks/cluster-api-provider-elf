package context

import (
	"fmt"

	"github.com/go-logr/logr"
)

// ControllerContext is the context of a controller.
type ControllerContext struct {
	*ControllerManagerContext

	// Name is the name of the controller.
	Name string

	// Logger is the controller's logger.
	Logger logr.Logger
}

// String returns ControllerManagerName/ControllerName.
func (r *ControllerContext) String() string {
	return fmt.Sprintf("%s/%s", r.ControllerManagerContext.String(), r.Name)
}
