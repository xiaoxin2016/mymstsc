//go:build windows

package main

import (
	"sync"
	"syscall"
	"unsafe"
)

// OLEINPLACEFRAMEINFO mirrors the Win32 structure of the same name.
type OLEINPLACEFRAMEINFO struct {
	Cb            uint32
	FMDIApp       int32
	HwndFrame     uintptr
	Haccel        uintptr
	CAccelEntries uint32
	_             uint32
}

// BORDERWIDTHS is a RECT.
type BORDERWIDTHS = RECT

// OLE verbs (oleidl.h).
const (
	OLEIVERB_PRIMARY          = 0
	OLEIVERB_SHOW             = -1
	OLEIVERB_OPEN             = -2
	OLEIVERB_HIDE             = -3
	OLEIVERB_UIACTIVATE       = -4
	OLEIVERB_INPLACEACTIVATE  = -5
	OLEIVERB_DISCARDUNDOSTATE = -6
)

// oleSite is the container the RDP ActiveX control is embedded into. It
// implements IOleClientSite, IOleInPlaceSite, IOleInPlaceFrame,
// IOleControlSite and a stub IDispatch for ambient properties.
//
// The interface pointers handed to the control are the addresses of the
// vtable-pointer fields below. Go's garbage collector does not relocate heap
// objects, and siteRegistry keeps every live site reachable, so those
// addresses stay valid for as long as the control holds them.
type oleSite struct {
	vtblClientSite  uintptr
	vtblInPlaceSite uintptr
	vtblFrame       uintptr
	vtblControlSite uintptr
	vtblDispatch    uintptr

	ref int32
	app *App

	// activeObject is the control's IOleInPlaceActiveObject, cached from
	// IOleInPlaceFrame::SetActiveObject so the message loop can offer it
	// keyboard input first.
	activeObject uintptr
}

var (
	siteMu       sync.RWMutex
	siteRegistry = map[uintptr]*oleSite{}
)

func lookupSite(this uintptr) *oleSite {
	siteMu.RLock()
	s := siteRegistry[this]
	siteMu.RUnlock()
	return s
}

func newOleSite(app *App) *oleSite {
	s := &oleSite{app: app, ref: 1}
	s.vtblClientSite = uintptr(unsafe.Pointer(&vtblOleClientSite[0]))
	s.vtblInPlaceSite = uintptr(unsafe.Pointer(&vtblOleInPlaceSite[0]))
	s.vtblFrame = uintptr(unsafe.Pointer(&vtblOleInPlaceFrame[0]))
	s.vtblControlSite = uintptr(unsafe.Pointer(&vtblOleControlSite[0]))
	s.vtblDispatch = uintptr(unsafe.Pointer(&vtblAmbientDispatch[0]))

	siteMu.Lock()
	siteRegistry[s.clientSite()] = s
	siteRegistry[s.inPlaceSite()] = s
	siteRegistry[s.frame()] = s
	siteRegistry[s.controlSite()] = s
	siteRegistry[s.ambient()] = s
	siteMu.Unlock()
	return s
}

func (s *oleSite) clientSite() uintptr  { return uintptr(unsafe.Pointer(&s.vtblClientSite)) }
func (s *oleSite) inPlaceSite() uintptr { return uintptr(unsafe.Pointer(&s.vtblInPlaceSite)) }
func (s *oleSite) frame() uintptr       { return uintptr(unsafe.Pointer(&s.vtblFrame)) }
func (s *oleSite) controlSite() uintptr { return uintptr(unsafe.Pointer(&s.vtblControlSite)) }
func (s *oleSite) ambient() uintptr     { return uintptr(unsafe.Pointer(&s.vtblDispatch)) }

func (s *oleSite) dispose() {
	siteMu.Lock()
	delete(siteRegistry, s.clientSite())
	delete(siteRegistry, s.inPlaceSite())
	delete(siteRegistry, s.frame())
	delete(siteRegistry, s.controlSite())
	delete(siteRegistry, s.ambient())
	siteMu.Unlock()
	safeRelease(&s.activeObject)
}

// queryInterface is shared by all five vtables.
func (s *oleSite) queryInterface(riid *GUID, ppv *uintptr) HRESULT {
	if ppv == nil {
		return E_POINTER
	}
	*ppv = 0
	if riid == nil {
		return E_POINTER
	}
	switch {
	case riid.Equals(&IID_IUnknown), riid.Equals(&IID_IOleClientSite):
		*ppv = s.clientSite()
	case riid.Equals(&IID_IOleInPlaceSite), riid.Equals(&IID_IOleWindow):
		*ppv = s.inPlaceSite()
	case riid.Equals(&IID_IOleInPlaceFrame), riid.Equals(&IID_IOleInPlaceUIWindow):
		*ppv = s.frame()
	case riid.Equals(&IID_IOleControlSite):
		*ppv = s.controlSite()
	case riid.Equals(&IID_IDispatch):
		*ppv = s.ambient()
	default:
		return E_NOINTERFACE
	}
	s.ref++
	return S_OK
}

