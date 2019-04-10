// Code generated by counterfeiter. DO NOT EDIT.
package securityfakes

import (
	"context"
	"sync"

	"github.com/Peripli/service-manager/pkg/security"
)

type FakeKeyFetcher struct {
	GetEncryptionKeyStub        func(context.Context) ([]byte, error)
	getEncryptionKeyMutex       sync.RWMutex
	getEncryptionKeyArgsForCall []struct {
		arg1 context.Context
	}
	getEncryptionKeyReturns struct {
		result1 []byte
		result2 error
	}
	getEncryptionKeyReturnsOnCall map[int]struct {
		result1 []byte
		result2 error
	}
	invocations      map[string][][]interface{}
	invocationsMutex sync.RWMutex
}

func (fake *FakeKeyFetcher) GetEncryptionKey(arg1 context.Context) ([]byte, error) {
	fake.getEncryptionKeyMutex.Lock()
	ret, specificReturn := fake.getEncryptionKeyReturnsOnCall[len(fake.getEncryptionKeyArgsForCall)]
	fake.getEncryptionKeyArgsForCall = append(fake.getEncryptionKeyArgsForCall, struct {
		arg1 context.Context
	}{arg1})
	fake.recordInvocation("GetEncryptionKey", []interface{}{arg1})
	fake.getEncryptionKeyMutex.Unlock()
	if fake.GetEncryptionKeyStub != nil {
		return fake.GetEncryptionKeyStub(arg1)
	}
	if specificReturn {
		return ret.result1, ret.result2
	}
	fakeReturns := fake.getEncryptionKeyReturns
	return fakeReturns.result1, fakeReturns.result2
}

func (fake *FakeKeyFetcher) GetEncryptionKeyCallCount() int {
	fake.getEncryptionKeyMutex.RLock()
	defer fake.getEncryptionKeyMutex.RUnlock()
	return len(fake.getEncryptionKeyArgsForCall)
}

func (fake *FakeKeyFetcher) GetEncryptionKeyCalls(stub func(context.Context) ([]byte, error)) {
	fake.getEncryptionKeyMutex.Lock()
	defer fake.getEncryptionKeyMutex.Unlock()
	fake.GetEncryptionKeyStub = stub
}

func (fake *FakeKeyFetcher) GetEncryptionKeyArgsForCall(i int) context.Context {
	fake.getEncryptionKeyMutex.RLock()
	defer fake.getEncryptionKeyMutex.RUnlock()
	argsForCall := fake.getEncryptionKeyArgsForCall[i]
	return argsForCall.arg1
}

func (fake *FakeKeyFetcher) GetEncryptionKeyReturns(result1 []byte, result2 error) {
	fake.getEncryptionKeyMutex.Lock()
	defer fake.getEncryptionKeyMutex.Unlock()
	fake.GetEncryptionKeyStub = nil
	fake.getEncryptionKeyReturns = struct {
		result1 []byte
		result2 error
	}{result1, result2}
}

func (fake *FakeKeyFetcher) GetEncryptionKeyReturnsOnCall(i int, result1 []byte, result2 error) {
	fake.getEncryptionKeyMutex.Lock()
	defer fake.getEncryptionKeyMutex.Unlock()
	fake.GetEncryptionKeyStub = nil
	if fake.getEncryptionKeyReturnsOnCall == nil {
		fake.getEncryptionKeyReturnsOnCall = make(map[int]struct {
			result1 []byte
			result2 error
		})
	}
	fake.getEncryptionKeyReturnsOnCall[i] = struct {
		result1 []byte
		result2 error
	}{result1, result2}
}

func (fake *FakeKeyFetcher) Invocations() map[string][][]interface{} {
	fake.invocationsMutex.RLock()
	defer fake.invocationsMutex.RUnlock()
	fake.getEncryptionKeyMutex.RLock()
	defer fake.getEncryptionKeyMutex.RUnlock()
	copiedInvocations := map[string][][]interface{}{}
	for key, value := range fake.invocations {
		copiedInvocations[key] = value
	}
	return copiedInvocations
}

func (fake *FakeKeyFetcher) recordInvocation(key string, args []interface{}) {
	fake.invocationsMutex.Lock()
	defer fake.invocationsMutex.Unlock()
	if fake.invocations == nil {
		fake.invocations = map[string][][]interface{}{}
	}
	if fake.invocations[key] == nil {
		fake.invocations[key] = [][]interface{}{}
	}
	fake.invocations[key] = append(fake.invocations[key], args)
}

var _ security.KeyFetcher = new(FakeKeyFetcher)
