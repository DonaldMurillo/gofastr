//go:build linux && (amd64 || arm64)

package linux

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/DonaldMurillo/gofastr/battery/desktop"
	"github.com/DonaldMurillo/gofastr/battery/desktop/internal/ffi"
)

func trayItemXML(iface string) string {
	windowID := "i"
	if iface == "org.freedesktop.StatusNotifierItem" {
		windowID = "u"
	}
	return `<node><interface name="` + iface + `">
<property name="Category" type="s" access="read"/><property name="Id" type="s" access="read"/>
<property name="Title" type="s" access="read"/><property name="Status" type="s" access="read"/>
<property name="WindowId" type="` + windowID + `" access="read"/><property name="IconThemePath" type="s" access="read"/>
<property name="Menu" type="o" access="read"/><property name="ItemIsMenu" type="b" access="read"/>
<property name="IconName" type="s" access="read"/><property name="IconPixmap" type="a(iiay)" access="read"/>
<property name="OverlayIconName" type="s" access="read"/><property name="OverlayIconPixmap" type="a(iiay)" access="read"/>
<property name="AttentionIconName" type="s" access="read"/><property name="AttentionIconPixmap" type="a(iiay)" access="read"/>
<property name="AttentionMovieName" type="s" access="read"/><property name="ToolTip" type="(sa(iiay)ss)" access="read"/>
<method name="Activate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="SecondaryActivate"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="ContextMenu"><arg type="i" direction="in"/><arg type="i" direction="in"/></method>
<method name="Scroll"><arg type="i" direction="in"/><arg type="s" direction="in"/></method>
<signal name="NewTitle"/><signal name="NewToolTip"/><signal name="NewIcon"/>
</interface></node>`
}

const trayMenuXML = `<node><interface name="com.canonical.dbusmenu">
<property name="Version" type="u" access="read"/><property name="TextDirection" type="s" access="read"/>
<property name="Status" type="s" access="read"/><property name="IconThemePath" type="as" access="read"/>
<method name="GetLayout"><arg type="i" direction="in"/><arg type="i" direction="in"/><arg type="as" direction="in"/><arg type="u" direction="out"/><arg type="(ia{sv}av)" direction="out"/></method>
<method name="GetGroupProperties"><arg type="ai" direction="in"/><arg type="as" direction="in"/><arg type="a(ia{sv})" direction="out"/></method>
<method name="GetProperty"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="out"/></method>
<method name="Event"><arg type="i" direction="in"/><arg type="s" direction="in"/><arg type="v" direction="in"/><arg type="u" direction="in"/></method>
<method name="EventGroup"><arg type="a(isvu)" direction="in"/><arg type="ai" direction="out"/></method>
<method name="AboutToShow"><arg type="i" direction="in"/><arg type="b" direction="out"/></method>
<method name="AboutToShowGroup"><arg type="ai" direction="in"/><arg type="ai" direction="out"/><arg type="ai" direction="out"/></method>
<signal name="LayoutUpdated"><arg type="u"/><arg type="i"/></signal>
</interface></node>`

type trayEndpoint interface {
	property(iface, name string) uintptr
	method(iface, name string, args uintptr) (uintptr, error)
}
type trayEndpointRecord struct {
	endpoint trayEndpoint
	refs     atomic.Int32
}

var trayEndpointSequence atomic.Uintptr
var trayEndpoints sync.Map
var trayLiveClosures atomic.Int64

