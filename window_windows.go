package main

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	_ "image/png"
	"runtime"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	controls = syscall.NewLazyDLL("comctl32.dll")
)

func win(dll *syscall.LazyDLL, name string, args ...any) uintptr {
	var words [16]uintptr
	for i, arg := range args {
		switch value := arg.(type) {
		case uintptr:
			words[i] = value
		case int:
			words[i] = uintptr(value)
		case uint32:
			words[i] = uintptr(value)
		case *uint16:
			words[i] = uintptr(unsafe.Pointer(value))
		case unsafe.Pointer:
			words[i] = uintptr(value)
		default:
			panic("unsupported Win32 argument")
		}
	}
	result, _, _ := dll.NewProc(name).Call(words[:len(args)]...)
	runtime.KeepAlive(args)
	return result
}
func wide(text string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(text)
	return p
}
func rgb(r, g, b byte) uintptr { return uintptr(r) | uintptr(g)<<8 | uintptr(b)<<16 }

type rect struct{ left, top, right, bottom int32 }
type point struct{ x, y int32 }
type wndClass struct {
	style                                           uint32
	proc                                            uintptr
	classExtra, windowExtra                         int32
	instance, icon, cursor, background, menu, class uintptr
}
type message struct {
	hwnd           uintptr
	number         uint32
	wparam, lparam uintptr
	timestamp      uint32
	at             point
	private        uint32
}
type paintInfo struct {
	dc              uintptr
	erase           int32
	area            rect
	restore, update int32
	reserved        [32]byte
}
type bitmapHeader struct {
	size                     uint32
	width, height            int32
	planes, bits             uint16
	compression, sizeImage   uint32
	xResolution, yResolution int32
	colors, important        uint32
}
type desktopWin struct {
	app *launcher

	hwnd, brush, font, instance             uintptr
	status, version, profile, progress      uintptr
	install, play, check, signin, uninstall uintptr
	frameScale                              int
	pixels                                  []byte
	imageWidth, imageHeight                 int
	rendered                                status
}

const (
	wDestroy     = 0x0002
	wPaint       = 0x000f
	wCommand     = 0x0111
	wTimer       = 0x0113
	wColorStatic = 0x0138
	wSetFont     = 0x0030
	idDownload   = 101
	idPlay       = 102
	idCheck      = 103
	idSignIn     = 104
	idUninstall  = 105
)

func desktopWindow(app *launcher) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	_ = win(user32, "SetProcessDPIAware")
	win(controls, "InitCommonControls")
	w := &desktopWin{app: app, frameScale: 96}
	if dpi := user32.NewProc("GetDpiForSystem"); dpi.Find() == nil {
		if value, _, _ := dpi.Call(); value != 0 {
			w.frameScale = int(value)
		}
	}
	w.instance = win(kernel32, "GetModuleHandleW", 0)
	w.brush = win(gdi32, "CreateSolidBrush", rgb(15, 29, 61))
	w.font = win(gdi32, "CreateFontW", uintptr(-int64(w.px(18))), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 5, 0, wide("Segoe UI"))
	defer win(gdi32, "DeleteObject", w.font)
	defer win(gdi32, "DeleteObject", w.brush)
	w.decodeLogo()
	cursor := win(user32, "LoadCursorW", 0, 32512)
	className := wide("ShippingLanesDesktop")
	class := wndClass{proc: syscall.NewCallback(w.windowProc), instance: w.instance, cursor: cursor, background: w.brush, class: uintptr(unsafe.Pointer(className))}
	registered := win(user32, "RegisterClassW", unsafe.Pointer(&class))
	runtime.KeepAlive(className)
	if registered == 0 {
		return errors.New("Could not register the launcher window.")
	}
	const style = 0x00ca0000
	frame := rect{right: int32(w.px(620)), bottom: int32(w.px(420))}
	win(user32, "AdjustWindowRectEx", unsafe.Pointer(&frame), style, 0, 0)
	x := (int(win(user32, "GetSystemMetrics", 0)) - int(frame.right-frame.left)) / 2
	y := (int(win(user32, "GetSystemMetrics", 1)) - int(frame.bottom-frame.top)) / 2
	w.hwnd = win(user32, "CreateWindowExW", 0, className, wide("Shipping Lanes Launcher"), style, uintptr(x), uintptr(y), uintptr(frame.right-frame.left), uintptr(frame.bottom-frame.top), 0, 0, w.instance, 0)
	if w.hwnd == 0 {
		return errors.New("Could not open the launcher window.")
	}
	if !w.makeControls() {
		win(user32, "DestroyWindow", w.hwnd)
		return errors.New("Could not create the launcher controls.")
	}
	w.refresh()
	win(user32, "SetTimer", w.hwnd, 1, 250, 0)
	win(user32, "ShowWindow", w.hwnd, 5)
	win(user32, "UpdateWindow", w.hwnd)
	var msg message
	for {
		received := int32(win(user32, "GetMessageW", unsafe.Pointer(&msg), 0, 0, 0))
		if received == -1 {
			return errors.New("The launcher window stopped responding.")
		}
		if received == 0 {
			return nil
		}
		if win(user32, "IsDialogMessageW", w.hwnd, unsafe.Pointer(&msg)) == 0 {
			win(user32, "TranslateMessage", unsafe.Pointer(&msg))
			win(user32, "DispatchMessageW", unsafe.Pointer(&msg))
		}
	}
}

