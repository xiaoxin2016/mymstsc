//go:build windows

package main

import (
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// rdpControl wraps the Remote Desktop ActiveX control hosted by this program.
type rdpControl struct {
	unk           uintptr // IUnknown
	dispatch      disp    // IDispatch
	oleObject     uintptr // IOleObject
	inPlaceObject uintptr // IOleInPlaceObject
	activeObject  uintptr // IOleInPlaceActiveObject

	tl      *typeLib
	coclass coclassInfo

	eventIID   GUID
	eventNames map[int32]string
	eventTI    uintptr

	advanced  disp
	secured   disp
	transport disp

	nonScriptable []*tlType

	moduleHandle syscall.Handle // set when we loaded mstscax.dll ourselves
	inPlaceDone  bool
}

// mstscaxPath returns the full path of the Remote Desktop client control.
func mstscaxPath() string {
	return filepath.Join(systemDirectory(), "mstscax.dll")
}

// newRDPControl loads the type library of mstscax.dll, picks the best coclass
// and creates an instance of it.
func newRDPControl(forceCoclass string) (*rdpControl, error) {
	path := mstscaxPath()
	tl, err := loadTypeLib(path)
	if err != nil {
		return nil, errf("%w\n\n%s hosts the Remote Desktop client control from %s; "+
			"that file is part of Windows and must be present", err, appName, path)
	}

	candidates := rdpCoclasses(tl)
	if len(candidates) == 0 {
		tl.Close()
		return nil, errf("%s contains no Remote Desktop client coclass", path)
	}
	if forceCoclass != "" {
		var filtered []coclassInfo
		for _, c := range candidates {
			if strings.EqualFold(c.Type.Name, forceCoclass) {
				filtered = append(filtered, c)
			}
		}
		if len(filtered) == 0 {
			names := make([]string, 0, len(candidates))
			for _, c := range candidates {
				names = append(names, c.Type.Name)
			}
			tl.Close()
			return nil, errf("no coclass named %q in %s (available: %s)",
				forceCoclass, path, strings.Join(names, ", "))
		}
		candidates = filtered
	}

	c := &rdpControl{tl: tl}
	var lastErr error
	for _, cand := range candidates {
		unk, err := createInstance(cand.Type.GUID, path, &c.moduleHandle)
		if err != nil {
			logDebugf("%s: %v", cand.Type.Name, err)
			lastErr = err
			continue
		}
		c.unk = unk
		c.coclass = cand
		logInfof("using %s from %s", cand.Type.Name, path)
		logDebugf("CLSID %s", cand.Type.GUID)
		break
	}
	if c.unk == 0 {
		tl.Close()
		return nil, errf("could not create any Remote Desktop client control: %w", lastErr)
	}

	d, hr := unkQueryInterface(c.unk, &IID_IDispatch)
	if hr.Failed() {
		c.Close()
		return nil, errf("the control does not implement IDispatch: %s", hr.Error())
	}
	c.dispatch = disp{p: d, name: c.coclass.Type.Name}

	c.nonScriptable = nonScriptableInterfaces(tl)
	if ti, iid, err := c.coclass.Type.DefaultSource(); err == nil {
		c.eventTI = ti
		c.eventIID = iid
		c.eventNames = memberNames(ti)
		logDebugf("event interface %s with %d members", iid, len(c.eventNames))
	} else {
		logWarnf("event interface not found (%v); disconnect notifications will be polled", err)
	}
	return c, nil
}

// createInstance creates the coclass, falling back to loading mstscax.dll
// directly when the class is not registered in the machine's COM registry.
func createInstance(clsid GUID, dllPath string, mod *syscall.Handle) (uintptr, error) {
	unk, hr := coCreateInstance(&clsid, CLSCTX_INPROC_SERVER, &IID_IUnknown)
	if hr.OK() {
		return unk, nil
	}
	logDebugf("CoCreateInstance(%s): %s; falling back to DllGetClassObject", clsid, hr.Error())

	if *mod == 0 {
		h, err := syscall.LoadLibrary(dllPath)
		if err != nil {
			return 0, errf("CoCreateInstance failed (%s) and LoadLibrary(%s) failed: %v",
				hr.Error(), dllPath, err)
		}
		*mod = h
	}
	proc, err := syscall.GetProcAddress(*mod, "DllGetClassObject")
	if err != nil {
		return 0, errf("CoCreateInstance failed (%s) and %s exports no DllGetClassObject: %v",
			hr.Error(), dllPath, err)
	}
	var factory uintptr
	r, _, _ := syscall.SyscallN(uintptr(proc),
		uintptr(unsafe.Pointer(&clsid)),
		uintptr(unsafe.Pointer(&IID_IClassFactory)),
		uintptr(unsafe.Pointer(&factory)))
	if hr2 := HRESULT(int32(uint32(r))); hr2.Failed() {
		return 0, errf("DllGetClassObject(%s): %s", clsid, hr2.Error())
	}
	defer unkRelease(factory)

	var out uintptr
	if hr2 := comCallHR(factory, 3, 0,
		uintptr(unsafe.Pointer(&IID_IUnknown)),
		uintptr(unsafe.Pointer(&out))); hr2.Failed() {
		return 0, errf("IClassFactory::CreateInstance: %s", hr2.Error())
	}
	return out, nil
}

// Embed activates the control in place inside the given window.
func (c *rdpControl) Embed(site *oleSite, hwnd uintptr, rc RECT) error {
	obj, hr := unkQueryInterface(c.unk, &IID_IOleObject)
	if hr.Failed() {
		return errf("the control does not implement IOleObject: %s", hr.Error())
	}
	c.oleObject = obj

	// Some controls insist on being initialised before use.
	if psi, hr := unkQueryInterface(c.unk, &IID_IPersistStreamInit); hr.OK() {
		comCall(psi, 8) // IPersistStreamInit::InitNew
		unkRelease(psi)
	}

	if hr := comCallHR(c.oleObject, 3, site.clientSite()); hr.Failed() { // SetClientSite
		return errf("IOleObject::SetClientSite: %s", hr.Error())
	}
	comCall(c.oleObject, 5, // SetHostNames
		uintptr(unsafe.Pointer(utf16Ptr(appName))),
		uintptr(unsafe.Pointer(utf16Ptr(appName))))
	oleSetContainedObject(c.unk, true)

	if hr := comCallHR(c.oleObject, 11, // DoVerb
		uintptr(OLEIVERB_INPLACEACTIVATE&0xFFFFFFFF),
		0,
		site.clientSite(),
		0,
		hwnd,
		uintptr(unsafe.Pointer(&rc))); hr.Failed() {
		return errf("IOleObject::DoVerb(INPLACEACTIVATE): %s", hr.Error())
	}
	c.inPlaceDone = true

	if ip, hr := unkQueryInterface(c.unk, &IID_IOleInPlaceObject); hr.OK() {
		c.inPlaceObject = ip
	}
	if ao, hr := unkQueryInterface(c.unk, &IID_IOleInPlaceActiveObject); hr.OK() {
		c.activeObject = ao
	}

	// Bring the control's window up as well; the RDP control draws nothing
	// until it has been shown.
	comCall(c.oleObject, 11, uintptr(OLEIVERB_SHOW&0xFFFFFFFF), 0,
		site.clientSite(), 0, hwnd, uintptr(unsafe.Pointer(&rc)))
	return nil
}

// SetRect repositions the embedded control.
func (c *rdpControl) SetRect(rc RECT) {
	if c.inPlaceObject == 0 {
		return
	}
	comCall(c.inPlaceObject, 7, // IOleInPlaceObject::SetObjectRects
		uintptr(unsafe.Pointer(&rc)), uintptr(unsafe.Pointer(&rc)))
}

// AdviseEvents connects the event sink.
func (c *rdpControl) AdviseEvents(sink *eventSink) error {
	cpc, hr := unkQueryInterface(c.unk, &IID_IConnectionPointContainer)
	if hr.Failed() {
		return errf("IConnectionPointContainer: %s", hr.Error())
	}
	defer unkRelease(cpc)
	return sink.advise(cpc)
}

// Close releases everything the control holds, in the order OLE expects.
func (c *rdpControl) Close() {
	if c.oleObject != 0 {
		if c.inPlaceDone && c.inPlaceObject != 0 {
			comCall(c.inPlaceObject, 5) // InPlaceDeactivate
		}
		comCall(c.oleObject, 6, 0) // IOleObject::Close(OLECLOSE_SAVEIFDIRTY)
		comCall(c.oleObject, 3, 0) // SetClientSite(NULL)
	}
	safeRelease(&c.activeObject)
	safeRelease(&c.inPlaceObject)
	safeRelease(&c.oleObject)
	c.advanced.release()
	c.secured.release()
	c.transport.release()
	c.advanced, c.secured, c.transport = disp{}, disp{}, disp{}
	c.dispatch.release()
	c.dispatch = disp{}
	safeRelease(&c.unk)
	if c.eventTI != 0 {
		unkRelease(c.eventTI)
		c.eventTI = 0
	}
	if c.tl != nil {
		c.tl.Close()
		c.tl = nil
	}
	// mstscax.dll is deliberately left loaded: COM may still hold pointers
	// into it while the apartment is torn down, and the process is about to
	// exit anyway.
}

// ---------------------------------------------------------------------------
// Property targets
// ---------------------------------------------------------------------------

// Advanced returns IMsRdpClientAdvancedSettings, newest revision first.
func (c *rdpControl) Advanced() (disp, error) {
	if c.advanced.valid() {
		return c.advanced, nil
	}
	names := versionedNames("AdvancedSettings", 12)
	d, picked, err := c.dispatch.subAny(names...)
	if err != nil {
		return disp{}, errf("no AdvancedSettings interface: %w", err)
	}
	logDebugf("advanced settings via %s", picked)
	c.advanced = d
	return d, nil
}

// Secured returns IMsRdpClientSecuredSettings, newest revision first.
func (c *rdpControl) Secured() (disp, error) {
	if c.secured.valid() {
		return c.secured, nil
	}
	d, picked, err := c.dispatch.subAny(versionedNames("SecuredSettings", 5)...)
	if err != nil {
		return disp{}, errf("no SecuredSettings interface: %w", err)
	}
	logDebugf("secured settings via %s", picked)
	c.secured = d
	return d, nil
}

// Transport returns IMsRdpClientTransportSettings (RD Gateway).
func (c *rdpControl) Transport() (disp, error) {
	if c.transport.valid() {
		return c.transport, nil
	}
	d, picked, err := c.dispatch.subAny(versionedNames("TransportSettings", 5)...)
	if err != nil {
		return disp{}, errf("no TransportSettings interface: %w", err)
	}
	logDebugf("transport settings via %s", picked)
	c.transport = d
	return d, nil
}

// target resolves one of the symbolic property targets.
func (c *rdpControl) target(name string) (disp, error) {
	switch name {
	case "", "control":
		return c.dispatch, nil
	case "advanced":
		return c.Advanced()
	case "secured":
		return c.Secured()
	case "transport":
		return c.Transport()
	}
	return disp{}, errf("unknown property target %q", name)
}

// setNonScriptable writes a property of an IMsRdpClientNonScriptable*
// interface. Those are deliberately not reachable through IDispatch, so the
// call is marshalled through the interface's own type information.
func (c *rdpControl) setNonScriptable(name string, value interface{}) error {
	if len(c.nonScriptable) == 0 {
		return &notSupportedError{member: name, object: "IMsRdpClientNonScriptable*", hr: DISP_E_UNKNOWNNAME}
	}
	var lastErr error
	for _, t := range c.nonScriptable {
		memid, hr := tiMemberID(t.ti, name)
		if hr.Failed() {
			continue
		}
		inst, hr := unkQueryInterface(c.unk, &t.GUID)
		if hr.Failed() {
			lastErr = errf("%s is not implemented by this control: %s", t.Name, hr.Error())
			continue
		}
		_, err := tiInvoke(t.ti, inst, memid, DISPATCH_PROPERTYPUT, value)
		unkRelease(inst)
		if err == nil {
			logDebugf("set %s.%s = %v", t.Name, name, redactIfSecret(name, value))
			return nil
		}
		lastErr = errf("%s.%s: %w", t.Name, name, err)
	}
	if lastErr == nil {
		return &notSupportedError{member: name, object: "IMsRdpClientNonScriptable*", hr: DISP_E_UNKNOWNNAME}
	}
	return lastErr
}

// setAny writes the first of the given "target.Property" paths that the control
// actually implements. Paths are tried in order, which is how one code base
// keeps working across the interface revisions shipped with Windows Server 2016
// through 2025.
func (c *rdpControl) setAny(value interface{}, paths ...string) error {
	var lastErr error
	for _, p := range paths {
		targetName, prop := splitPath(p)
		if targetName == "nonscriptable" {
			err := c.setNonScriptable(prop, value)
			if err == nil {
				return nil
			}
			lastErr = err
			continue
		}
		t, err := c.target(targetName)
		if err != nil {
			lastErr = err
			continue
		}
		if err := t.put(prop, value); err != nil {
			lastErr = err
			continue
		}
		logDebugf("set %s = %v", p, redactIfSecret(prop, value))
		return nil
	}
	return lastErr
}

func splitPath(p string) (target, prop string) {
	if i := strings.LastIndex(p, "."); i >= 0 {
		return strings.ToLower(p[:i]), p[i+1:]
	}
	return "control", p
}

func redactIfSecret(name string, value interface{}) interface{} {
	n := strings.ToLower(name)
	if strings.Contains(n, "password") || strings.Contains(n, "secret") ||
		strings.Contains(n, "loadbalanceinfo") {
		return "********"
	}
	return value
}

// ---------------------------------------------------------------------------
// Connection state
// ---------------------------------------------------------------------------

// ConnectedState returns the control's Connected property:
// 0 = disconnected, 1 = connected, 2 = connecting.
func (c *rdpControl) ConnectedState() int32 {
	n, err := c.dispatch.getInt("Connected")
	if err != nil {
		logDebugf("reading Connected: %v", err)
		return -1
	}
	return n
}

func (c *rdpControl) Connect() error {
	return c.dispatch.call("Connect")
}

func (c *rdpControl) Disconnect() error {
	return c.dispatch.call("Disconnect")
}

// ExtendedDisconnectReason returns the control's extended reason code, or 0.
func (c *rdpControl) ExtendedDisconnectReason() int32 {
	n, err := c.dispatch.getInt("ExtendedDisconnectReason")
	if err != nil {
		return 0
	}
	return n
}

// ErrorDescription asks the control for the human-readable text of a
// disconnect. The strings come from the resources of mstscax.dll, so they are
// localised exactly as mstsc would show them.
func (c *rdpControl) ErrorDescription(reason, extended int32) string {
	s, err := c.dispatch.callString("GetErrorDescription", uint32(reason), uint32(extended))
	if err != nil {
		logDebugf("GetErrorDescription: %v", err)
		return ""
	}
	return strings.TrimSpace(s)
}

// UpdateSessionDisplaySettings asks the server to resize the desktop, which is
// what makes a windowed session follow the window like mstsc does. It exists
// from IMsRdpClient9 (Windows 8.1 / Server 2012 R2) onwards.
func (c *rdpControl) UpdateSessionDisplaySettings(w, h int32) error {
	return c.dispatch.call("UpdateSessionDisplaySettings",
		uint32(w), uint32(h), // desktop width/height
		uint32(0), uint32(0), // physical width/height in mm, 0 = unspecified
		uint32(0),   // orientation
		uint32(100), // desktop scale factor, percent
		uint32(100)) // device scale factor, percent
}

// Reconnect is the pre-IMsRdpClient9 way to change the session size.
func (c *rdpControl) Reconnect(w, h int32) error {
	return c.dispatch.call("Reconnect", uint32(w), uint32(h))
}
