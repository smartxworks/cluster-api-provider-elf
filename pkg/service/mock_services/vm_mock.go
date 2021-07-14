// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/smartxworks/cluster-api-provider-elf/pkg/service/vm.go

// Package mock_services is a generated GoMock package.
package mock_services

import (
	reflect "reflect"

	gomock "github.com/golang/mock/gomock"
	v1alpha4 "github.com/smartxworks/cluster-api-provider-elf/api/v1alpha4"
	client "github.smartx.com/smartx/elf-sdk-go/client"
	v1alpha40 "sigs.k8s.io/cluster-api/api/v1alpha4"
)

// MockVMService is a mock of VMService interface.
type MockVMService struct {
	ctrl     *gomock.Controller
	recorder *MockVMServiceMockRecorder
}

// MockVMServiceMockRecorder is the mock recorder for MockVMService.
type MockVMServiceMockRecorder struct {
	mock *MockVMService
}

// NewMockVMService creates a new mock instance.
func NewMockVMService(ctrl *gomock.Controller) *MockVMService {
	mock := &MockVMService{ctrl: ctrl}
	mock.recorder = &MockVMServiceMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockVMService) EXPECT() *MockVMServiceMockRecorder {
	return m.recorder
}

// Clone mocks base method.
func (m *MockVMService) Clone(machine *v1alpha40.Machine, elfMachine *v1alpha4.ElfMachine, bootstrapData string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Clone", machine, elfMachine, bootstrapData)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Clone indicates an expected call of Clone.
func (mr *MockVMServiceMockRecorder) Clone(machine, elfMachine, bootstrapData interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Clone", reflect.TypeOf((*MockVMService)(nil).Clone), machine, elfMachine, bootstrapData)
}

// Delete mocks base method.
func (m *MockVMService) Delete(uuid string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Delete", uuid)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Delete indicates an expected call of Delete.
func (mr *MockVMServiceMockRecorder) Delete(uuid interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Delete", reflect.TypeOf((*MockVMService)(nil).Delete), uuid)
}

// Get mocks base method.
func (m *MockVMService) Get(uuid string) (*v1alpha4.VirtualMachine, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Get", uuid)
	ret0, _ := ret[0].(*v1alpha4.VirtualMachine)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Get indicates an expected call of Get.
func (mr *MockVMServiceMockRecorder) Get(uuid interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Get", reflect.TypeOf((*MockVMService)(nil).Get), uuid)
}

// GetJob mocks base method.
func (m *MockVMService) GetJob(jobId string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetJob", jobId)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetJob indicates an expected call of GetJob.
func (mr *MockVMServiceMockRecorder) GetJob(jobId interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetJob", reflect.TypeOf((*MockVMService)(nil).GetJob), jobId)
}

// GetVMTemplate mocks base method.
func (m *MockVMService) GetVMTemplate(templateUUID string) (*client.VmTemplate, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "GetVMTemplate", templateUUID)
	ret0, _ := ret[0].(*client.VmTemplate)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// GetVMTemplate indicates an expected call of GetVMTemplate.
func (mr *MockVMServiceMockRecorder) GetVMTemplate(templateUUID interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "GetVMTemplate", reflect.TypeOf((*MockVMService)(nil).GetVMTemplate), templateUUID)
}

// PowerOff mocks base method.
func (m *MockVMService) PowerOff(uuid string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PowerOff", uuid)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PowerOff indicates an expected call of PowerOff.
func (mr *MockVMServiceMockRecorder) PowerOff(uuid interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PowerOff", reflect.TypeOf((*MockVMService)(nil).PowerOff), uuid)
}

// PowerOn mocks base method.
func (m *MockVMService) PowerOn(uuid string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "PowerOn", uuid)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// PowerOn indicates an expected call of PowerOn.
func (mr *MockVMServiceMockRecorder) PowerOn(uuid interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "PowerOn", reflect.TypeOf((*MockVMService)(nil).PowerOn), uuid)
}

// WaitJob mocks base method.
func (m *MockVMService) WaitJob(jobId string) (*v1alpha4.VMJob, error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "WaitJob", jobId)
	ret0, _ := ret[0].(*v1alpha4.VMJob)
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// WaitJob indicates an expected call of WaitJob.
func (mr *MockVMServiceMockRecorder) WaitJob(jobId interface{}) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "WaitJob", reflect.TypeOf((*MockVMService)(nil).WaitJob), jobId)
}