func (w *desktopWin) px(n int) int { return n * w.frameScale / 96 }

func (w *desktopWin) child(kind, text string, x, y, width, height int, id, style uintptr) uintptr {
	control := win(user32, "CreateWindowExW", 0, wide(kind), wide(text), 0x50000000|style,
		uintptr(w.px(x)), uintptr(w.px(y)), uintptr(w.px(width)), uintptr(w.px(height)), w.hwnd, id, w.instance, 0)
	win(user32, "SendMessageW", control, wSetFont, w.font, 1)
	return control
}
func (w *desktopWin) makeControls() bool {
	w.status = w.child("STATIC", "", 35, 167, 550, 40, 0, 0)
	w.version = w.child("STATIC", "", 35, 209, 550, 24, 0, 0)
	w.progress = w.child("msctls_progress32", "", 35, 239, 550, 12, 0, 1)
	w.install = w.child("BUTTON", "Download game", 35, 270, 172, 42, idDownload, 0x10000)
	w.play = w.child("BUTTON", "Play", 219, 270, 145, 42, idPlay, 0x10001)
	w.check = w.child("BUTTON", "Check for updates", 376, 270, 209, 42, idCheck, 0x10000)
	w.profile = w.child("STATIC", "", 35, 319, 550, 24, 0, 0)
	w.signin = w.child("BUTTON", "Sign in or create an account", 35, 354, 250, 42, idSignIn, 0x10000)
	w.uninstall = w.child("BUTTON", "Uninstall game", 373, 354, 212, 42, idUninstall, 0x10000)
	return w.uninstall != 0 && w.status != 0 && w.version != 0 && w.progress != 0 && w.install != 0 && w.play != 0 && w.check != 0 && w.profile != 0 && w.signin != 0
}
func (w *desktopWin) refresh() {
	state := w.app.snapshot()
	if state == w.rendered {
		return
	}
	w.rendered = state
	win(user32, "SetWindowTextW", w.status, wide(state.Message))
	win(user32, "SetWindowTextW", w.version, wide(state.versions()))
	win(user32, "SetWindowTextW", w.profile, wide(state.profile()))
	win(user32, "SetWindowTextW", w.signin, wide(state.signInTitle()))
	win(user32, "SetWindowTextW", w.install, wide(state.installTitle()))
	win(user32, "EnableWindow", w.install, flag(state.CanInstall))
	win(user32, "EnableWindow", w.play, flag(state.Installed != "" && state.Username != ""))
	win(user32, "EnableWindow", w.check, flag(!state.Busy))
	win(user32, "EnableWindow", w.uninstall, flag(!state.Busy && state.Installed != ""))
	win(user32, "ShowWindow", w.progress, 5*flag(state.Busy && state.Progress > 0))
	win(user32, "SendMessageW", w.progress, 0x0402, uintptr(state.Progress), 0)
}

