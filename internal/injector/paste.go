package injector

import (
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32Mod            = syscall.NewLazyDLL("user32.dll")
	kernel32Mod          = syscall.NewLazyDLL("kernel32.dll")
	procOpenClipboard    = user32Mod.NewProc("OpenClipboard")
	procCloseClipboard   = user32Mod.NewProc("CloseClipboard")
	procEmptyClipboard   = user32Mod.NewProc("EmptyClipboard")
	procSetClipboardData = user32Mod.NewProc("SetClipboardData")
	procKeybdEvent       = user32Mod.NewProc("keybd_event")
	procGlobalAlloc      = kernel32Mod.NewProc("GlobalAlloc")
	procGlobalLock       = kernel32Mod.NewProc("GlobalLock")
	procGlobalUnlock     = kernel32Mod.NewProc("GlobalUnlock")
)

const (
	CF_UNICODETEXT  = 13
	GMEM_MOVEABLE   = 0x0002
	VK_CONTROL      = 0x11
	VK_SHIFT        = 0x10
	VK_MENU         = 0x12 // Alt
	VK_V            = 0x56
	KEYEVENTF_KEYUP = 0x0002
)

// PasteText places text in clipboard and reliably simulates Ctrl+V
func PasteText(text string) error {
	if text == "" {
		return nil
	}

	err := setClipboardText(text)
	if err != nil {
		return fmt.Errorf("failed to set clipboard: %w", err)
	}

	// Short delay for clipboard to register in Windows
	time.Sleep(30 * time.Millisecond)

	// Release any modifier keys that might still be held
	procKeybdEvent.Call(uintptr(VK_SHIFT), 0, KEYEVENTF_KEYUP, 0)
	procKeybdEvent.Call(uintptr(VK_MENU), 0, KEYEVENTF_KEYUP, 0)

	// Send Ctrl+V
	sendCtrlV()

	return nil
}

func sendCtrlV() {
	// Ctrl DOWN
	procKeybdEvent.Call(uintptr(VK_CONTROL), 0, 0, 0)
	time.Sleep(10 * time.Millisecond)
	// V DOWN
	procKeybdEvent.Call(uintptr(VK_V), 0, 0, 0)
	time.Sleep(20 * time.Millisecond)
	// V UP
	procKeybdEvent.Call(uintptr(VK_V), 0, KEYEVENTF_KEYUP, 0)
	time.Sleep(10 * time.Millisecond)
	// Ctrl UP
	procKeybdEvent.Call(uintptr(VK_CONTROL), 0, KEYEVENTF_KEYUP, 0)
}

func setClipboardText(text string) error {
	utf16Chars, err := syscall.UTF16FromString(text)
	if err != nil {
		return err
	}
	size := len(utf16Chars) * 2

	hMem, _, _ := procGlobalAlloc.Call(GMEM_MOVEABLE, uintptr(size))
	if hMem == 0 {
		return fmt.Errorf("GlobalAlloc failed")
	}

	ptr, _, _ := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock failed")
	}

	dest := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16Chars))
	copy(dest, utf16Chars)
	procGlobalUnlock.Call(hMem)

	opened := false
	for i := 0; i < 10; i++ {
		r, _, _ := procOpenClipboard.Call(0)
		if r != 0 {
			opened = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !opened {
		return fmt.Errorf("OpenClipboard failed")
	}
	defer procCloseClipboard.Call()

	procEmptyClipboard.Call()
	r, _, _ := procSetClipboardData.Call(CF_UNICODETEXT, hMem)
	if r == 0 {
		return fmt.Errorf("SetClipboardData failed")
	}

	return nil
}