func registerTrayEndpoint(connection uintptr, path, xml, iface string, endpoint trayEndpoint) (uint32, error) {
	var nativeError uintptr
	x, xp := activationCString(xml)
	node := trayCall("g_dbus_node_info_new_for_xml", xp, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(x)
	if node == 0 {
		return 0, trayNativeError("parse tray interface", nativeError)
	}
	defer trayCall("g_dbus_node_info_unref", node)
	i, ip := activationCString(iface)
	info := trayCall("g_dbus_node_info_lookup_interface", node, ip)
	runtime.KeepAlive(i)
	if info == 0 {
		return 0, errors.New("desktop/linux: tray interface missing from introspection")
	}
	token := trayEndpointSequence.Add(1)
	record := &trayEndpointRecord{endpoint: endpoint}
	record.refs.Store(2)
	trayEndpoints.Store(token, record)
	method := trayClosure(token, trayMethodCallback)
	property := trayClosure(token, trayPropertyCallback)
	defer trayCall("g_closure_unref", method)
	defer trayCall("g_closure_unref", property)
	p, pp := activationCString(path)
	id := trayCall("g_dbus_connection_register_object_with_closures", connection, pp, info, method, property, 0, uintptr(unsafe.Pointer(&nativeError)))
	runtime.KeepAlive(p)
	if id == 0 {
		return 0, trayNativeError("register tray interface", nativeError)
	}
	return uint32(id), nil
}
func trayClosure(token, callback uintptr) uintptr {
	c := trayCall("g_cclosure_new", callback, 0, 0)
	trayLiveClosures.Add(1)
	trayCall("g_closure_add_finalize_notifier", c, token, trayClosureFinalized)
	trayCall("g_closure_ref", c)
	trayCall("g_closure_sink", c)
	trayCall("g_closure_set_meta_marshal", c, token, callback)
	return c
}

var trayClosureFinalized = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	trayLiveClosures.Add(-1)
	if v, ok := trayEndpoints.Load(a.Int[0]); ok {
		if v.(*trayEndpointRecord).refs.Add(-1) == 0 {
			trayEndpoints.Delete(a.Int[0])
		}
	}
	return 0
})
var trayPropertyCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	if a.Int[2] != 5 || a.Int[3] == 0 || a.Int[1] == 0 {
		return 0
	}
	if v, ok := trayEndpoints.Load(a.Int[5]); ok {
		record := v.(*trayEndpointRecord)
		result := record.endpoint.property(trayValueString(a.Int[3], 3), trayValueString(a.Int[3], 4))
		if result != 0 {
			trayCall("g_value_take_variant", a.Int[1], result)
		}
	}
	return 0
})
var trayMethodCallback = ffi.NewCallbackAnyThread(func(a *ffi.Args) uintptr {
	if a.Int[2] != 7 || a.Int[3] == 0 {
		return 0
	}
	invocation := trayCall("g_value_get_object", a.Int[3]+6*activationGValueSize)
	parameters := trayCall("g_value_get_variant", a.Int[3]+5*activationGValueSize)
	if trayCall("g_variant_get_size", parameters) > 64<<10 {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.InvalidArgs", "Tray request is too large")
		return 0
	}
	v, ok := trayEndpoints.Load(a.Int[5])
	if !ok {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.Disconnected", "Tray endpoint closed")
		return 0
	}
	result, err := v.(*trayEndpointRecord).endpoint.method(trayValueString(a.Int[3], 3), trayValueString(a.Int[3], 4), parameters)
	if err != nil {
		activationMethodError(invocation, "org.freedesktop.DBus.Error.InvalidArgs", "Invalid or unavailable tray operation")
		return 0
	}
	// This pre-2.84 API transfers one invocation ref; return consumes it.
	trayCall("g_dbus_method_invocation_return_value", invocation, result)
	return 0
})

