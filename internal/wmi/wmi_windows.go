//go:build windows

package wmi

import (
	"fmt"
	"syscall"
	"unsafe"
)

const (
	clsctxInprocServer                = 0x1
	coinitMultithreaded               = 0x0
	eoacNone                          = 0x0
	rpcCAuthnDefault                  = 0xffffffff
	rpcCAuthzDefault                  = 0xffffffff
	rpcCAuthnWinNT                    = 10
	rpcCAuthzNone                     = 0
	rpcCAuthnLevelCall                = 3
	rpcCImpLevelImpersonate           = 3
	rpcEIChangedMode          uintptr = 0x80010106
	rpcETooLate               uintptr = 0x80010119
	sFalse                    uintptr = 0x00000001
	sOk                       uintptr = 0x00000000
	wbemFlagReturnImmediately         = 0x10
	wbemFlagForwardOnly               = 0x20
	wbemInfinite                      = 0xffffffff
	vtEmpty                           = 0x0
	vtNull                            = 0x1
	vtI2                              = 0x2
	vtI4                              = 0x3
	vtR4                              = 0x4
	vtR8                              = 0x5
	vtBSTR                            = 0x8
	vtBool                            = 0xb
	vtUI1                             = 0x11
	vtUI2                             = 0x12
	vtUI4                             = 0x13
	vtI8                              = 0x14
	vtUI8                             = 0x15
	vtInt                             = 0x16
	vtUint                            = 0x17
	vtArray                           = 0x2000
)

var (
	modOle32  = syscall.NewLazyDLL("ole32.dll")
	modOleAut = syscall.NewLazyDLL("oleaut32.dll")

	procCoInitializeEx       = modOle32.NewProc("CoInitializeEx")
	procCoInitializeSecurity = modOle32.NewProc("CoInitializeSecurity")
	procCoCreateInstance     = modOle32.NewProc("CoCreateInstance")
	procCoSetProxyBlanket    = modOle32.NewProc("CoSetProxyBlanket")
	procCoUninitialize       = modOle32.NewProc("CoUninitialize")

	procSysAllocString        = modOleAut.NewProc("SysAllocString")
	procSysFreeString         = modOleAut.NewProc("SysFreeString")
	procVariantClear          = modOleAut.NewProc("VariantClear")
	procSafeArrayGetLBound    = modOleAut.NewProc("SafeArrayGetLBound")
	procSafeArrayGetUBound    = modOleAut.NewProc("SafeArrayGetUBound")
	procSafeArrayAccessData   = modOleAut.NewProc("SafeArrayAccessData")
	procSafeArrayUnaccessData = modOleAut.NewProc("SafeArrayUnaccessData")
)

var (
	clsidWbemLocator = guid{0x4590F811, 0x1D3A, 0x11D0, [8]byte{0x89, 0x1F, 0x00, 0xAA, 0x00, 0x4B, 0x2E, 0x24}}
	iidIWbemLocator  = guid{0xDC12A687, 0x737F, 0x11CF, [8]byte{0x88, 0x4D, 0x00, 0xAA, 0x00, 0x4B, 0x2E, 0x24}}
)

type guid struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type unknown struct {
	lpVtbl *unknownVtbl
}

type unknownVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
}

type wbemLocator struct {
	lpVtbl *wbemLocatorVtbl
}

type wbemLocatorVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	ConnectServer  uintptr
}

type wbemServices struct {
	lpVtbl *wbemServicesVtbl
}

type wbemServicesVtbl struct {
	QueryInterface             uintptr
	AddRef                     uintptr
	Release                    uintptr
	OpenNamespace              uintptr
	CancelAsyncCall            uintptr
	QueryObjectSink            uintptr
	GetObject                  uintptr
	GetObjectAsync             uintptr
	PutClass                   uintptr
	PutClassAsync              uintptr
	DeleteClass                uintptr
	DeleteClassAsync           uintptr
	CreateClassEnum            uintptr
	CreateClassEnumAsync       uintptr
	PutInstance                uintptr
	PutInstanceAsync           uintptr
	DeleteInstance             uintptr
	DeleteInstanceAsync        uintptr
	CreateInstanceEnum         uintptr
	CreateInstanceEnumAsync    uintptr
	ExecQuery                  uintptr
	ExecQueryAsync             uintptr
	ExecNotificationQuery      uintptr
	ExecNotificationQueryAsync uintptr
	ExecMethod                 uintptr
	ExecMethodAsync            uintptr
}

