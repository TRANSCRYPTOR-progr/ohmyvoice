package hook

import (
	"fmt"
	"runtime"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32Mod               = syscall.NewLazyDLL("user32.dll")
	procSetWindowsHookExW   = user32Mod.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx = user32Mod.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx      = user32Mod.NewProc("CallNextHookEx")
	procGetMessageW         = user32Mod.NewProc("GetMessageW")
	procKeybdEvent          = user32Mod.NewProc("keybd_event")
)

const (
	WH_KEYBOARD_LL        = 13
	WM_KEYDOWN            = 0x0100
	WM_KEYUP              = 0x0101
	WM_SYSKEYDOWN         = 0x0104
	WM_SYSKEYUP           = 0x0105
	VK_CAPITAL            = 0x14
	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002
	SYNTHETIC_MARKER      = 0x1337
)

var KeyMap = map[string]uint32{
	"caps_lock":   0x14, // VK_CAPITAL
	"right_ctrl":  0xA3, // VK_RCONTROL
	"scroll_lock": 0x91, // VK_SCROLL
	"f8":          0x77, // VK_F8
	"right_alt":   0xA5, // VK_RMENU
}

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type KeyboardListener struct {
	hHook      uintptr
	isHolding  bool
	pressStart time.Time
	targetVk   uint32

	OnStart  func()
	OnStop   func()
	OnCancel func()
}

var globalListener *KeyboardListener

func NewKeyboardListener(onStart func(), onStop func(), onCancel func()) *KeyboardListener {
	l := &KeyboardListener{
		targetVk: VK_CAPITAL,
		OnStart:  onStart,
		OnStop:   onStop,
		OnCancel: onCancel,
	}
	globalListener = l
	return l
}

func (l *KeyboardListener) SetTargetKey(keyName string) {
	if vk, ok := KeyMap[keyName]; ok {
		l.targetVk = vk
	} else {
		l.targetVk = VK_CAPITAL
	}
}

func (l *KeyboardListener) Start() error {
	errChan := make(chan error, 1)

	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()

		hookCallback := syscall.NewCallback(lowLevelKeyboardProc)
		hHook, _, err := procSetWindowsHookExW.Call(
			uintptr(WH_KEYBOARD_LL),
			hookCallback,
			0,
			0,
		)
		if hHook == 0 {
			errChan <- fmt.Errorf("failed to install hook: %w", err)
			return
		}
		l.hHook = hHook
		errChan <- nil

		// Windows Message Loop
		var msg struct {
			Hwnd    uintptr
			Message uint32
			WParam  uintptr
			LParam  uintptr
			Time    uint32
			Pt      struct{ X, Y int32 }
		}

		for {
			ret, _, _ := procGetMessageW.Call(
				uintptr(unsafe.Pointer(&msg)),
				0,
				0,
				0,
			)
			if ret == 0 || int32(ret) == -1 {
				break
			}
		}

		procUnhookWindowsHookEx.Call(l.hHook)
	}()

	return <-errChan
}

func lowLevelKeyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode < 0 || globalListener == nil {
		r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return r
	}

	info := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))

	// If this event was synthesized by our own program, let it pass through
	if info.DwExtraInfo == SYNTHETIC_MARKER {
		r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
		return r
	}

	targetVk := globalListener.targetVk
	if targetVk == 0 {
		targetVk = VK_CAPITAL
	}

	if info.VkCode == targetVk {
		switch wParam {
		case WM_KEYDOWN, WM_SYSKEYDOWN:
			if !globalListener.isHolding {
				globalListener.isHolding = true
				globalListener.pressStart = time.Now()
				if globalListener.OnStart != nil {
					go globalListener.OnStart()
				}
			}
			return 1

		case WM_KEYUP, WM_SYSKEYUP:
			if globalListener.isHolding {
				globalListener.isHolding = false
				duration := time.Since(globalListener.pressStart)

				if targetVk == VK_CAPITAL {
					if duration < 250*time.Millisecond {
						// Quick tap: user wanted normal Caps Lock behavior
						if globalListener.OnCancel != nil {
							go globalListener.OnCancel()
						}
						toggleNativeCapsLock()
					} else {
						// Held down: user dictated text
						if globalListener.OnStop != nil {
							go globalListener.OnStop()
						}
					}
				} else {
					// Non-Caps key: release dictates
					if duration < 200*time.Millisecond {
						if globalListener.OnCancel != nil {
							go globalListener.OnCancel()
						}
					} else {
						if globalListener.OnStop != nil {
							go globalListener.OnStop()
						}
					}
				}
			}
			return 1
		}
	}

	r, _, _ := procCallNextHookEx.Call(0, uintptr(nCode), wParam, lParam)
	return r
}

func toggleNativeCapsLock() {
	// Send synthetic key down and key up with SYNTHETIC_MARKER
	procKeybdEvent.Call(
		uintptr(VK_CAPITAL),
		0x45,
		uintptr(KEYEVENTF_EXTENDEDKEY),
		uintptr(SYNTHETIC_MARKER),
	)
	procKeybdEvent.Call(
		uintptr(VK_CAPITAL),
		0x45,
		uintptr(KEYEVENTF_EXTENDEDKEY|KEYEVENTF_KEYUP),
		uintptr(SYNTHETIC_MARKER),
	)
}