func (w *desktopWin) windowProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wDestroy:
		win(user32, "KillTimer", hwnd, 1)
		win(user32, "PostQuitMessage", 0)
		return 0
	case wTimer:
		w.refresh()
		return 0
	case wCommand:
		var action string
		switch wp & 0xffff {
		case idDownload:
			action = "install"
		case idPlay:
			action = "play"
		case idCheck:
			action = "check"
		case idUninstall:
			answer := win(user32, "MessageBoxW", w.hwnd, wide("Remove the installed Shipping Lanes game files? Your saves will be kept."), wide("Uninstall game"), 0x124)
			if answer == 6 {
				action = "uninstall"
			}
		case idSignIn:
			action = "signin"
			if w.app.snapshot().Username != "" {
				action = "signout"
			}
		}
		if action != "" {
			w.app.action(action)
			w.refresh()
			return 0
		}
	case wColorStatic:
		win(gdi32, "SetTextColor", wp, rgb(255, 247, 224))
		win(gdi32, "SetBkColor", wp, rgb(15, 29, 61))
		return w.brush
	case wPaint:
		var info paintInfo
		dc := win(user32, "BeginPaint", hwnd, unsafe.Pointer(&info))
		w.paint(dc)
		win(user32, "EndPaint", hwnd, unsafe.Pointer(&info))
		return 0
	}
	return win(user32, "DefWindowProcW", hwnd, uintptr(msg), wp, lp)
}
func (w *desktopWin) decodeLogo() {
	img, _, err := image.Decode(bytes.NewReader(brandPNG))
	if err != nil {
		return
	}
	bg := image.NewRGBA(img.Bounds())
	draw.Draw(bg, bg.Bounds(), image.NewUniform(color.RGBA{10, 70, 174, 255}), image.Point{}, draw.Src)
	draw.Draw(bg, bg.Bounds(), img, img.Bounds().Min, draw.Over)
	w.imageWidth, w.imageHeight = bg.Bounds().Dx(), bg.Bounds().Dy()
	w.pixels = bg.Pix
	for i := 0; i < len(w.pixels); i += 4 {
		w.pixels[i], w.pixels[i+2] = w.pixels[i+2], w.pixels[i]
	}
}
func (w *desktopWin) paint(dc uintptr) {
	top := rect{right: int32(w.px(620)), bottom: int32(w.px(145))}
	brush := win(gdi32, "CreateSolidBrush", rgb(10, 70, 174))
	win(user32, "FillRect", dc, unsafe.Pointer(&top), brush)
	win(gdi32, "DeleteObject", brush)
	if len(w.pixels) == 0 {
		return
	}
	width := w.px(278)
	height := width * w.imageHeight / w.imageWidth
	info := bitmapHeader{size: 40, width: int32(w.imageWidth), height: -int32(w.imageHeight), planes: 1, bits: 32}
	win(gdi32, "StretchDIBits", dc, uintptr((w.px(620)-width)/2), uintptr((w.px(145)-height)/2),
		uintptr(width), uintptr(height), 0, 0, uintptr(w.imageWidth), uintptr(w.imageHeight),
		unsafe.Pointer(&w.pixels[0]), unsafe.Pointer(&info), 0, 0x00cc0020)
}
func flag(value bool) uintptr {
	if value {
		return 1
	}
	return 0
}
func showLaunchError(msg string) {
	win(user32, "MessageBoxW", 0, wide(msg), wide("Shipping Lanes Launcher"), 0x10)
}