type enumWbemClassObject struct {
	lpVtbl *enumWbemClassObjectVtbl
}

type enumWbemClassObjectVtbl struct {
	QueryInterface uintptr
	AddRef         uintptr
	Release        uintptr
	Reset          uintptr
	Next           uintptr
	NextAsync      uintptr
	Clone          uintptr
	Skip           uintptr
}

type wbemClassObject struct {
	lpVtbl *wbemClassObjectVtbl
}

type wbemClassObjectVtbl struct {
	QueryInterface          uintptr
	AddRef                  uintptr
	Release                 uintptr
	GetQualifierSet         uintptr
	Get                     uintptr
	Put                     uintptr
	Delete                  uintptr
	GetNames                uintptr
	BeginEnumeration        uintptr
	Next                    uintptr
	EndEnumeration          uintptr
	GetPropertyQualifierSet uintptr
	Clone                   uintptr
	GetObjectText           uintptr
	SpawnDerivedClass       uintptr
	SpawnInstance           uintptr
	CompareTo               uintptr
	GetPropertyOrigin       uintptr
	InheritsFrom            uintptr
	GetMethod               uintptr
	PutMethod               uintptr
	DeleteMethod            uintptr
	BeginMethodEnumeration  uintptr
	NextMethod              uintptr
	EndMethodEnumeration    uintptr
	GetMethodQualifierSet   uintptr
	GetMethodOrigin         uintptr
}

type variant struct {
	VT        uint16
	Reserved1 uint16
	Reserved2 uint16
	Reserved3 uint16
	Val       uintptr
	ValHigh   uintptr
}

type Client struct {
	locator  *wbemLocator
	services map[string]*wbemServices
}

func NewClient() (*Client, error) {
	if hr, _, _ := procCoInitializeEx.Call(0, coinitMultithreaded); failed(hr) && hr != sFalse && hr != rpcEIChangedMode {
		return nil, hresultError("CoInitializeEx", hr)
	}

	if hr, _, _ := procCoInitializeSecurity.Call(
		0,
		0xffffffff,
		0,
		0,
		rpcCAuthnLevelCall,
		rpcCImpLevelImpersonate,
		0,
		eoacNone,
		0,
	); failed(hr) && hr != rpcETooLate {
		procCoUninitialize.Call()
		return nil, hresultError("CoInitializeSecurity", hr)
	}

	var locator *wbemLocator
	if hr, _, _ := procCoCreateInstance.Call(
		uintptr(unsafe.Pointer(&clsidWbemLocator)),
		0,
		clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidIWbemLocator)),
		uintptr(unsafe.Pointer(&locator)),
	); failed(hr) {
		procCoUninitialize.Call()
		return nil, hresultError("CoCreateInstance", hr)
	}

	return &Client{
		locator:  locator,
		services: make(map[string]*wbemServices),
	}, nil
}

func (c *Client) Close() {
	for namespace, service := range c.services {
		if service != nil {
			service.Release()
		}
		delete(c.services, namespace)
	}
	if c.locator != nil {
		c.locator.Release()
	}
	procCoUninitialize.Call()
}

