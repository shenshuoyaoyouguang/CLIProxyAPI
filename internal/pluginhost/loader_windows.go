//go:build windows

package pluginhost

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsBuffer struct {
	ptr uintptr
	len uintptr
}

type windowsHostAPI struct {
	abiVersion uint32
	hostCtx    uintptr
	call       uintptr
	freeBuffer uintptr
}

type windowsPluginAPI struct {
	abiVersion uint32
	call       uintptr
	freeBuffer uintptr
	shutdown   uintptr
}

var (
	windowsHostCallbackID      atomic.Uintptr
	windowsHostCallbackEntries sync.Map
	windowsHostCallCallback    = syscall.NewCallback(windowsHostCall)
	windowsHostFreeCallback    = syscall.NewCallback(windowsHostFree)
	procLocalAlloc             = syscall.NewLazyDLL("kernel32.dll").NewProc("LocalAlloc")
)

type dynamicLibraryLoader struct{}

type dynamicLibraryClient struct {
	dll       *syscall.DLL
	hostAPI   *windowsHostAPI
	hostCtxID uintptr
	api       windowsPluginAPI
}

func defaultPluginLoader() pluginLoader {
	return dynamicLibraryLoader{}
}

func (dynamicLibraryLoader) Open(file pluginFile, host *Host) (pluginClient, error) {
	dll, errLoad := syscall.LoadDLL(file.Path)
	if errLoad != nil {
		return nil, errLoad
	}
	proc, errProc := dll.FindProc("cliproxy_plugin_init")
	if errProc != nil {
		_ = dll.Release()
		return nil, errProc
	}
	id := windowsHostCallbackID.Add(1)
	windowsHostCallbackEntries.Store(id, dynamicHostCallbackEntry{host: host, pluginID: file.ID})
	client := &dynamicLibraryClient{
		dll:       dll,
		hostCtxID: id,
		hostAPI: &windowsHostAPI{
			abiVersion: pluginHostABIVersion,
			hostCtx:    id,
			call:       windowsHostCallCallback,
			freeBuffer: windowsHostFreeCallback,
		},
	}
	rc, _, errCall := proc.Call(uintptr(unsafe.Pointer(client.hostAPI)), uintptr(unsafe.Pointer(&client.api)))
	if rc != 0 {
		client.Shutdown()
		return nil, fmt.Errorf("cliproxy_plugin_init returned %d: %v", rc, errCall)
	}
	if client.api.abiVersion != pluginHostABIVersion {
		client.Shutdown()
		return nil, fmt.Errorf("plugin ABI version %d is not supported", client.api.abiVersion)
	}
	if client.api.call == 0 || client.api.freeBuffer == 0 {
		client.Shutdown()
		return nil, fmt.Errorf("plugin function table is incomplete")
	}
	return client, nil
}

func (c *dynamicLibraryClient) Call(ctx context.Context, method string, request []byte) ([]byte, error) {
	if c == nil || c.api.call == 0 {
		return nil, fmt.Errorf("plugin client is closed")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}
	}
	methodBytes, errMethod := syscall.BytePtrFromString(method)
	if errMethod != nil {
		return nil, errMethod
	}
	var requestPtr uintptr
	if len(request) > 0 {
		requestPtr = uintptr(unsafe.Pointer(&request[0]))
	}
	var response windowsBuffer
	rc, _, _ := syscall.SyscallN(
		c.api.call,
		uintptr(unsafe.Pointer(methodBytes)),
		requestPtr,
		uintptr(len(request)),
		uintptr(unsafe.Pointer(&response)),
	)
	var out []byte
	if response.ptr != 0 && response.len > 0 {
		out = unsafe.Slice((*byte)(unsafe.Add(nil, uintptr(response.ptr))), response.len)
		out = append([]byte(nil), out...)
	}
	if response.ptr != 0 {
		_, _, _ = syscall.SyscallN(c.api.freeBuffer, response.ptr, response.len)
	}
	if rc != 0 {
		if isPluginErrorEnvelope(out) {
			return out, nil
		}
		return nil, fmt.Errorf("plugin call %s returned %d: %s", method, rc, string(out))
	}
	return out, nil
}

func (c *dynamicLibraryClient) Shutdown() {
	if c == nil {
		return
	}
	if c.api.shutdown != 0 {
		_, _, _ = syscall.SyscallN(c.api.shutdown)
		c.api.shutdown = 0
	}
	if c.hostCtxID != 0 {
		windowsHostCallbackEntries.Delete(c.hostCtxID)
		c.hostCtxID = 0
	}
	if c.dll != nil {
		_ = c.dll.Release()
		c.dll = nil
	}
}

func windowsHostCall(hostCtx unsafe.Pointer, methodPtr unsafe.Pointer, requestPtr unsafe.Pointer, requestLen uintptr, responsePtr unsafe.Pointer) uintptr {
	if responsePtr != nil {
		response := (*windowsBuffer)(responsePtr)
		response.ptr = 0
		response.len = 0
	}
	if hostCtx == nil || methodPtr == nil {
		return 1
	}
	id := uintptr(hostCtx)
	rawHost, okHost := windowsHostCallbackEntries.Load(id)
	if !okHost {
		return 1
	}
	entry, okHost := rawHost.(dynamicHostCallbackEntry)
	if !okHost || entry.host == nil {
		return 1
	}
	var request []byte
	if requestPtr != nil && requestLen > 0 {
		request = unsafe.Slice((*byte)(requestPtr), requestLen)
		request = append([]byte(nil), request...)
	}
	ctx := withHostCallbackPluginID(context.Background(), entry.pluginID)
	resp, errCall := entry.host.callFromPlugin(ctx, windowsString(methodPtr), request)
	if errCall != nil {
		resp = marshalRPCError("host_call_failed", errCall.Error())
	}
	if len(resp) == 0 || responsePtr == nil {
		return 0
	}
	mem, _, errCall := syscall.SyscallN(procLocalAlloc.Addr(), windows.LMEM_FIXED, uintptr(len(resp)))
	if errCall != syscall.Errno(0) || mem == 0 {
		return 1
	}
	response := (*windowsBuffer)(responsePtr)
	copy(unsafe.Slice((*byte)(unsafe.Add(nil, mem)), len(resp)), resp)
	response.ptr = mem
	response.len = uintptr(len(resp))
	return 0
}

func windowsHostFree(ptr uintptr, len uintptr) uintptr {
	if ptr != 0 {
		_, _ = windows.LocalFree(windows.Handle(ptr))
	}
	return 0
}

func windowsString(ptr unsafe.Pointer) string {
	if ptr == nil {
		return ""
	}
	var out []byte
	for i := 0; ; i++ {
		b := *(*byte)(unsafe.Add(ptr, i))
		if b == 0 {
			break
		}
		out = append(out, b)
	}
	return string(out)
}
