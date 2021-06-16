package service

func IsVMNotFound(err error) bool {
	return err.Error() == "VM_NOT_FOUND"
}