func (c *Client) Query(namespace, query string, properties ...string) ([]map[string]any, error) {
	service, err := c.service(namespace)
	if err != nil {
		return nil, err
	}

	lang := sysAllocString("WQL")
	defer sysFreeString(lang)
	queryText := sysAllocString(query)
	defer sysFreeString(queryText)

	var enum *enumWbemClassObject
	hr, _, _ := syscall.SyscallN(
		service.lpVtbl.ExecQuery,
		uintptr(unsafe.Pointer(service)),
		uintptr(unsafe.Pointer(lang)),
		uintptr(unsafe.Pointer(queryText)),
		wbemFlagForwardOnly|wbemFlagReturnImmediately,
		0,
		uintptr(unsafe.Pointer(&enum)),
	)
	if failed(hr) {
		return nil, hresultError("ExecQuery", hr)
	}
	defer enum.Release()

	var rows []map[string]any
	for {
		var returned uint32
		var obj *wbemClassObject
		hr, _, _ = syscall.SyscallN(
			enum.lpVtbl.Next,
			uintptr(unsafe.Pointer(enum)),
			wbemInfinite,
			1,
			uintptr(unsafe.Pointer(&obj)),
			uintptr(unsafe.Pointer(&returned)),
		)
		if returned == 0 {
			break
		}
		if failed(hr) {
			return nil, hresultError("IEnumWbemClassObject::Next", hr)
		}

		row := make(map[string]any, len(properties))
		for _, property := range properties {
			value, getErr := obj.Get(property)
			if getErr != nil {
				obj.Release()
				return nil, getErr
			}
			row[property] = value
		}
		obj.Release()
		rows = append(rows, row)
	}

	return rows, nil
}

func (c *Client) service(namespace string) (*wbemServices, error) {
	if service, ok := c.services[namespace]; ok {
		return service, nil
	}

	ns := sysAllocString(namespace)
	defer sysFreeString(ns)

	var service *wbemServices
	hr, _, _ := syscall.SyscallN(
		c.locator.lpVtbl.ConnectServer,
		uintptr(unsafe.Pointer(c.locator)),
		uintptr(unsafe.Pointer(ns)),
		0,
		0,
		0,
		0,
		0,
		uintptr(unsafe.Pointer(&service)),
	)
	if failed(hr) {
		return nil, hresultError("IWbemLocator::ConnectServer", hr)
	}

	if hr, _, _ = procCoSetProxyBlanket.Call(
		uintptr(unsafe.Pointer(service)),
		rpcCAuthnWinNT,
		rpcCAuthzNone,
		0,
		rpcCAuthnLevelCall,
		rpcCImpLevelImpersonate,
		0,
		eoacNone,
	); failed(hr) {
		service.Release()
		return nil, hresultError("CoSetProxyBlanket", hr)
	}

	c.services[namespace] = service
	return service, nil
}

func (o *wbemClassObject) Get(name string) (any, error) {
	ptr, err := syscall.UTF16PtrFromString(name)
	if err != nil {
		return nil, err
	}

	var value variant
	hr, _, _ := syscall.SyscallN(
		o.lpVtbl.Get,
		uintptr(unsafe.Pointer(o)),
		uintptr(unsafe.Pointer(ptr)),
		0,
		uintptr(unsafe.Pointer(&value)),
		0,
		0,
	)
	if failed(hr) {
		return nil, hresultError("IWbemClassObject::Get", hr)
	}
	defer variantClear(&value)

	return variantValue(&value)
}

func (u *wbemLocator) Release() {
	syscall.SyscallN(u.lpVtbl.Release, uintptr(unsafe.Pointer(u)))
}

func (u *wbemServices) Release() {
	syscall.SyscallN(u.lpVtbl.Release, uintptr(unsafe.Pointer(u)))
}

func (u *enumWbemClassObject) Release() {
	syscall.SyscallN(u.lpVtbl.Release, uintptr(unsafe.Pointer(u)))
}

func (u *wbemClassObject) Release() {
	syscall.SyscallN(u.lpVtbl.Release, uintptr(unsafe.Pointer(u)))
}

