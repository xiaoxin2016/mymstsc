//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

// eventSink receives the control's outgoing dispinterface (IMsTscAxEvents).
//
// Event DISPIDs are not hard-coded: they are read from the type library of the
// running mstscax.dll at start-up, so the mapping is always the one this
// Windows build actually uses.
type eventSink struct {
	vtblDispatch uintptr

	ref    int32
	iid    GUID
	names  map[int32]string
	cookie uint32
	cp     uintptr // IConnectionPoint we are advised on
	app    *App
}

var (
	sinkMu       sync.RWMutex
	sinkRegistry = map[uintptr]*eventSink{}
)

func lookupSink(this uintptr) *eventSink {
	sinkMu.RLock()
	s := sinkRegistry[this]
	sinkMu.RUnlock()
	return s
}

var vtblEventSink []uintptr

func newEventSink(app *App, iid GUID, names map[int32]string) *eventSink {
	s := &eventSink{app: app, iid: iid, names: names, ref: 1}
	s.vtblDispatch = uintptr(unsafe.Pointer(&vtblEventSink[0]))
	sinkMu.Lock()
	sinkRegistry[s.iface()] = s
	sinkMu.Unlock()
	return s
}

func (s *eventSink) iface() uintptr { return uintptr(unsafe.Pointer(&s.vtblDispatch)) }

func (s *eventSink) dispose() {
	s.unadvise()
	sinkMu.Lock()
	delete(sinkRegistry, s.iface())
	sinkMu.Unlock()
}

// advise hooks the sink onto the control's connection point.
func (s *eventSink) advise(container uintptr) error {
	var cp uintptr
	if hr := comCallHR(container, 4, // IConnectionPointContainer::FindConnectionPoint
		uintptr(unsafe.Pointer(&s.iid)), uintptr(unsafe.Pointer(&cp))); hr.Failed() {
		return errf("FindConnectionPoint(%s): %s", s.iid, hr.Error())
	}
	var cookie uint32
	if hr := comCallHR(cp, 5, // IConnectionPoint::Advise
		s.iface(), uintptr(unsafe.Pointer(&cookie))); hr.Failed() {
		unkRelease(cp)
		return errf("IConnectionPoint::Advise: %s", hr.Error())
	}
	s.cp, s.cookie = cp, cookie
	return nil
}

func (s *eventSink) unadvise() {
	if s.cp != 0 {
		if s.cookie != 0 {
			comCall(s.cp, 6, uintptr(s.cookie)) // IConnectionPoint::Unadvise
			s.cookie = 0
		}
		safeRelease(&s.cp)
	}
}

func init() {
	vtblEventSink = []uintptr{
		syscall.NewCallback(func(this uintptr, riid *GUID, ppv *uintptr) uintptr { // QueryInterface
			s := lookupSink(this)
			if s == nil || ppv == nil || riid == nil {
				return hres(E_POINTER)
			}
			*ppv = 0
			if riid.Equals(&IID_IUnknown) || riid.Equals(&IID_IDispatch) || riid.Equals(&s.iid) {
				*ppv = s.iface()
				s.ref++
				return hres(S_OK)
			}
			return hres(E_NOINTERFACE)
		}),
		syscall.NewCallback(func(this uintptr) uintptr { // AddRef
			s := lookupSink(this)
			if s == nil {
				return 1
			}
			s.ref++
			return uintptr(s.ref)
		}),
		syscall.NewCallback(func(this uintptr) uintptr { // Release
			s := lookupSink(this)
			if s == nil {
				return 0
			}
			s.ref--
			if s.ref < 0 {
				s.ref = 0
			}
			return uintptr(s.ref)
		}),
		syscall.NewCallback(func(this uintptr, pctinfo *uint32) uintptr { // GetTypeInfoCount
			if pctinfo != nil {
				*pctinfo = 0
			}
			return hres(S_OK)
		}),
		syscall.NewCallback(func(this, itinfo, lcid uintptr, pptinfo *uintptr) uintptr { // GetTypeInfo
			if pptinfo != nil {
				*pptinfo = 0
			}
			return hres(E_NOTIMPL)
		}),
		syscall.NewCallback(func(this, riid, names, cNames, lcid, dispids uintptr) uintptr { // GetIDsOfNames
			return hres(DISP_E_UNKNOWNNAME)
		}),
		syscall.NewCallback(cbSinkInvoke),
	}
}

// cbSinkInvoke implements IDispatch::Invoke for the event sink.
func cbSinkInvoke(this uintptr, dispid int32, riid *GUID, lcid uint32, flags uint16,
	params *DISPPARAMS, result *VARIANT, excep *EXCEPINFO, argErr *uint32) uintptr {
	// A panic must never cross back into the control.
	defer func() {
		if r := recover(); r != nil {
			logErrorf("panic in event handler: %v", r)
		}
	}()

	s := lookupSink(this)
	if s == nil {
		return hres(S_OK)
	}

	id := dispid
	name := s.names[id]

	var args []VARIANT
	if params != nil {
		dp := params
		if dp.CArgs > 0 && dp.Rgvarg != nil {
			raw := unsafe.Slice(dp.Rgvarg, int(dp.CArgs))
			// DISPPARAMS holds arguments in reverse declaration order.
			args = make([]VARIANT, len(raw))
			for i := range raw {
				args[len(raw)-1-i] = raw[i]
			}
		}
	}

	if s.app != nil {
		s.app.onEvent(id, name, args)
	}
	return hres(S_OK)
}

// argInt returns argument i as an int32.
func argInt(args []VARIANT, i int) (int32, bool) {
	if i < 0 || i >= len(args) {
		return 0, false
	}
	return args[i].Int()
}

// argString returns argument i rendered as a string.
func argString(args []VARIANT, i int) string {
	if i < 0 || i >= len(args) {
		return ""
	}
	return args[i].Str()
}