func (s *oleSite) hwnd() uintptr {
	if s.app == nil {
		return 0
	}
	return s.app.hwnd
}

// ---------------------------------------------------------------------------
// Vtables
// ---------------------------------------------------------------------------

var (
	vtblOleClientSite   []uintptr
	vtblOleInPlaceSite  []uintptr
	vtblOleInPlaceFrame []uintptr
	vtblOleControlSite  []uintptr
	vtblAmbientDispatch []uintptr
)

// cbQueryInterface / cbAddRef / cbRelease are shared across every vtable; the
// site is found by the interface pointer, so no pointer arithmetic is needed.
func cbQueryInterface(this uintptr, riid *GUID, ppv *uintptr) uintptr {
	s := lookupSite(this)
	if s == nil {
		return hres(E_POINTER)
	}
	return hres(s.queryInterface(riid, ppv))
}

func cbAddRef(this uintptr) uintptr {
	s := lookupSite(this)
	if s == nil {
		return 1
	}
	s.ref++
	return uintptr(s.ref)
}

func cbRelease(this uintptr) uintptr {
	s := lookupSite(this)
	if s == nil {
		return 0
	}
	s.ref--
	if s.ref < 0 {
		s.ref = 0
	}
	return uintptr(s.ref)
}

func cbReturn(hr HRESULT) func(uintptr) uintptr {
	return func(uintptr) uintptr { return hres(hr) }
}

func init() {
	// --- IOleClientSite ---------------------------------------------------
	vtblOleClientSite = []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
		syscall.NewCallback(cbReturn(E_NOTIMPL)), // SaveObject
		syscall.NewCallback(func(this, assign, which uintptr, ppmk *uintptr) uintptr {
			if ppmk != nil {
				*ppmk = 0
			}
			return hres(E_NOTIMPL)
		}), // GetMoniker
		syscall.NewCallback(func(this uintptr, ppContainer *uintptr) uintptr {
			if ppContainer != nil {
				*ppContainer = 0
			}
			return hres(E_NOINTERFACE)
		}), // GetContainer
		syscall.NewCallback(cbReturn(S_OK)), // ShowObject
		syscall.NewCallback(func(this, fShow uintptr) uintptr {
			return hres(S_OK)
		}), // OnShowWindow
		syscall.NewCallback(cbReturn(E_NOTIMPL)), // RequestNewObjectLayout
	}

	// --- IOleInPlaceSite --------------------------------------------------
	vtblOleInPlaceSite = []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
		syscall.NewCallback(func(this uintptr, phwnd *uintptr) uintptr { // GetWindow
			s := lookupSite(this)
			if s == nil || phwnd == nil {
				return hres(E_POINTER)
			}
			*phwnd = s.hwnd()
			return hres(S_OK)
		}),
		syscall.NewCallback(func(this, enter uintptr) uintptr { // ContextSensitiveHelp
			return hres(E_NOTIMPL)
		}),
		syscall.NewCallback(cbReturn(S_OK)), // CanInPlaceActivate
		syscall.NewCallback(cbReturn(S_OK)), // OnInPlaceActivate
		syscall.NewCallback(cbReturn(S_OK)), // OnUIActivate
		syscall.NewCallback(cbGetWindowContext),
		syscall.NewCallback(func(this, extent uintptr) uintptr { // Scroll(SIZE by value)
			return hres(E_NOTIMPL)
		}),
		syscall.NewCallback(func(this, undoable uintptr) uintptr { // OnUIDeactivate
			return hres(S_OK)
		}),
		syscall.NewCallback(cbReturn(S_OK)),      // OnInPlaceDeactivate
		syscall.NewCallback(cbReturn(S_OK)),      // DiscardUndoState
		syscall.NewCallback(cbReturn(E_NOTIMPL)), // DeactivateAndUndo
		syscall.NewCallback(cbOnPosRectChange),
	}

	// --- IOleInPlaceFrame -------------------------------------------------
	vtblOleInPlaceFrame = []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
		syscall.NewCallback(func(this uintptr, phwnd *uintptr) uintptr { // GetWindow
			s := lookupSite(this)
			if s == nil || phwnd == nil {
				return hres(E_POINTER)
			}
			*phwnd = s.hwnd()
			return hres(S_OK)
		}),
		syscall.NewCallback(func(this, enter uintptr) uintptr { return hres(E_NOTIMPL) }),  // ContextSensitiveHelp
		syscall.NewCallback(func(this, rect uintptr) uintptr { return hres(E_NOTIMPL) }),   // GetBorder
		syscall.NewCallback(func(this, widths uintptr) uintptr { return hres(E_NOTIMPL) }), // RequestBorderSpace
		syscall.NewCallback(func(this, widths uintptr) uintptr { return hres(E_NOTIMPL) }), // SetBorderSpace
		syscall.NewCallback(cbSetActiveObject),
		syscall.NewCallback(func(this, hmenu, widths uintptr) uintptr { return hres(E_NOTIMPL) }),    // InsertMenus
		syscall.NewCallback(func(this, hmenu, holemenu, hwnd uintptr) uintptr { return hres(S_OK) }), // SetMenu
		syscall.NewCallback(func(this, hmenu uintptr) uintptr { return hres(S_OK) }),                 // RemoveMenus
		syscall.NewCallback(func(this, text uintptr) uintptr { return hres(S_OK) }),                  // SetStatusText
		syscall.NewCallback(func(this, enable uintptr) uintptr { return hres(S_OK) }),                // EnableModeless
		syscall.NewCallback(func(this, msg, id uintptr) uintptr { return hres(S_FALSE) }),            // TranslateAccelerator
	}

	// --- IOleControlSite --------------------------------------------------
	vtblOleControlSite = []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
		syscall.NewCallback(cbReturn(S_OK)),                                         // OnControlInfoChanged
		syscall.NewCallback(func(this, lock uintptr) uintptr { return hres(S_OK) }), // LockInPlaceActive
		syscall.NewCallback(func(this uintptr, ppDisp *uintptr) uintptr { // GetExtendedControl
			if ppDisp != nil {
				*ppDisp = 0
			}
			return hres(E_NOTIMPL)
		}),
		syscall.NewCallback(func(this, himetric, container, flags uintptr) uintptr { // TransformCoords
			return hres(E_NOTIMPL)
		}),
		syscall.NewCallback(func(this uintptr, msg *MSG, modifiers uint32) uintptr { // TranslateAccelerator
			return hres(S_FALSE)
		}),
		syscall.NewCallback(cbOnFocus),
		syscall.NewCallback(cbReturn(E_NOTIMPL)), // ShowPropertyFrame
	}

	// --- IDispatch (ambient properties) -----------------------------------
	// Returning DISP_E_MEMBERNOTFOUND for every ambient property makes the
	// control fall back to its own defaults, which is what we want.
	vtblAmbientDispatch = []uintptr{
		syscall.NewCallback(cbQueryInterface),
		syscall.NewCallback(cbAddRef),
		syscall.NewCallback(cbRelease),
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
		syscall.NewCallback(func(this, dispid, riid, lcid, flags, params, result, excep, argErr uintptr) uintptr {
			return hres(DISP_E_MEMBERNOTFOUND)
		}), // Invoke
	}
}