func variantValue(v *variant) (any, error) {
	switch v.VT {
	case vtEmpty, vtNull:
		return nil, nil
	case vtBSTR:
		return bstrToString((*uint16)(unsafe.Pointer(v.Val))), nil
	case vtBool:
		return int16(v.Val) != 0, nil
	case vtUI1:
		return uint8(v.Val), nil
	case vtUI2:
		return uint16(v.Val), nil
	case vtUI4, vtUint:
		return uint32(v.Val), nil
	case vtI2:
		return int16(v.Val), nil
	case vtI4, vtInt:
		return int32(v.Val), nil
	case vtI8:
		return int64(v.Val), nil
	case vtUI8:
		return uint64(v.Val), nil
	case vtR4:
		return *(*float32)(unsafe.Pointer(&v.Val)), nil
	case vtR8:
		return *(*float64)(unsafe.Pointer(&v.Val)), nil
	case vtArray | vtUI2:
		return safeArrayUint16((*safeArray)(unsafe.Pointer(v.Val)))
	case vtArray | vtUI1:
		return safeArrayBytes((*safeArray)(unsafe.Pointer(v.Val)))
	default:
		return nil, nil
	}
}

type safeArray struct{}

func safeArrayBounds(arr *safeArray) (int32, int32, error) {
	var lower int32
	if hr, _, _ := procSafeArrayGetLBound.Call(uintptr(unsafe.Pointer(arr)), 1, uintptr(unsafe.Pointer(&lower))); failed(hr) {
		return 0, 0, hresultError("SafeArrayGetLBound", hr)
	}

	var upper int32
	if hr, _, _ := procSafeArrayGetUBound.Call(uintptr(unsafe.Pointer(arr)), 1, uintptr(unsafe.Pointer(&upper))); failed(hr) {
		return 0, 0, hresultError("SafeArrayGetUBound", hr)
	}

	return lower, upper, nil
}

func safeArrayBytes(arr *safeArray) ([]byte, error) {
	lower, upper, err := safeArrayBounds(arr)
	if err != nil {
		return nil, err
	}
	if upper < lower {
		return nil, nil
	}

	var raw uintptr
	if hr, _, _ := procSafeArrayAccessData.Call(uintptr(unsafe.Pointer(arr)), uintptr(unsafe.Pointer(&raw))); failed(hr) {
		return nil, hresultError("SafeArrayAccessData", hr)
	}
	defer procSafeArrayUnaccessData.Call(uintptr(unsafe.Pointer(arr)))

	length := int(upper-lower) + 1
	return unsafe.Slice((*byte)(unsafe.Pointer(raw)), length), nil
}

func safeArrayUint16(arr *safeArray) ([]uint16, error) {
	lower, upper, err := safeArrayBounds(arr)
	if err != nil {
		return nil, err
	}
	if upper < lower {
		return nil, nil
	}

	var raw uintptr
	if hr, _, _ := procSafeArrayAccessData.Call(uintptr(unsafe.Pointer(arr)), uintptr(unsafe.Pointer(&raw))); failed(hr) {
		return nil, hresultError("SafeArrayAccessData", hr)
	}
	defer procSafeArrayUnaccessData.Call(uintptr(unsafe.Pointer(arr)))

	length := int(upper-lower) + 1
	return unsafe.Slice((*uint16)(unsafe.Pointer(raw)), length), nil
}

func sysAllocString(value string) *uint16 {
	ptr, _ := syscall.UTF16PtrFromString(value)
	raw, _, _ := procSysAllocString.Call(uintptr(unsafe.Pointer(ptr)))
	return (*uint16)(unsafe.Pointer(raw))
}

func sysFreeString(value *uint16) {
	if value != nil {
		procSysFreeString.Call(uintptr(unsafe.Pointer(value)))
	}
}

func variantClear(v *variant) {
	procVariantClear.Call(uintptr(unsafe.Pointer(v)))
}

func bstrToString(value *uint16) string {
	if value == nil {
		return ""
	}
	return syscall.UTF16ToString(unsafe.Slice(value, 1<<20))
}

func failed(hr uintptr) bool {
	return int32(hr) < 0
}

func hresultError(op string, hr uintptr) error {
	return fmt.Errorf("%s failed: 0x%08X", op, uint32(hr))
}
