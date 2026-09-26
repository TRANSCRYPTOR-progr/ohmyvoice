package ui

import (
	"fmt"
	"math"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")

	procGetModuleHandleW       = kernel32.NewProc("GetModuleHandleW")
	procRegisterClassExW       = user32.NewProc("RegisterClassExW")
	procCreateWindowExW        = user32.NewProc("CreateWindowExW")
	procDefWindowProcW         = user32.NewProc("DefWindowProcW")
	procShowWindow             = user32.NewProc("ShowWindow")
	procUpdateWindow           = user32.NewProc("UpdateWindow")
	procGetMessageW            = user32.NewProc("GetMessageW")
	procTranslateMessage       = user32.NewProc("TranslateMessage")
	procDispatchMessageW       = user32.NewProc("DispatchMessageW")
	procPostQuitMessage        = user32.NewProc("PostQuitMessage")
	procPostMessageW           = user32.NewProc("PostMessageW")
	procBeginPaint             = user32.NewProc("BeginPaint")
	procEndPaint               = user32.NewProc("EndPaint")
	procInvalidateRect         = user32.NewProc("InvalidateRect")
	procGetSystemMetrics       = user32.NewProc("GetSystemMetrics")
	procSetWindowRgn           = user32.NewProc("SetWindowRgn")
	procDestroyWindow          = user32.NewProc("DestroyWindow")
	procGetCursorPos           = user32.NewProc("GetCursorPos")
	procScreenToClient         = user32.NewProc("ScreenToClient")
	procSetTimer               = user32.NewProc("SetTimer")

	procCreateCompatibleDC     = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = gdi32.NewProc("SelectObject")
	procDeleteObject           = gdi32.NewProc("DeleteObject")
	procDeleteDC               = gdi32.NewProc("DeleteDC")
	procBitBlt                 = gdi32.NewProc("BitBlt")
	procCreateSolidBrush       = gdi32.NewProc("CreateSolidBrush")
	procCreateRoundRectRgn     = gdi32.NewProc("CreateRoundRectRgn")
	procCreatePen              = gdi32.NewProc("CreatePen")
	procRoundRect              = gdi32.NewProc("RoundRect")
	procEllipse                = gdi32.NewProc("Ellipse")
	procSetTextColor           = gdi32.NewProc("SetTextColor")
	procSetBkMode              = gdi32.NewProc("SetBkMode")
	procCreateFontW            = gdi32.NewProc("CreateFontW")
	procDrawTextW              = user32.NewProc("DrawTextW")
)

const (
	WS_POPUP         = 0x80000000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000

	WM_DESTROY    = 0x0002
	WM_PAINT      = 0x000F
	WM_CLOSE      = 0x0010
	WM_TIMER      = 0x0113
	WM_NCHITTEST  = 0x0084
	WM_LBUTTONUP  = 0x0202
	WM_RBUTTONUP  = 0x0205
	WM_MOUSEMOVE  = 0x0200
	WM_USER_STATE = 0x0401

	HTCAPTION     = 2
	HTCLIENT      = 1
	SRCCOPY       = 0x00CC0020
	TRANSPARENT   = 1
	DT_CENTER     = 0x00000001
	DT_VCENTER    = 0x00000004
	DT_SINGLELINE = 0x00000020
	PS_SOLID      = 0

	WinWidth  = 146
	WinHeight = 146

	SW_HIDE           = 0
	SW_SHOWNOACTIVATE = 4
)

type State int

const (
	StateIdle State = iota
	StateRecording
	StateTranscribing
	StateDone
	StateNoSpeech
	StateError
	StateSettings
)

type MiniWindow struct {
	mu             sync.Mutex
	state          State
	subtext        string
	recStart       time.Time
	animTick       int
	isCloseHover   bool
	hwnd           uintptr
	onClose        func()
	onOpenSettings func()
	resetTimer     *time.Timer
}

var currentWin *MiniWindow

