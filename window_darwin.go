package main

import (
	"errors"
	"runtime"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

type cocoaPoint struct{ X, Y float64 }
type cocoaSize struct{ Width, Height float64 }
type cocoaRect struct {
	Origin cocoaPoint
	Size   cocoaSize
}

func init() { runtime.LockOSThread() }

func cocoaFrame(x, y, width, height float64) cocoaRect {
	return cocoaRect{cocoaPoint{x, y}, cocoaSize{width, height}}
}
func obj(class string) objc.ID { return objc.ID(objc.GetClass(class)) }
func send(id objc.ID, selector string, args ...any) objc.ID {
	return id.Send(objc.RegisterName(selector), args...)
}
func text(value string) objc.ID {
	return send(obj("NSString"), "stringWithUTF8String:", value)
}
func tint(r, g, b float64) objc.ID {
	return send(obj("NSColor"), "colorWithSRGBRed:green:blue:alpha:", r, g, b, 1.0)
}

type desktopMac struct {
	app                                                              *launcher
	host                                                             string
	application, window                                              objc.ID
	status, version, profile, progress, install, play, check, signin objc.ID
	rendered                                                         status
}

func desktopWindow(app *launcher, host string) error {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
		return err
	}
	pool := send(send(obj("NSAutoreleasePool"), "alloc"), "init")
	defer send(pool, "drain")
	w := &desktopMac{app: app, host: host}
	w.application = send(obj("NSApplication"), "sharedApplication")
	send(w.application, "setActivationPolicy:", 0)
	w.window = send(send(obj("NSWindow"), "alloc"), "initWithContentRect:styleMask:backing:defer:", cocoaFrame(0, 0, 620, 420), 7, 2, false)
	if w.window == 0 {
		return errors.New("Could not open the launcher window.")
	}
	defer send(w.window, "release")
	send(w.window, "setReleasedWhenClosed:", false)
	send(w.window, "setTitle:", text("Shipping Lanes Launcher"))
	send(w.window, "setAppearance:", send(obj("NSAppearance"), "appearanceNamed:", text("NSAppearanceNameDarkAqua")))
	send(w.window, "setBackgroundColor:", tint(.06, .11, .24))

	method := func(name string, fn func()) objc.MethodDef {
		return objc.MethodDef{Cmd: objc.RegisterName(name), Fn: func(_ objc.ID, _ objc.SEL, _ objc.ID) { fn() }}
	}
	delegateClass, err := objc.RegisterClass("ShippingLanesDesktopController", objc.GetClass("NSObject"), nil, nil, []objc.MethodDef{
		method("updateLauncher:", w.refresh),
		method("downloadGame:", func() { w.action("install") }),
		method("playGame:", func() { w.action("play") }),
		method("checkUpdates:", func() { w.action("check") }),
		method("signIn:", func() {
			if w.app.snapshot().Username == "" {
				w.action("signin")
			} else {
				w.action("signout")
			}
		}),
		method("closeLauncher:", func() { send(w.window, "close") }),
		method("windowWillClose:", func() { send(w.application, "stop:", objc.ID(0)) }),
	})
	if err != nil {
		return err
	}
	target := send(send(objc.ID(delegateClass), "alloc"), "init")
	defer send(target, "release")
	send(w.window, "setDelegate:", target)
	w.menu(target)
	w.controls(target)
	w.refresh()
	timer := send(obj("NSTimer"), "scheduledTimerWithTimeInterval:target:selector:userInfo:repeats:", .25, target, objc.RegisterName("updateLauncher:"), objc.ID(0), true)
	defer send(timer, "invalidate")
	send(w.window, "center")
	send(w.window, "makeKeyAndOrderFront:", objc.ID(0))
	send(w.application, "activateIgnoringOtherApps:", true)
	send(w.application, "run")
	return nil
}

func (w *desktopMac) menu(target objc.ID) {
	menu := send(send(obj("NSMenu"), "alloc"), "init")
	group := send(send(obj("NSMenuItem"), "alloc"), "init")
	appMenu := send(send(obj("NSMenu"), "alloc"), "init")
	quit := send(send(obj("NSMenuItem"), "alloc"), "initWithTitle:action:keyEquivalent:", text("Quit Shipping Lanes Launcher"), objc.RegisterName("closeLauncher:"), text("q"))
	send(quit, "setTarget:", target)
	send(appMenu, "addItem:", quit)
	send(group, "setSubmenu:", appMenu)
	send(menu, "addItem:", group)
	send(w.application, "setMainMenu:", menu)
	send(quit, "release")
	send(appMenu, "release")
	send(group, "release")
	send(menu, "release")
}

