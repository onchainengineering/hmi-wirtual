// Code generated by MockGen. DO NOT EDIT.
// Source: github.com/onchainengineering/hmi-wirtual/wirtuald/database/pubsub (interfaces: Pubsub)
//
// Generated by this command:
//
//	mockgen -destination ./psmock.go -package psmock github.com/onchainengineering/hmi-wirtual/wirtuald/database/pubsub Pubsub
//

// Package psmock is a generated GoMock package.
package psmock

import (
	reflect "reflect"

	pubsub "github.com/onchainengineering/hmi-wirtual/wirtuald/database/pubsub"
	gomock "go.uber.org/mock/gomock"
)

// MockPubsub is a mock of Pubsub interface.
type MockPubsub struct {
	ctrl     *gomock.Controller
	recorder *MockPubsubMockRecorder
}

// MockPubsubMockRecorder is the mock recorder for MockPubsub.
type MockPubsubMockRecorder struct {
	mock *MockPubsub
}

// NewMockPubsub creates a new mock instance.
func NewMockPubsub(ctrl *gomock.Controller) *MockPubsub {
	mock := &MockPubsub{ctrl: ctrl}
	mock.recorder = &MockPubsubMockRecorder{mock}
	return mock
}

// EXPECT returns an object that allows the caller to indicate expected use.
func (m *MockPubsub) EXPECT() *MockPubsubMockRecorder {
	return m.recorder
}

// Close mocks base method.
func (m *MockPubsub) Close() error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Close")
	ret0, _ := ret[0].(error)
	return ret0
}

// Close indicates an expected call of Close.
func (mr *MockPubsubMockRecorder) Close() *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Close", reflect.TypeOf((*MockPubsub)(nil).Close))
}

// Publish mocks base method.
func (m *MockPubsub) Publish(arg0 string, arg1 []byte) error {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Publish", arg0, arg1)
	ret0, _ := ret[0].(error)
	return ret0
}

// Publish indicates an expected call of Publish.
func (mr *MockPubsubMockRecorder) Publish(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Publish", reflect.TypeOf((*MockPubsub)(nil).Publish), arg0, arg1)
}

// Subscribe mocks base method.
func (m *MockPubsub) Subscribe(arg0 string, arg1 pubsub.Listener) (func(), error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "Subscribe", arg0, arg1)
	ret0, _ := ret[0].(func())
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// Subscribe indicates an expected call of Subscribe.
func (mr *MockPubsubMockRecorder) Subscribe(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "Subscribe", reflect.TypeOf((*MockPubsub)(nil).Subscribe), arg0, arg1)
}

// SubscribeWithErr mocks base method.
func (m *MockPubsub) SubscribeWithErr(arg0 string, arg1 pubsub.ListenerWithErr) (func(), error) {
	m.ctrl.T.Helper()
	ret := m.ctrl.Call(m, "SubscribeWithErr", arg0, arg1)
	ret0, _ := ret[0].(func())
	ret1, _ := ret[1].(error)
	return ret0, ret1
}

// SubscribeWithErr indicates an expected call of SubscribeWithErr.
func (mr *MockPubsubMockRecorder) SubscribeWithErr(arg0, arg1 any) *gomock.Call {
	mr.mock.ctrl.T.Helper()
	return mr.mock.ctrl.RecordCallWithMethodType(mr.mock, "SubscribeWithErr", reflect.TypeOf((*MockPubsub)(nil).SubscribeWithErr), arg0, arg1)
}