type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type POINT struct {
	X, Y int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	FErase      int32
	RcPaint     RECT
	FRestore    int32
	FIncUpdate  int32
	RgbReserved [32]byte
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	Pt      POINT
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func rgb(r, g, b byte) uint32 {
	return uint32(r) | (uint32(g) << 8) | (uint32(b) << 16)
}

func New(onClose func(), onOpenSettings func()) *MiniWindow {
	w := &MiniWindow{
		state:          StateIdle,
		subtext:        "[Caps Lock]",
		onClose:        onClose,
		onOpenSettings: onOpenSettings,
	}
	currentWin = w
	return w
}

func (w *MiniWindow) Start() error {
	readyChan := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hInst, _, _ := procGetModuleHandleW.Call(0)
		className := utf16Ptr("OhMyVoice_MiniHUD")

		wc := WNDCLASSEXW{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			Style:         3, // CS_HREDRAW | CS_VREDRAW
			LpfnWndProc:   syscall.NewCallback(wndProc),
			HInstance:     hInst,
			LpszClassName: className,
		}

		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

		cxScreen, _, _ := procGetSystemMetrics.Call(0)
		cyScreen, _, _ := procGetSystemMetrics.Call(1)

		// Place by default near bottom-right corner above taskbar
		posX := int32(cxScreen) - WinWidth - 28
		posY := int32(cyScreen) - WinHeight - 68
		if posX < 10 {
			posX = 10
		}
		if posY < 10 {
			posY = 10
		}

		hwnd, _, err := procCreateWindowExW.Call(
			WS_EX_TOPMOST|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(utf16Ptr("OhMyVoice"))),
			WS_POPUP,
			uintptr(posX), uintptr(posY), WinWidth, WinHeight,
			0, 0, hInst, 0,
		)

		if hwnd == 0 {
			readyChan <- fmt.Errorf("failed to create UI window: %w", err)
			return
		}

		w.hwnd = hwnd

		// Modern rounded window corners
		rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, WinWidth, WinHeight, 28, 28)
		procSetWindowRgn.Call(hwnd, rgn, 1)

		// 30 FPS update timer for silky animations
		procSetTimer.Call(hwnd, 1, 33, 0)

		// Start hidden by default: only appears during dictation!
		procShowWindow.Call(hwnd, SW_HIDE)

		readyChan <- nil

		var msg MSG
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if ret == 0 || int32(ret) == -1 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}

		if w.onClose != nil {
			w.onClose()
		}
	}()

	return <-readyChan
}

func (w *MiniWindow) SetRecording() {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateRecording
	w.recStart = time.Now()
	w.subtext = "0.0 сек"
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}
}

func (w *MiniWindow) SetTranscribing() {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateTranscribing
	w.subtext = "Whisper CUDA"
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}
}

func (w *MiniWindow) SetDone(dur time.Duration) {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateDone
	w.subtext = fmt.Sprintf("%d мс", dur.Milliseconds())
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}

	w.scheduleReset(1200 * time.Millisecond)
}

func (w *MiniWindow) SetNoSpeech() {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateNoSpeech
	w.subtext = "Речь не найдена"
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}

	w.scheduleReset(1000 * time.Millisecond)
}

func (w *MiniWindow) SetIdle() {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateIdle
	w.subtext = "[Caps Lock]"
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procShowWindow.Call(hwnd, SW_HIDE)
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}
}

func (w *MiniWindow) SetError(msg string) {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateError
	w.subtext = msg
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}

	w.scheduleReset(2000 * time.Millisecond)
}

func (w *MiniWindow) SetSettings() {
	w.mu.Lock()
	if w.resetTimer != nil {
		w.resetTimer.Stop()
	}
	w.state = StateSettings
	w.subtext = "Настройки ⚙"
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procShowWindow.Call(hwnd, SW_SHOWNOACTIVATE)
		procPostMessageW.Call(hwnd, WM_USER_STATE, 0, 0)
	}

	w.scheduleReset(1600 * time.Millisecond)
}

func (w *MiniWindow) Close() {
	w.mu.Lock()
	hwnd := w.hwnd
	w.mu.Unlock()

	if hwnd != 0 {
		procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
	}
}

func (w *MiniWindow) scheduleReset(delay time.Duration) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.resetTimer = time.AfterFunc(delay, func() {
		w.SetIdle()
	})
}

func wndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0

	case WM_CLOSE:
		procDestroyWindow.Call(hwnd)
		return 0

	case WM_TIMER:
		if currentWin != nil {
			currentWin.mu.Lock()
			currentWin.animTick++
			state := currentWin.state
			recStart := currentWin.recStart
			currentWin.mu.Unlock()

			// Only trigger repaints during active animation states or mouse hovers to save CPU
			if state == StateRecording || state == StateTranscribing {
				if state == StateRecording && !recStart.IsZero() {
					sec := time.Since(recStart).Seconds()
					currentWin.mu.Lock()
					currentWin.subtext = fmt.Sprintf("%.1f сек", sec)
					currentWin.mu.Unlock()
				}
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0

	case WM_USER_STATE:
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0

	case WM_MOUSEMOVE:
		if currentWin != nil {
			x := int32(int16(lParam & 0xFFFF))
			y := int32(int16((lParam >> 16) & 0xFFFF))
			hover := (x >= WinWidth-32 && x <= WinWidth-8 && y >= 6 && y <= 30)
			if hover != currentWin.isCloseHover {
				currentWin.isCloseHover = hover
				procInvalidateRect.Call(hwnd, 0, 0)
			}
		}
		return 0

	case WM_LBUTTONUP:
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		if x >= WinWidth-32 && x <= WinWidth-8 && y >= 6 && y <= 30 {
			procPostMessageW.Call(hwnd, WM_CLOSE, 0, 0)
			return 0
		}
		return 0

	case WM_RBUTTONUP:
		if currentWin != nil && currentWin.onOpenSettings != nil {
			go currentWin.onOpenSettings()
		}
		return 0

	case WM_NCHITTEST:
		// Enable dragging anywhere inside the window except the close button
		var pt POINT
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		if pt.X >= WinWidth-32 && pt.X <= WinWidth-8 && pt.Y >= 6 && pt.Y <= 30 {
			return HTCLIENT
		}
		return HTCAPTION

	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			paintWindow(hdc)
			procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func paintWindow(hdc uintptr) {
	if currentWin == nil {
		return
	}

	currentWin.mu.Lock()
	state := currentWin.state
	sub := currentWin.subtext
	tick := currentWin.animTick
	closeHover := currentWin.isCloseHover
	currentWin.mu.Unlock()

	hdcMem, _, _ := procCreateCompatibleDC.Call(hdc)
	hbm, _, _ := procCreateCompatibleBitmap.Call(hdc, WinWidth, WinHeight)
	procSelectObject.Call(hdcMem, hbm)

	// Pure OLED black theme palette
	bgColor := rgb(0, 0, 0)
	innerCard := rgb(18, 18, 22)
	txtMain := rgb(255, 255, 255)
	txtSub := rgb(155, 155, 170)

	var accentCol uint32
	var statusTitle string
	var iconText string

	switch state {
	case StateRecording:
		accentCol = rgb(255, 75, 95) // Vivid Coral / Red
		statusTitle = "СЛУШАЮ..."
		iconText = "🎙"
	case StateTranscribing:
		accentCol = rgb(175, 125, 255) // Cyber Purple / Mauve
		statusTitle = "НЕЙРОСЕТЬ"
		iconText = "⚡"
	case StateDone:
		accentCol = rgb(60, 230, 130) // Neon Emerald Green
		statusTitle = "ГОТОВО!"
		iconText = "✨"
	case StateNoSpeech:
		accentCol = rgb(255, 195, 70) // Warm Amber
		statusTitle = "ТИШИНА"
		iconText = "⚠️"
	case StateError:
		accentCol = rgb(255, 60, 60) // Alert Red
		statusTitle = "ОШИБКА"
		iconText = "✕"
	case StateSettings:
		accentCol = rgb(255, 195, 70) // Warm Gold
		statusTitle = "НАСТРОЙКИ"
		iconText = "⚙"
	default: // StateIdle
		accentCol = rgb(90, 160, 255) // Soft Cyan / Blue
		statusTitle = "ГОТОВ"
		iconText = "🎙"
		if sub == "" {
			sub = "[Caps Lock]"
		}
	}

	// 1. Background fill with rounded corners
	hBgBrush, _, _ := procCreateSolidBrush.Call(uintptr(bgColor))
	hBorderPen, _, _ := procCreatePen.Call(PS_SOLID, 2, uintptr(accentCol))
	procSelectObject.Call(hdcMem, hBgBrush)
	procSelectObject.Call(hdcMem, hBorderPen)
	procRoundRect.Call(hdcMem, 1, 1, WinWidth-1, WinHeight-1, 28, 28)
	procDeleteObject.Call(hBgBrush)
	procDeleteObject.Call(hBorderPen)

	// 2. Center Icon circle (with pulse animation when recording / transcribing)
	pulseRadius := int32(24)
	if state == StateRecording {
		pulse := math.Sin(float64(tick)*0.25) * 3.5
		pulseRadius = int32(24 + pulse)
	} else if state == StateTranscribing {
		pulse := math.Sin(float64(tick)*0.35) * 2.0
		pulseRadius = int32(24 + pulse)
	}
	cx := int32(WinWidth / 2)
	cy := int32(52)

	hCardBrush, _, _ := procCreateSolidBrush.Call(uintptr(innerCard))
	hAccentPen, _, _ := procCreatePen.Call(PS_SOLID, 1, uintptr(accentCol))
	procSelectObject.Call(hdcMem, hCardBrush)
	procSelectObject.Call(hdcMem, hAccentPen)
	procEllipse.Call(hdcMem, uintptr(cx-pulseRadius), uintptr(cy-pulseRadius), uintptr(cx+pulseRadius), uintptr(cy+pulseRadius))
	procDeleteObject.Call(hCardBrush)
	procDeleteObject.Call(hAccentPen)

	// 3. Status indicator dot (top-left)
	hDotBrush, _, _ := procCreateSolidBrush.Call(uintptr(accentCol))
	hDotPen, _, _ := procCreatePen.Call(PS_SOLID, 1, uintptr(accentCol))
	procSelectObject.Call(hdcMem, hDotBrush)
	procSelectObject.Call(hdcMem, hDotPen)
	procEllipse.Call(hdcMem, 14, 14, 22, 22)
	procDeleteObject.Call(hDotBrush)
	procDeleteObject.Call(hDotPen)

	// 4. Close Button (top-right)
	procSetBkMode.Call(hdcMem, TRANSPARENT)
	if closeHover {
		procSetTextColor.Call(hdcMem, uintptr(rgb(255, 100, 120)))
	} else {
		procSetTextColor.Call(hdcMem, uintptr(rgb(100, 105, 130)))
	}
	hFontClose, _, _ := procCreateFontW.Call(
		13, 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	procSelectObject.Call(hdcMem, hFontClose)
	rcClose := RECT{Left: WinWidth - 30, Top: 7, Right: WinWidth - 10, Bottom: 26}
	procDrawTextW.Call(hdcMem, uintptr(unsafe.Pointer(utf16Ptr("✕"))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rcClose)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procDeleteObject.Call(hFontClose)

	// 5. Center Icon Glyph
	procSetTextColor.Call(hdcMem, uintptr(accentCol))
	hFontIcon, _, _ := procCreateFontW.Call(
		25, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI Emoji"))),
	)
	procSelectObject.Call(hdcMem, hFontIcon)
	rcIcon := RECT{Left: cx - 24, Top: cy - 20, Right: cx + 24, Bottom: cy + 19}
	procDrawTextW.Call(hdcMem, uintptr(unsafe.Pointer(utf16Ptr(iconText))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rcIcon)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procDeleteObject.Call(hFontIcon)

	// 6. Main Status Title
	procSetTextColor.Call(hdcMem, uintptr(txtMain))
	hFontTitle, _, _ := procCreateFontW.Call(
		14, 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	procSelectObject.Call(hdcMem, hFontTitle)
	rcTitle := RECT{Left: 6, Top: 90, Right: WinWidth - 6, Bottom: 110}
	procDrawTextW.Call(hdcMem, uintptr(unsafe.Pointer(utf16Ptr(statusTitle))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rcTitle)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procDeleteObject.Call(hFontTitle)

	// 7. Subtitle / Details
	procSetTextColor.Call(hdcMem, uintptr(txtSub))
	hFontSub, _, _ := procCreateFontW.Call(
		12, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	procSelectObject.Call(hdcMem, hFontSub)
	rcSub := RECT{Left: 6, Top: 112, Right: WinWidth - 6, Bottom: 134}
	procDrawTextW.Call(hdcMem, uintptr(unsafe.Pointer(utf16Ptr(sub))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rcSub)), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	procDeleteObject.Call(hFontSub)

	// 8. Copy to screen buffer
	procBitBlt.Call(hdc, 0, 0, WinWidth, WinHeight, hdcMem, 0, 0, SRCCOPY)
	procDeleteObject.Call(hbm)
	procDeleteDC.Call(hdcMem)
}
