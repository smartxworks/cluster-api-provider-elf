package service

import "strings"

func IsVMNotFound(err error) bool {
	return err.Error() == "VM_NOT_FOUND"
}

func IsVMDuplicate(err error) bool {
	return strings.Contains(err.Error(), "VM_DUPLICATE")
}
