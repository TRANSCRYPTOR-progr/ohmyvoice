package ui

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"unsafe"

	"ohmyvoice/internal/config"
)

var (
	settingsHwnd   uintptr
	settingsMu     sync.Mutex
	isSettingsOpen bool

	procBringWindowToTop    = user32.NewProc("BringWindowToTop")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procTrackMouseEvent     = user32.NewProc("TrackMouseEvent")
	procLoadCursorW         = user32.NewProc("LoadCursorW")
	procSetCursor           = user32.NewProc("SetCursor")
)

const (
	WM_LBUTTONDOWN = 0x0201
	WM_SETCURSOR   = 0x0020
	WM_MOUSELEAVE  = 0x02A3
	WM_KEYDOWN     = 0x0100
	VK_ESCAPE      = 0x1B

	DT_LEFT     = 0x00000000
	DT_RIGHT    = 0x00000002
	DT_NOPREFIX = 0x00000800

	DlgWidth  int32 = 500
	DlgHeight int32 = 574
)

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

var (
	activeBaseDir string
	activeCfg     *config.Config
	activeOnSave  func(*config.Config)

	tempAutoStart bool
	tempHotkey    string
	tempModel     string
	tempLang      string
	tempSound     bool

	currentHover    string
	isTrackingMouse bool

	hHandCursor  uintptr
	hArrowCursor uintptr

	// Font handles cached for the settings window life
	fntTitle    uintptr
	fntSubtitle uintptr
	fntHeader   uintptr
	fntBody     uintptr
	fntSmall    uintptr
	fntBadge    uintptr
	fntButton   uintptr
	fntSymbol   uintptr

	// UI Component Rectangles
	rcClose         = RECT{Left: 450, Top: 16, Right: 480, Bottom: 46}
	rcCardAutostart = RECT{Left: 20, Top: 68, Right: 480, Bottom: 124}
	rcToggleAuto    = RECT{Left: 422, Top: 84, Right: 466, Bottom: 108}

	rcCardHotkey = RECT{Left: 20, Top: 132, Right: 480, Bottom: 222}
	rcChipCaps   = RECT{Left: 32, Top: 184, Right: 114, Bottom: 212}
	rcChipRCtrl  = RECT{Left: 120, Top: 184, Right: 204, Bottom: 212}
	rcChipScroll = RECT{Left: 210, Top: 184, Right: 296, Bottom: 212}
	rcChipF8     = RECT{Left: 302, Top: 184, Right: 372, Bottom: 212}
	rcChipRAlt   = RECT{Left: 378, Top: 184, Right: 468, Bottom: 212}

	rcCardModel   = RECT{Left: 20, Top: 230, Right: 480, Bottom: 332}
	rcModelBase   = RECT{Left: 32, Top: 278, Right: 172, Bottom: 324}
	rcModelSmall  = RECT{Left: 180, Top: 278, Right: 320, Bottom: 324}
	rcModelMedium = RECT{Left: 328, Top: 278, Right: 468, Bottom: 324}

	rcCardLang = RECT{Left: 20, Top: 340, Right: 480, Bottom: 430}
	rcLangRu   = RECT{Left: 32, Top: 392, Right: 172, Bottom: 420}
	rcLangEn   = RECT{Left: 180, Top: 392, Right: 320, Bottom: 420}
	rcLangAuto = RECT{Left: 328, Top: 392, Right: 468, Bottom: 420}

	rcCardSound  = RECT{Left: 20, Top: 438, Right: 480, Bottom: 494}
	rcToggleSound = RECT{Left: 422, Top: 454, Right: 466, Bottom: 478}

	rcBtnCancel = RECT{Left: 252, Top: 512, Right: 348, Bottom: 552}
	rcBtnSave   = RECT{Left: 356, Top: 512, Right: 480, Bottom: 552}
)