func (w *desktopMac) controls(target objc.ID) {
	content := send(w.window, "contentView")
	add := func(control objc.ID) objc.ID {
		send(content, "addSubview:", control)
		send(control, "release")
		return control
	}
	frame := func(class string, x, y, width, height float64) objc.ID {
		return send(send(obj(class), "alloc"), "initWithFrame:", cocoaFrame(x, y, width, height))
	}
	masthead := frame("NSBox", 0, 275, 620, 145)
	send(masthead, "setBoxType:", 4)
	send(masthead, "setBorderType:", 0)
	send(masthead, "setFillColor:", tint(.039, .275, .682))
	add(masthead)
	if len(brandPNG) > 0 {
		data := send(obj("NSData"), "dataWithBytes:length:", unsafe.Pointer(&brandPNG[0]), uint(len(brandPNG)))
		image := send(send(obj("NSImage"), "alloc"), "initWithData:", data)
		runtime.KeepAlive(brandPNG)
		logo := frame("NSImageView", 171, 287, 278, 121)
		send(logo, "setImageScaling:", 3)
		send(logo, "setImage:", image)
		add(logo)
		send(image, "release")
	}
	label := func(x, y, width, height, size float64) objc.ID {
		view := frame("NSTextField", x, y, width, height)
		send(view, "setEditable:", false)
		send(view, "setSelectable:", false)
		send(view, "setBordered:", false)
		send(view, "setDrawsBackground:", false)
		send(view, "setFont:", send(obj("NSFont"), "systemFontOfSize:", size))
		send(view, "setTextColor:", tint(1, .968, .878))
		return add(view)
	}
	button := func(title, selector string, x, y, width float64) objc.ID {
		view := frame("NSButton", x, y, width, 40)
		send(view, "setTitle:", text(title))
		send(view, "setBezelStyle:", 1)
		send(view, "setTarget:", target)
		send(view, "setAction:", objc.RegisterName(selector))
		return add(view)
	}
	w.status = label(34, 218, 552, 30, 17)
	w.version = label(34, 187, 552, 27, 14)
	w.progress = frame("NSProgressIndicator", 34, 165, 552, 15)
	send(w.progress, "setIndeterminate:", false)
	send(w.progress, "setMinValue:", 0.0)
	send(w.progress, "setMaxValue:", 100.0)
	add(w.progress)
	w.install = button("Download game", "downloadGame:", 34, 108, 174)
	w.play = button("Play", "playGame:", 219, 108, 146)
	w.check = button("Check for updates", "checkUpdates:", 376, 108, 210)
	w.profile = label(34, 33, 310, 30, 15)
	w.signin = button("Sign in or create an account", "signIn:", 353, 29, 233)
}

func (w *desktopMac) action(action string) {
	w.app.action(action, w.host)
	w.refresh()
}
func (w *desktopMac) refresh() {
	state := w.app.snapshot()
	if state == w.rendered {
		return
	}
	w.rendered = state
	pool := send(send(obj("NSAutoreleasePool"), "alloc"), "init")
	defer send(pool, "drain")
	send(w.status, "setStringValue:", text(state.Message))
	send(w.version, "setStringValue:", text(state.versions()))
	send(w.profile, "setStringValue:", text(state.profile()))
	send(w.signin, "setTitle:", text(state.signInTitle()))
	send(w.install, "setTitle:", text(state.installTitle()))
	send(w.install, "setEnabled:", state.CanInstall)
	send(w.play, "setEnabled:", state.Installed != "" && state.Username != "")
	send(w.check, "setEnabled:", !state.Busy)
	send(w.progress, "setHidden:", !state.Busy || state.Progress == 0)
	send(w.progress, "setDoubleValue:", float64(state.Progress))
}

func showLaunchError(msg string) {
	if _, err := purego.Dlopen("/System/Library/Frameworks/AppKit.framework/AppKit", purego.RTLD_GLOBAL|purego.RTLD_LAZY); err != nil {
		return
	}
	pool := send(send(obj("NSAutoreleasePool"), "alloc"), "init")
	defer send(pool, "drain")
	send(obj("NSApplication"), "sharedApplication")
	alert := send(send(obj("NSAlert"), "alloc"), "init")
	defer send(alert, "release")
	send(alert, "setMessageText:", text("Shipping Lanes Launcher"))
	send(alert, "setInformativeText:", text(msg))
	send(alert, "runModal")
}