func (b *trayBroker) property(iface, name string) uintptr {
	if iface == trayMenuInterface {
		switch name {
		case "Version":
			return trayCall("g_variant_new_uint32", 3)
		case "TextDirection":
			return trayString("ltr")
		case "Status":
			return trayString("normal")
		case "IconThemePath":
			return trayArray("s")
		}
		return 0
	}
	b.mu.Lock()
	title := b.title
	b.mu.Unlock()
	switch name {
	case "Category":
		return trayString("ApplicationStatus")
	case "Id":
		return trayString(b.model.id)
	case "Title":
		return trayString(title)
	case "Status":
		return trayString("Active")
	case "WindowId":
		if iface == "org.freedesktop.StatusNotifierItem" {
			return trayCall("g_variant_new_uint32", 0)
		}
		return trayInt(0)
	case "Menu":
		return trayObjectPath(trayMenuPath)
	case "ItemIsMenu":
		return trayBool(false)
	case "IconPixmap":
		return trayArray("(iiay)", trayTuple(trayInt(int32(b.model.width)), trayInt(int32(b.model.height)), trayBytes(b.model.argb)))
	case "OverlayIconPixmap", "AttentionIconPixmap":
		return trayArray("(iiay)")
	case "ToolTip":
		return trayTuple(trayString(""), trayArray("(iiay)"), trayString(title), trayString(b.model.tooltip))
	case "IconName", "IconThemePath", "OverlayIconName", "AttentionIconName", "AttentionMovieName":
		return trayString("")
	}
	return 0
}

func trayArgInt(args uintptr, index int) int32 {
	v := trayChild(args, index)
	defer trayUnref(v)
	return int32(trayCall("g_variant_get_int32", v))
}
func trayArgString(args uintptr, index int) (string, bool) {
	v := trayChild(args, index)
	defer trayUnref(v)
	return trayReadString(v, trayTextLimit)
}

var errTrayRequest = errors.New("invalid tray request")

func (b *trayBroker) method(iface, name string, args uintptr) (uintptr, error) {
	if iface != trayMenuInterface {
		switch name {
		case "Activate", "SecondaryActivate", "ContextMenu":
			// Hosts with DBusMenu support render the exported menu themselves.
			// A legacy ContextMenu request still restores the app, never hides it.
			if !b.enqueue(trayAction{plan: desktop.MenuItemPlan{Role: desktop.RoleShow}}) {
				return 0, errTrayRequest
			}
			return 0, nil
		case "Scroll":
			orientation, ok := trayArgString(args, 1)
			if !ok || (orientation != "vertical" && orientation != "horizontal") {
				return 0, errTrayRequest
			}
			return 0, nil
		}
		return 0, errTrayRequest
	}
	switch name {
	case "GetLayout":
		node := b.model.nodes[trayArgInt(args, 0)]
		depth := trayArgInt(args, 1)
		properties := trayChild(args, 2)
		defer trayUnref(properties)
		filter, err := trayPropertyFilter(properties)
		if err != nil || node == nil || depth < -1 || depth > trayDepthLimit {
			return 0, errTrayRequest
		}
		return trayTuple(trayCall("g_variant_new_uint32", 1), trayLayout(node, depth, filter)), nil
	case "GetGroupProperties":
		ids := trayChild(args, 0)
		defer trayUnref(ids)
		properties := trayChild(args, 1)
		defer trayUnref(properties)
		filter, err := trayPropertyFilter(properties)
		if err != nil || trayCall("g_variant_n_children", ids) > trayNodeLimit {
			return 0, errTrayRequest
		}
		var rows []uintptr
		count := int(trayCall("g_variant_n_children", ids))
		if count == 0 {
			for id := int32(0); id < int32(len(b.model.nodes)); id++ {
				rows = append(rows, trayTuple(trayInt(id), trayNodeProperties(b.model.nodes[id], filter)))
			}
		} else {
			for i := 0; i < count; i++ {
				id := trayArgInt(ids, i)
				if node := b.model.nodes[id]; node != nil {
					rows = append(rows, trayTuple(trayInt(id), trayNodeProperties(node, filter)))
				}
			}
		}
		return trayTuple(trayArray("(ia{sv})", rows...)), nil
	case "GetProperty":
		node := b.model.nodes[trayArgInt(args, 0)]
		property, ok := trayArgString(args, 1)
		if node == nil || !ok {
			return 0, errTrayRequest
		}
		v := trayNodeProperty(node, property)
		if v == 0 {
			return 0, errTrayRequest
		}
		return trayTuple(trayCall("g_variant_new_variant", v)), nil
	case "Event":
		return 0, b.event(args)
	case "EventGroup":
		events := trayChild(args, 0)
		defer trayUnref(events)
		n := int(trayCall("g_variant_n_children", events))
		if n > trayNodeLimit {
			return 0, errTrayRequest
		}
		var failed []uintptr
		for i := 0; i < n; i++ {
			event := trayChild(events, i)
			if b.event(event) != nil {
				failed = append(failed, trayInt(trayArgInt(event, 0)))
			}
			trayUnref(event)
		}
		return trayTuple(trayArray("i", failed...)), nil
	case "AboutToShow":
		if b.model.nodes[trayArgInt(args, 0)] == nil {
			return 0, errTrayRequest
		}
		return trayTuple(trayBool(false)), nil
	case "AboutToShowGroup":
		ids := trayChild(args, 0)
		defer trayUnref(ids)
		n := int(trayCall("g_variant_n_children", ids))
		if n > trayNodeLimit {
			return 0, errTrayRequest
		}
		var failed []uintptr
		for i := 0; i < n; i++ {
			id := trayArgInt(ids, i)
			if b.model.nodes[id] == nil {
				failed = append(failed, trayInt(id))
			}
		}
		return trayTuple(trayArray("i"), trayArray("i", failed...)), nil
	}
	return 0, errTrayRequest
}