func initFonts() {
	fntTitle, _, _ = procCreateFontW.Call(
		18, 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntSubtitle, _, _ = procCreateFontW.Call(
		12, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntHeader, _, _ = procCreateFontW.Call(
		14, 0, 0, 0, 600, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntBody, _, _ = procCreateFontW.Call(
		12, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntSmall, _, _ = procCreateFontW.Call(
		11, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntBadge, _, _ = procCreateFontW.Call(
		11, 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntButton, _, _ = procCreateFontW.Call(
		13, 0, 0, 0, 700, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI"))),
	)
	fntSymbol, _, _ = procCreateFontW.Call(
		15, 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0, uintptr(unsafe.Pointer(utf16Ptr("Segoe UI Symbol"))),
	)
}

func freeFonts() {
	fonts := []uintptr{fntTitle, fntSubtitle, fntHeader, fntBody, fntSmall, fntBadge, fntButton, fntSymbol}
	for _, f := range fonts {
		if f != 0 {
			procDeleteObject.Call(f)
		}
	}
}

func isPointInRect(pt POINT, rc RECT) bool {
	return pt.X >= rc.Left && pt.X <= rc.Right && pt.Y >= rc.Top && pt.Y <= rc.Bottom
}

func getHoveredControl(pt POINT) string {
	switch {
	case isPointInRect(pt, rcClose):
		return "close"
	case isPointInRect(pt, rcChipCaps):
		return "hotkey_caps"
	case isPointInRect(pt, rcChipRCtrl):
		return "hotkey_rctrl"
	case isPointInRect(pt, rcChipScroll):
		return "hotkey_scroll"
	case isPointInRect(pt, rcChipF8):
		return "hotkey_f8"
	case isPointInRect(pt, rcChipRAlt):
		return "hotkey_ralt"
	case isPointInRect(pt, rcModelBase):
		return "model_base"
	case isPointInRect(pt, rcModelSmall):
		return "model_small"
	case isPointInRect(pt, rcModelMedium):
		return "model_medium"
	case isPointInRect(pt, rcLangRu):
		return "lang_ru"
	case isPointInRect(pt, rcLangEn):
		return "lang_en"
	case isPointInRect(pt, rcLangAuto):
		return "lang_auto"
	case isPointInRect(pt, rcCardAutostart):
		return "autostart"
	case isPointInRect(pt, rcCardSound):
		return "sound"
	case isPointInRect(pt, rcBtnCancel):
		return "btn_cancel"
	case isPointInRect(pt, rcBtnSave):
		return "btn_save"
	default:
		return ""
	}
}

func settingsWndProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_SETCURSOR:
		if currentHover != "" {
			if hHandCursor == 0 {
				hHandCursor, _, _ = procLoadCursorW.Call(0, 32649) // IDC_HAND
			}
			procSetCursor.Call(hHandCursor)
			return 1
		}
		if hArrowCursor == 0 {
			hArrowCursor, _, _ = procLoadCursorW.Call(0, 32512) // IDC_ARROW
		}
		procSetCursor.Call(hArrowCursor)
		return 1

	case WM_MOUSEMOVE:
		pt := POINT{
			X: int32(int16(lParam & 0xFFFF)),
			Y: int32(int16((lParam >> 16) & 0xFFFF)),
		}

		if !isTrackingMouse {
			var tme TRACKMOUSEEVENT
			tme.CbSize = uint32(unsafe.Sizeof(tme))
			tme.DwFlags = 0x00000002 // TME_LEAVE
			tme.HwndTrack = hwnd
			procTrackMouseEvent.Call(uintptr(unsafe.Pointer(&tme)))
			isTrackingMouse = true
		}

		newHover := getHoveredControl(pt)
		if newHover != currentHover {
			currentHover = newHover
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case WM_MOUSELEAVE:
		isTrackingMouse = false
		if currentHover != "" {
			currentHover = ""
			procInvalidateRect.Call(hwnd, 0, 0)
		}
		return 0

	case WM_LBUTTONDOWN:
		pt := POINT{
			X: int32(int16(lParam & 0xFFFF)),
			Y: int32(int16((lParam >> 16) & 0xFFFF)),
		}

		target := getHoveredControl(pt)
		switch target {
		case "close", "btn_cancel":
			procDestroyWindow.Call(hwnd)
			return 0
		case "autostart":
			tempAutoStart = !tempAutoStart
			procInvalidateRect.Call(hwnd, 0, 0)
		case "hotkey_caps":
			tempHotkey = "caps_lock"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "hotkey_rctrl":
			tempHotkey = "right_ctrl"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "hotkey_scroll":
			tempHotkey = "scroll_lock"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "hotkey_f8":
			tempHotkey = "f8"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "hotkey_ralt":
			tempHotkey = "right_alt"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "model_base":
			tempModel = "ggml-base.bin"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "model_small":
			tempModel = "ggml-small.bin"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "model_medium":
			tempModel = "ggml-medium.bin"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "lang_ru":
			tempLang = "ru"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "lang_en":
			tempLang = "en"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "lang_auto":
			tempLang = "auto"
			procInvalidateRect.Call(hwnd, 0, 0)
		case "sound":
			tempSound = !tempSound
			procInvalidateRect.Call(hwnd, 0, 0)
		case "btn_save":
			saveSettings(hwnd)
			procDestroyWindow.Call(hwnd)
			return 0
		}
		return 0

	case WM_LBUTTONUP:
		return 0

	case WM_KEYDOWN:
		if wParam == VK_ESCAPE {
			procDestroyWindow.Call(hwnd)
		}
		return 0

	case WM_NCHITTEST:
		var pt POINT
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		procScreenToClient.Call(hwnd, uintptr(unsafe.Pointer(&pt)))
		// Allow window dragging from top header except the close button
		if pt.Y <= 60 && !isPointInRect(pt, rcClose) {
			return HTCAPTION
		}
		return HTCLIENT

	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		if hdc != 0 {
			paintSettings(hdc)
			procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		}
		return 0

	case WM_DESTROY:
		freeFonts()
		procPostQuitMessage.Call(0)
		settingsMu.Lock()
		isSettingsOpen = false
		settingsHwnd = 0
		settingsMu.Unlock()
		return 0
	}

	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wParam, lParam)
	return r
}

func IsSettingsOpen() bool {
	settingsMu.Lock()
	defer settingsMu.Unlock()
	return isSettingsOpen
}

func OpenSettings(baseDir string, cfg *config.Config, onSave func(*config.Config)) {
	settingsMu.Lock()
	if isSettingsOpen && settingsHwnd != 0 {
		procShowWindow.Call(settingsHwnd, 5) // SW_SHOW
		procBringWindowToTop.Call(settingsHwnd)
		procSetForegroundWindow.Call(settingsHwnd)
		settingsMu.Unlock()
		return
	}
	isSettingsOpen = true
	settingsMu.Unlock()

	activeBaseDir = baseDir
	activeCfg = cfg
	activeOnSave = onSave

	// Populate working copy
	tempAutoStart = config.IsAutoStartEnabled()
	tempHotkey = cfg.Hotkey
	if tempHotkey == "" {
		tempHotkey = "caps_lock"
	}
	tempModel = cfg.Model
	if tempModel == "" {
		tempModel = "ggml-small.bin"
	}
	tempLang = cfg.Language
	if tempLang == "" {
		tempLang = "ru"
	}
	tempSound = cfg.SoundFeedback
	currentHover = ""

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hInst, _, _ := procGetModuleHandleW.Call(0)
		className := utf16Ptr("OhMyVoice_CustomSettings")

		hBlackBrush, _, _ := procCreateSolidBrush.Call(uintptr(rgb(0, 0, 0)))

		wc := WNDCLASSEXW{
			CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
			Style:         3,
			LpfnWndProc:   syscall.NewCallback(settingsWndProc),
			HInstance:     hInst,
			HbrBackground: hBlackBrush,
			LpszClassName: className,
		}
		procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))

		initFonts()

		cxScreen, _, _ := procGetSystemMetrics.Call(0)
		cyScreen, _, _ := procGetSystemMetrics.Call(1)
		posX := (int32(cxScreen) - DlgWidth) / 2
		posY := (int32(cyScreen) - DlgHeight) / 2

		hwnd, _, _ := procCreateWindowExW.Call(
			WS_EX_TOPMOST,
			uintptr(unsafe.Pointer(className)),
			uintptr(unsafe.Pointer(utf16Ptr("OhMyVoice Settings"))),
			uintptr(WS_POPUP),
			uintptr(posX), uintptr(posY), uintptr(DlgWidth), uintptr(DlgHeight),
			0, 0, hInst, 0,
		)

		if hwnd == 0 {
			settingsMu.Lock()
			isSettingsOpen = false
			settingsMu.Unlock()
			return
		}

		settingsMu.Lock()
		settingsHwnd = hwnd
		settingsMu.Unlock()

		// 22px rounded window region
		rgn, _, _ := procCreateRoundRectRgn.Call(0, 0, uintptr(DlgWidth), uintptr(DlgHeight), 22, 22)
		procSetWindowRgn.Call(hwnd, rgn, 1)

		procShowWindow.Call(hwnd, 5) // SW_SHOW
		procUpdateWindow.Call(hwnd)
		procBringWindowToTop.Call(hwnd)
		procSetForegroundWindow.Call(hwnd)

		var msg MSG
		for {
			ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
			if ret == 0 || int32(ret) == -1 {
				break
			}
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}()
}

// ---------------------------------------------------------
// Custom GDI Rendering Engine
// ---------------------------------------------------------

func drawBox(hdc uintptr, rc RECT, radius int32, fillCol, borderCol uint32, borderW int32) {
	hBrush, _, _ := procCreateSolidBrush.Call(uintptr(fillCol))
	hPen, _, _ := procCreatePen.Call(PS_SOLID, uintptr(borderW), uintptr(borderCol))
	oldBrush, _, _ := procSelectObject.Call(hdc, hBrush)
	oldPen, _, _ := procSelectObject.Call(hdc, hPen)

	procRoundRect.Call(hdc, uintptr(rc.Left), uintptr(rc.Top), uintptr(rc.Right), uintptr(rc.Bottom), uintptr(radius), uintptr(radius))

	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(hBrush)
	procDeleteObject.Call(hPen)
}

func drawCircle(hdc uintptr, cx, cy, radius int32, fillCol, borderCol uint32) {
	hBrush, _, _ := procCreateSolidBrush.Call(uintptr(fillCol))
	hPen, _, _ := procCreatePen.Call(PS_SOLID, 1, uintptr(borderCol))
	oldBrush, _, _ := procSelectObject.Call(hdc, hBrush)
	oldPen, _, _ := procSelectObject.Call(hdc, hPen)

	procEllipse.Call(hdc, uintptr(cx-radius), uintptr(cy-radius), uintptr(cx+radius), uintptr(cy+radius))

	procSelectObject.Call(hdc, oldBrush)
	procSelectObject.Call(hdc, oldPen)
	procDeleteObject.Call(hBrush)
	procDeleteObject.Call(hPen)
}

func drawText(hdc uintptr, text string, rc RECT, font uintptr, col uint32, flags uint32) {
	procSetTextColor.Call(hdc, uintptr(col))
	oldFont, _, _ := procSelectObject.Call(hdc, font)
	procDrawTextW.Call(hdc, uintptr(unsafe.Pointer(utf16Ptr(text))), uintptr(^uint32(0)), uintptr(unsafe.Pointer(&rc)), uintptr(flags|DT_NOPREFIX))
	procSelectObject.Call(hdc, oldFont)
}

func drawPillToggle(hdc uintptr, rc RECT, isChecked, isHovered bool) {
	var fill, border, knobFill, knobBorder uint32
	if isChecked {
		fill = rgb(34, 197, 94)      // Emerald green
		border = rgb(74, 222, 128)   // Light emerald
		if isHovered {
			fill = rgb(22, 163, 74)
		}
		knobFill = rgb(255, 255, 255)
		knobBorder = rgb(255, 255, 255)
	} else {
		fill = rgb(36, 39, 52)       // Dark slate
		border = rgb(56, 61, 80)
		if isHovered {
			fill = rgb(46, 50, 68)
		}
		knobFill = rgb(148, 163, 184) // Cool slate
		knobBorder = rgb(148, 163, 184)
	}

	drawBox(hdc, rc, 24, fill, border, 1)

	cy := (rc.Top + rc.Bottom) / 2
	var cx int32
	if isChecked {
		cx = rc.Right - 12
	} else {
		cx = rc.Left + 12
	}
	drawCircle(hdc, cx, cy, 8, knobFill, knobBorder)
}

func drawChip(hdc uintptr, rc RECT, label string, isSelected, isHovered bool) {
	var fill, border, textCol uint32
	if isSelected {
		fill = rgb(124, 58, 237)      // Violet primary
		border = rgb(167, 139, 250)   // Violet glow
		textCol = rgb(255, 255, 255)
	} else if isHovered {
		fill = rgb(34, 37, 50)        // Subtle hover
		border = rgb(65, 71, 95)
		textCol = rgb(241, 245, 249)
	} else {
		fill = rgb(18, 20, 27)        // Dark chip surface
		border = rgb(36, 40, 54)
		textCol = rgb(156, 163, 175)
	}

	drawBox(hdc, rc, 12, fill, border, 1)
	drawText(hdc, label, rc, fntHeader, textCol, DT_CENTER|DT_VCENTER|DT_SINGLELINE)
}

func drawModelCard(hdc uintptr, rc RECT, title, sub, badge string, isSelected, isHovered bool) {
	var fill, border, titleCol, subCol uint32
	borderWidth := int32(1)

	if isSelected {
		fill = rgb(24, 20, 48)       // Indigo tint
		border = rgb(139, 92, 246)   // Vivid violet
		borderWidth = 2
		titleCol = rgb(255, 255, 255)
		subCol = rgb(196, 181, 253)
	} else if isHovered {
		fill = rgb(26, 29, 39)
		border = rgb(60, 66, 88)
		titleCol = rgb(241, 245, 249)
		subCol = rgb(148, 163, 184)
	} else {
		fill = rgb(14, 16, 23)
		border = rgb(32, 36, 49)
		titleCol = rgb(203, 213, 225)
		subCol = rgb(100, 116, 139)
	}

	drawBox(hdc, rc, 14, fill, border, borderWidth)

	rcTitle := RECT{Left: rc.Left + 10, Top: rc.Top + 6, Right: rc.Right - 22, Bottom: rc.Top + 24}
	drawText(hdc, title, rcTitle, fntHeader, titleCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	rcSub := RECT{Left: rc.Left + 10, Top: rc.Top + 24, Right: rc.Right - 6, Bottom: rc.Top + 40}
	drawText(hdc, sub, rcSub, fntSmall, subCol, DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	// If selected, display a neat checkmark badge on the right
	if isSelected {
		cx := rc.Right - 13
		cy := rc.Top + 14
		drawCircle(hdc, cx, cy, 7, rgb(139, 92, 246), rgb(167, 139, 250))
		rcCheck := RECT{Left: cx - 7, Top: cy - 8, Right: cx + 7, Bottom: cy + 6}
		drawText(hdc, "✓", rcCheck, fntSmall, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)
	}
}

func paintSettings(hdc uintptr) {
	hdcMem, _, _ := procCreateCompatibleDC.Call(hdc)
	hbm, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(DlgWidth), uintptr(DlgHeight))
	procSelectObject.Call(hdcMem, hbm)

	procSetBkMode.Call(hdcMem, TRANSPARENT)

	// 1. Window Background: Pure OLED Black #000000 with subtle slate border
	rcWindow := RECT{Left: 0, Top: 0, Right: DlgWidth, Bottom: DlgHeight}
	drawBox(hdcMem, rcWindow, 22, rgb(0, 0, 0), rgb(32, 36, 50), 1)

	// 2. Header
	// Icon circle
	drawCircle(hdcMem, 40, 32, 16, rgb(28, 22, 54), rgb(124, 58, 237))
	rcIcon := RECT{Left: 24, Top: 16, Right: 56, Bottom: 48}
	drawText(hdcMem, "⚙", rcIcon, fntSymbol, rgb(167, 139, 250), DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// Title
	rcTitle := RECT{Left: 64, Top: 15, Right: 260, Bottom: 36}
	drawText(hdcMem, "Настройки OhMyVoice", rcTitle, fntTitle, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	// CUDA Badge
	rcBadge := RECT{Left: 266, Top: 17, Right: 334, Bottom: 34}
	drawBox(hdcMem, rcBadge, 8, rgb(6, 78, 59), rgb(16, 185, 129), 1)
	drawText(hdcMem, "CUDA 12.4", rcBadge, fntBadge, rgb(52, 211, 153), DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// Subtitle
	rcSubtitle := RECT{Left: 64, Top: 37, Right: 360, Bottom: 53}
	drawText(hdcMem, "Конфигурация распознавания и горячих клавиш", rcSubtitle, fntSubtitle, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	// Close Button
	closeHover := (currentHover == "close")
	var closeFill, closeBorder, closeText uint32
	if closeHover {
		closeFill = rgb(239, 68, 68)
		closeBorder = rgb(248, 113, 113)
		closeText = rgb(255, 255, 255)
	} else {
		closeFill = rgb(20, 22, 30)
		closeBorder = rgb(38, 42, 58)
		closeText = rgb(148, 163, 184)
	}
	drawBox(hdcMem, rcClose, 10, closeFill, closeBorder, 1)
	drawText(hdcMem, "✕", rcClose, fntBody, closeText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// ---------------------------------------------------------
	// Card 1: Autostart Toggle
	// ---------------------------------------------------------
	autoHover := (currentHover == "autostart")
	card1Border := rgb(26, 29, 39)
	if autoHover {
		card1Border = rgb(50, 56, 76)
	}
	drawBox(hdcMem, rcCardAutostart, 14, rgb(11, 12, 17), card1Border, 1)

	// Autostart Text
	rcAutoTitle := RECT{Left: 40, Top: 77, Right: 410, Bottom: 96}
	drawText(hdcMem, "Запуск вместе с Windows (Автозагрузка)", rcAutoTitle, fntHeader, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	rcAutoSub := RECT{Left: 40, Top: 97, Right: 410, Bottom: 115}
	drawText(hdcMem, "Автоматически запускать OhMyVoice в фоне при включении ПК", rcAutoSub, fntSmall, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	// Autostart Toggle Switch
	drawPillToggle(hdcMem, rcToggleAuto, tempAutoStart, autoHover)

	// ---------------------------------------------------------
	// Card 2: Hotkey Segmented Picker
	// ---------------------------------------------------------
	drawBox(hdcMem, rcCardHotkey, 14, rgb(11, 12, 17), rgb(26, 29, 39), 1)

	rcKeyTitle := RECT{Left: 36, Top: 142, Right: 460, Bottom: 161}
	drawText(hdcMem, "Клавиша Push-to-Talk (зажать для диктовки)", rcKeyTitle, fntHeader, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	rcKeySub := RECT{Left: 36, Top: 161, Right: 460, Bottom: 178}
	drawText(hdcMem, "Одиночный клик по Caps Lock по-прежнему работает стандартно", rcKeySub, fntSmall, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	drawChip(hdcMem, rcChipCaps, "Caps Lock", tempHotkey == "caps_lock", currentHover == "hotkey_caps")
	drawChip(hdcMem, rcChipRCtrl, "Right Ctrl", tempHotkey == "right_ctrl", currentHover == "hotkey_rctrl")
	drawChip(hdcMem, rcChipScroll, "Scroll Lock", tempHotkey == "scroll_lock", currentHover == "hotkey_scroll")
	drawChip(hdcMem, rcChipF8, "F8", tempHotkey == "f8", currentHover == "hotkey_f8")
	drawChip(hdcMem, rcChipRAlt, "Right Alt", tempHotkey == "right_alt", currentHover == "hotkey_ralt")

	// ---------------------------------------------------------
	// Card 3: Model 3-Card Selectable Cards
	// ---------------------------------------------------------
	drawBox(hdcMem, rcCardModel, 14, rgb(11, 12, 17), rgb(26, 29, 39), 1)

	rcModelTitle := RECT{Left: 36, Top: 240, Right: 460, Bottom: 259}
	drawText(hdcMem, "Модель нейросети Whisper (CUDA ускорение)", rcModelTitle, fntHeader, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	rcModelSub := RECT{Left: 36, Top: 259, Right: 460, Bottom: 275}
	drawText(hdcMem, "Размер модели влияет на точность распознавания и скорость", rcModelSub, fntSmall, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	drawModelCard(hdcMem, rcModelBase, "Base", "~100 мс • Быстрая", "✓", tempModel == "ggml-base.bin", currentHover == "model_base")
	drawModelCard(hdcMem, rcModelSmall, "Small", "~250 мс • Баланс", "✓", tempModel == "ggml-small.bin", currentHover == "model_small")
	drawModelCard(hdcMem, rcModelMedium, "Medium", "~550 мс • Точность", "✓", tempModel == "ggml-medium.bin", currentHover == "model_medium")

	// ---------------------------------------------------------
	// Card 4: Language Segmented Picker
	// ---------------------------------------------------------
	drawBox(hdcMem, rcCardLang, 14, rgb(11, 12, 17), rgb(26, 29, 39), 1)

	rcLangTitle := RECT{Left: 36, Top: 349, Right: 460, Bottom: 368}
	drawText(hdcMem, "Язык диктовки", rcLangTitle, fntHeader, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	rcLangSub := RECT{Left: 36, Top: 368, Right: 460, Bottom: 385}
	drawText(hdcMem, "Фиксация языка ускоряет старт распознавания", rcLangSub, fntSmall, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	drawChip(hdcMem, rcLangRu, "Русский (ru)", tempLang == "ru", currentHover == "lang_ru")
	drawChip(hdcMem, rcLangEn, "English (en)", tempLang == "en", currentHover == "lang_en")
	drawChip(hdcMem, rcLangAuto, "Автоопределение", tempLang == "auto", currentHover == "lang_auto")

	// ---------------------------------------------------------
	// Card 5: Sound Feedback Toggle
	// ---------------------------------------------------------
	soundHover := (currentHover == "sound")
	card5Border := rgb(26, 29, 39)
	if soundHover {
		card5Border = rgb(50, 56, 76)
	}
	drawBox(hdcMem, rcCardSound, 14, rgb(11, 12, 17), card5Border, 1)

	rcSoundTitle := RECT{Left: 40, Top: 447, Right: 410, Bottom: 466}
	drawText(hdcMem, "Звуковые эффекты Discord PTT", rcSoundTitle, fntHeader, rgb(255, 255, 255), DT_LEFT|DT_VCENTER|DT_SINGLELINE)
	rcSoundSub := RECT{Left: 40, Top: 467, Right: 410, Bottom: 485}
	drawText(hdcMem, "Щелчки активации записи и уведомление о вставке текста", rcSoundSub, fntSmall, rgb(148, 163, 184), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	drawPillToggle(hdcMem, rcToggleSound, tempSound, soundHover)

	// ---------------------------------------------------------
	// Footer: Esc hint & Action Buttons
	// ---------------------------------------------------------
	rcEscHint := RECT{Left: 32, Top: 524, Right: 240, Bottom: 544}
	drawText(hdcMem, "Нажмите Esc для отмены", rcEscHint, fntSmall, rgb(100, 116, 139), DT_LEFT|DT_VCENTER|DT_SINGLELINE)

	// Cancel Button
	btnCancelHover := (currentHover == "btn_cancel")
	var cancelFill, cancelBorder, cancelText uint32
	if btnCancelHover {
		cancelFill = rgb(36, 40, 54)
		cancelBorder = rgb(65, 72, 98)
		cancelText = rgb(255, 255, 255)
	} else {
		cancelFill = rgb(18, 20, 28)
		cancelBorder = rgb(36, 40, 56)
		cancelText = rgb(156, 163, 175)
	}
	drawBox(hdcMem, rcBtnCancel, 12, cancelFill, cancelBorder, 1)
	drawText(hdcMem, "Отмена", rcBtnCancel, fntButton, cancelText, DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// Save Button
	btnSaveHover := (currentHover == "btn_save")
	var saveFill, saveBorder uint32
	if btnSaveHover {
		saveFill = rgb(147, 51, 234)    // Bright purple
		saveBorder = rgb(192, 132, 252)
	} else {
		saveFill = rgb(124, 58, 237)    // Violet
		saveBorder = rgb(167, 139, 250)
	}
	drawBox(hdcMem, rcBtnSave, 12, saveFill, saveBorder, 1)
	drawText(hdcMem, "Сохранить", rcBtnSave, fntButton, rgb(255, 255, 255), DT_CENTER|DT_VCENTER|DT_SINGLELINE)

	// Copy to screen
	procBitBlt.Call(hdc, 0, 0, uintptr(DlgWidth), uintptr(DlgHeight), hdcMem, 0, 0, SRCCOPY)
	procDeleteObject.Call(hbm)
	procDeleteDC.Call(hdcMem)
}

func saveSettings(hwnd uintptr) {
	if activeCfg == nil {
		return
	}

	// 1. Autostart
	_ = config.SetAutoStart(tempAutoStart)
	activeCfg.AutoStart = tempAutoStart

	// 2. Hotkey
	activeCfg.Hotkey = tempHotkey

	// 3. Model
	activeCfg.Model = tempModel

	// 4. Language
	activeCfg.Language = tempLang

	// 5. Sound
	activeCfg.SoundFeedback = tempSound

	// Save to disk
	_ = activeCfg.Save(activeBaseDir)

	if activeOnSave != nil {
		activeOnSave(activeCfg)
	}

	fmt.Printf("[OK] Настройки сохранены: автозагрузка=%v, hotkey=%s, модель=%s, язык=%s, звук=%v\n",
		activeCfg.AutoStart, activeCfg.Hotkey, activeCfg.Model, activeCfg.Language, activeCfg.SoundFeedback)
}
