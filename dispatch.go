//go:build windows

package main

import (
	"fmt"
	"unsafe"
)

const localeUserDefault = 0x0400 // LOCALE_USER_DEFAULT

var iidNull GUID

// disp is a thin wrapper around an IDispatch pointer. The zero value is nil.
type disp struct {
	p    uintptr
	name string // for diagnostics only
}

func (d disp) valid() bool { return d.p != 0 }

func (d disp) release() {
	if d.p != 0 {
		unkRelease(d.p)
	}
}

// memberID resolves a member name to its DISPID.
func (d disp) memberID(name string) (int32, HRESULT) {
	if d.p == 0 {
		return 0, E_POINTER
	}
	w := utf16Ptr(name)
	var id int32
	hr := comCallHR(d.p, 5,
		uintptr(unsafe.Pointer(&iidNull)),
		uintptr(unsafe.Pointer(&w)),
		1,
		localeUserDefault,
		uintptr(unsafe.Pointer(&id)))
	return id, hr
}

// has reports whether the object exposes a member with this name.
func (d disp) has(name string) bool {
	_, hr := d.memberID(name)
	return hr.OK()
}

// invokeID performs IDispatch::Invoke on an already resolved DISPID.
func (d disp) invokeID(id int32, flags uint16, args ...interface{}) (VARIANT, error) {
	if d.p == 0 {
		return VARIANT{}, fmt.Errorf("invoke on nil IDispatch")
	}

	// DISPPARAMS carries arguments in reverse order.
	vars := make([]VARIANT, len(args))
	for i, a := range args {
		v, err := newVariant(a)
		if err != nil {
			for j := 0; j < i; j++ {
				releaseVariant(&vars[j])
			}
			return VARIANT{}, err
		}
		vars[len(args)-1-i] = v
	}
	defer func() {
		for i := range vars {
			releaseVariant(&vars[i])
		}
	}()

	var dp DISPPARAMS
	dp.CArgs = uint32(len(vars))
	if len(vars) > 0 {
		dp.Rgvarg = &vars[0]
	}
	putID := int32(DISPID_PROPERTYPUT)
	if flags&(DISPATCH_PROPERTYPUT|DISPATCH_PROPERTYPUTREF) != 0 {
		dp.RgdispidNamedArgs = &putID
		dp.CNamedArgs = 1
	}

	var result VARIANT
	var ex EXCEPINFO
	var argErr uint32
	hr := comCallHR(d.p, 6,
		uintptr(id),
		uintptr(unsafe.Pointer(&iidNull)),
		localeUserDefault,
		uintptr(flags),
		uintptr(unsafe.Pointer(&dp)),
		uintptr(unsafe.Pointer(&result)),
		uintptr(unsafe.Pointer(&ex)),
		uintptr(unsafe.Pointer(&argErr)))
	if hr.Failed() {
		msg := ex.message()
		ex.free()
		if msg != "" {
			return VARIANT{}, fmt.Errorf("%s: %s", hr.Error(), msg)
		}
		return VARIANT{}, fmt.Errorf("%s", hr.Error())
	}
	return result, nil
}

func (d disp) invoke(name string, flags uint16, args ...interface{}) (VARIANT, error) {
	id, hr := d.memberID(name)
	if hr.Failed() {
		return VARIANT{}, &notSupportedError{member: name, object: d.name, hr: hr}
	}
	v, err := d.invokeID(id, flags, args...)
	if err != nil {
		return VARIANT{}, fmt.Errorf("%s.%s: %w", d.name, name, err)
	}
	return v, nil
}

// notSupportedError marks a member that this build of the control does not have.
type notSupportedError struct {
	member string
	object string
	hr     HRESULT
}

func (e *notSupportedError) Error() string {
	obj := e.object
	if obj == "" {
		obj = "control"
	}
	return fmt.Sprintf("%s does not expose %q (%s)", obj, e.member, e.hr.Error())
}

func isNotSupported(err error) bool {
	if err == nil {
		return false
	}
	_, ok := err.(*notSupportedError)
	return ok
}

// get reads a property.
func (d disp) get(name string, args ...interface{}) (VARIANT, error) {
	return d.invoke(name, DISPATCH_PROPERTYGET, args...)
}

// put writes a property.
func (d disp) put(name string, value interface{}) error {
	v, err := d.invoke(name, DISPATCH_PROPERTYPUT, value)
	if err != nil {
		return err
	}
	v.clear()
	return nil
}

// call invokes a method and discards any result.
func (d disp) call(name string, args ...interface{}) error {
	v, err := d.invoke(name, DISPATCH_METHOD, args...)
	if err != nil {
		return err
	}
	v.clear()
	return nil
}

// callRet invokes a method and returns its result.
func (d disp) callRet(name string, args ...interface{}) (VARIANT, error) {
	return d.invoke(name, DISPATCH_METHOD|DISPATCH_PROPERTYGET, args...)
}

// getInt reads an integer property.
func (d disp) getInt(name string) (int32, error) {
	v, err := d.get(name)
	if err != nil {
		return 0, err
	}
	defer v.clear()
	n, ok := v.Int()
	if !ok {
		return 0, fmt.Errorf("%s.%s: not an integer (vt=%d)", d.name, name, v.VT)
	}
	return n, nil
}

// getString reads a string property.
func (d disp) getString(name string, args ...interface{}) (string, error) {
	return d.stringWith(DISPATCH_PROPERTYGET, name, args...)
}

// callString invokes a method that returns a string. Members such as
// GetErrorDescription are methods, not properties, so they need
// DISPATCH_METHOD; dual interfaces reject a plain property get for those.
func (d disp) callString(name string, args ...interface{}) (string, error) {
	return d.stringWith(DISPATCH_METHOD|DISPATCH_PROPERTYGET, name, args...)
}

func (d disp) stringWith(flags uint16, name string, args ...interface{}) (string, error) {
	v, err := d.invoke(name, flags, args...)
	if err != nil {
		return "", err
	}
	defer v.clear()
	if v.VT != VT_BSTR {
		return fmt.Sprintf("%v", v.Value()), nil
	}
	return bstrToString(v.data[0]), nil
}

// sub fetches a property that returns another IDispatch. The caller owns the
// returned reference.
func (d disp) sub(name string) (disp, error) {
	v, err := d.get(name)
	if err != nil {
		return disp{}, err
	}
	if v.VT != VT_DISPATCH || v.data[0] == 0 {
		v.clear()
		return disp{}, fmt.Errorf("%s.%s: not an object (vt=%d)", d.name, name, v.VT)
	}
	// Ownership of the reference moves to the returned disp.
	return disp{p: v.data[0], name: d.name + "." + name}, nil
}

// subAny returns the first of names that resolves to an object, so that the
// newest interface revision available on this host is used.
func (d disp) subAny(names ...string) (disp, string, error) {
	var last error
	for _, n := range names {
		s, err := d.sub(n)
		if err == nil {
			return s, n, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("no candidates")
	}
	return disp{}, "", last
}