func (b *trayBroker) event(args uintptr) error {
	id := trayArgInt(args, 0)
	event, ok := trayArgString(args, 1)
	node := b.model.nodes[id]
	if node == nil || !ok {
		return errTrayRequest
	}
	if event == "opened" || event == "closed" {
		return nil
	}
	if event != "clicked" || id == 0 || !node.enabled() || len(node.children) > 0 {
		return errTrayRequest
	}
	if !b.enqueue(trayAction{plan: node.plan, id: node.actionID}) {
		return errTrayRequest
	}
	return nil
}
func trayPropertyFilter(properties uintptr) (map[string]bool, error) {
	n := int(trayCall("g_variant_n_children", properties))
	if n > 32 {
		return nil, errTrayRequest
	}
	if n == 0 {
		return nil, nil
	}
	filter := make(map[string]bool, n)
	for i := 0; i < n; i++ {
		s, ok := trayArgString(properties, i)
		if !ok {
			return nil, errTrayRequest
		}
		filter[s] = true
	}
	return filter, nil
}
func trayNodeProperty(n *trayNode, key string) uintptr {
	switch key {
	case "type":
		if n.plan.Sep {
			return trayString("separator")
		}
		return trayString("standard")
	case "label":
		return trayString(strings.ReplaceAll(n.plan.Title, "_", "__"))
	case "enabled":
		return trayBool(n.enabled())
	case "visible":
		return trayBool(true)
	case "children-display":
		if len(n.children) > 0 {
			return trayString("submenu")
		}
	}
	return 0
}
func trayNodeProperties(n *trayNode, filter map[string]bool) uintptr {
	var entries []uintptr
	for _, key := range []string{"type", "label", "enabled", "visible", "children-display"} {
		if filter != nil && !filter[key] {
			continue
		}
		if value := trayNodeProperty(n, key); value != 0 {
			entries = append(entries, trayDictEntry(key, value))
		}
	}
	return trayArray("{sv}", entries...)
}
func trayLayout(n *trayNode, depth int32, filter map[string]bool) uintptr {
	var children []uintptr
	if depth != 0 {
		next := depth
		if next > 0 {
			next--
		}
		for _, child := range n.children {
			children = append(children, trayCall("g_variant_new_variant", trayLayout(child, next, filter)))
		}
	}
	return trayTuple(trayInt(n.id), trayNodeProperties(n, filter), trayArray("v", children...))
}
