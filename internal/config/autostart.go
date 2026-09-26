package config

import (
	"fmt"
	"os"
	"syscall"
	"unsafe"
)

var (
	advapi32 = syscall.NewLazyDLL("advapi32.dll")

	procRegOpenKeyExW    = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueExW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueExW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValueW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey      = advapi32.NewProc("RegCloseKey")
)

const (
	HKEY_CURRENT_USER = uintptr(0x80000001)
	KEY_READ          = 0x20019
	KEY_SET_VALUE     = 0x0002
	REG_SZ            = 1
	runSubKey         = `Software\Microsoft\Windows\CurrentVersion\Run`
	appName           = "OhMyVoice"
)

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

// IsAutoStartEnabled checks if OhMyVoice is registered in Windows startup
func IsAutoStartEnabled() bool {
	var hKey uintptr
	r, _, _ := procRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(utf16Ptr(runSubKey))),
		0,
		uintptr(KEY_READ),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return false
	}
	defer procRegCloseKey.Call(hKey)

	var valType uint32
	var buf [512]uint16
	bufLen := uint32(len(buf) * 2)

	r, _, _ = procRegQueryValueExW.Call(
		hKey,
		uintptr(unsafe.Pointer(utf16Ptr(appName))),
		0,
		uintptr(unsafe.Pointer(&valType)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(unsafe.Pointer(&bufLen)),
	)

	return r == 0
}

// SetAutoStart enables or disables autostart in Windows Registry
func SetAutoStart(enable bool) error {
	if enable {
		exePath, err := os.Executable()
		if err != nil {
			return fmt.Errorf("failed to get executable path: %w", err)
		}

		quotedPath := fmt.Sprintf("\"%s\"", exePath)
		pathU16, err := syscall.UTF16FromString(quotedPath)
		if err != nil {
			return err
		}

		var hKey uintptr
		r, _, err := procRegOpenKeyExW.Call(
			HKEY_CURRENT_USER,
			uintptr(unsafe.Pointer(utf16Ptr(runSubKey))),
			0,
			uintptr(KEY_SET_VALUE),
			uintptr(unsafe.Pointer(&hKey)),
		)
		if r != 0 {
			return fmt.Errorf("failed to open registry key: %v", err)
		}
		defer procRegCloseKey.Call(hKey)

		byteLen := uint32(len(pathU16) * 2)
		r, _, err = procRegSetValueExW.Call(
			hKey,
			uintptr(unsafe.Pointer(utf16Ptr(appName))),
			0,
			uintptr(REG_SZ),
			uintptr(unsafe.Pointer(&pathU16[0])),
			uintptr(byteLen),
		)
		if r != 0 {
			return fmt.Errorf("failed to set registry value: %v", err)
		}
		return nil
	}

	var hKey uintptr
	r, _, _ := procRegOpenKeyExW.Call(
		HKEY_CURRENT_USER,
		uintptr(unsafe.Pointer(utf16Ptr(runSubKey))),
		0,
		uintptr(KEY_SET_VALUE),
		uintptr(unsafe.Pointer(&hKey)),
	)
	if r != 0 {
		return nil
	}
	defer procRegCloseKey.Call(hKey)

	procRegDeleteValueW.Call(
		hKey,
		uintptr(unsafe.Pointer(utf16Ptr(appName))),
	)
	return nil
}