// cbGetWindowContext implements IOleInPlaceSite::GetWindowContext.
func cbGetWindowContext(this uintptr, ppFrame, ppDoc *uintptr, posRect, clipRect *RECT, frameInfo *OLEINPLACEFRAMEINFO) uintptr {
	s := lookupSite(this)
	if s == nil {
		return hres(E_UNEXPECTED)
	}
	if ppFrame != nil {
		s.ref++ // the control owns a reference to the frame
		*ppFrame = s.frame()
	}
	if ppDoc != nil {
		*ppDoc = 0
	}
	rc := clientRect(s.hwnd())
	if posRect != nil {
		*posRect = rc
	}
	if clipRect != nil {
		*clipRect = rc
	}
	if frameInfo != nil {
		frameInfo.Cb = uint32(unsafe.Sizeof(OLEINPLACEFRAMEINFO{}))
		frameInfo.FMDIApp = 0
		frameInfo.HwndFrame = s.hwnd()
		frameInfo.Haccel = 0
		frameInfo.CAccelEntries = 0
	}
	return hres(S_OK)
}

// cbOnPosRectChange implements IOleInPlaceSite::OnPosRectChange.
func cbOnPosRectChange(this uintptr, posRect *RECT) uintptr {
	s := lookupSite(this)
	if s == nil || s.app == nil || posRect == nil {
		return hres(S_OK)
	}
	s.app.setControlRect(*posRect)
	return hres(S_OK)
}

// cbSetActiveObject implements IOleInPlaceFrame::SetActiveObject and caches the
// control's IOleInPlaceActiveObject for keyboard forwarding.
func cbSetActiveObject(this, active uintptr, name *uint16) uintptr {
	s := lookupSite(this)
	if s == nil {
		return hres(S_OK)
	}
	safeRelease(&s.activeObject)
	if active != 0 {
		unkAddRef(active)
		s.activeObject = active
	}
	return hres(S_OK)
}

// cbOnFocus implements IOleControlSite::OnFocus.
func cbOnFocus(this, gotFocus uintptr) uintptr {
	return hres(S_OK)
}
