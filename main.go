package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unicode"

	"ohmyvoice/internal/audio"
	"ohmyvoice/internal/config"
	"ohmyvoice/internal/feedback"
	"ohmyvoice/internal/hook"
	"ohmyvoice/internal/injector"
	"ohmyvoice/internal/transcribe"
	"ohmyvoice/internal/ui"
)

func isSettingsCommand(text string) bool {
	lower := strings.ToLower(text)
	clean := strings.Map(func(r rune) rune {
		if unicode.IsPunct(r) || unicode.IsSpace(r) {
			return ' '
		}
		return r
	}, lower)
	fields := strings.Fields(clean)
	combined := strings.Join(fields, " ")

	return strings.Contains(combined, "настройк") ||
		strings.Contains(combined, "параметр") ||
		strings.Contains(combined, "settings")
}

func main() {
	// If launched from existing terminal/PowerShell, attach to its console output
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	procAttachConsole := kernel32.NewProc("AttachConsole")
	r, _, _ := procAttachConsole.Call(uintptr(^uint32(0)))
	if r != 0 {
		procGetStdHandle := kernel32.NewProc("GetStdHandle")
		hOut, _, _ := procGetStdHandle.Call(uintptr(^uint32(10))) // STD_OUTPUT_HANDLE
		if hOut != 0 && hOut != uintptr(^uintptr(0)) {
			os.Stdout = os.NewFile(hOut, "/dev/stdout")
		}
		hErr, _, _ := procGetStdHandle.Call(uintptr(^uint32(11))) // STD_ERROR_HANDLE
		if hErr != 0 && hErr != uintptr(^uintptr(0)) {
			os.Stderr = os.NewFile(hErr, "/dev/stderr")
		}
	}

	exePath, err := os.Executable()
	baseDir := "."
	if err == nil {
		baseDir = filepath.Dir(exePath)
	}
	if _, err := os.Stat(filepath.Join(baseDir, "bin", "whisper-cli.exe")); err != nil {
		cwd, err := os.Getwd()
		if err == nil {
			baseDir = cwd
		}
	}

	cfg := config.Load(baseDir)

	for _, arg := range os.Args[1:] {
		if arg == "--settings" || arg == "-settings" || arg == "/settings" {
			ui.OpenSettings(baseDir, cfg, nil)
			time.Sleep(150 * time.Millisecond)
			for ui.IsSettingsOpen() {
				time.Sleep(100 * time.Millisecond)
			}
			return
		}
	}

	fmt.Println()
	fmt.Println("=========================================================")
	fmt.Println("     🎙️  OhMyVoice — Голосовой ввод для Windows  🎙️     ")
	fmt.Println("=========================================================")

	engine, err := transcribe.NewEngine(baseDir, cfg)
	if err != nil {
		log.Fatalf("Ошибка инициализации движка Whisper: %v", err)
	}

	feedback.Init(baseDir)
	defer feedback.Close()

	recorder := audio.NewRecorder()
	err = recorder.InitDevice()
	if err != nil {
		log.Fatalf("Ошибка инициализации микрофона: %v", err)
	}
	defer recorder.Close()

	tempWavPath := filepath.Join(baseDir, "temp_voice.wav")

	fmt.Printf("[OK] Модель: %s (язык: %s, потоков: %d)\n", cfg.Model, cfg.Language, cfg.Threads)
	fmt.Printf("[OK] Микрофон: активен в фоне (0 мс задержка + 350мс буфер предзаписи)\n")
	fmt.Printf("[OK] Клавиша активации: %s\n", cfg.Hotkey)
	fmt.Printf("[OK] Автозагрузка Windows: %v\n", cfg.AutoStart)
	fmt.Println("---------------------------------------------------------")
	fmt.Println("💡 КАК ПОЛЬЗОВАТЬСЯ:")
	fmt.Println("   • ЗАЖМИ клавишу активации (по умолчанию [Caps Lock]) и говори фразу")
	fmt.Println("   • ОТПУСТИ клавишу — текст мгновенно напечатается")
	fmt.Println("   • Скажи «Открой настройки» или кликни правой кнопкой по виджету")
	fmt.Println("---------------------------------------------------------")
	fmt.Println("Ожидание нажатия... (Нажмите Ctrl+C для выхода)\n")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)

	var listener *hook.KeyboardListener
	var hud *ui.MiniWindow

	// Callback to open settings dialog
	openSettingsDialog := func() {
		hud.SetSettings()
		ui.OpenSettings(baseDir, cfg, func(newCfg *config.Config) {
			cfg = newCfg
			if listener != nil {
				listener.SetTargetKey(cfg.Hotkey)
			}
			newEngine, err := transcribe.NewEngine(baseDir, cfg)
			if err == nil {
				engine = newEngine
			}
		})
	}

	// Initialize and launch floating Mini HUD
	hud = ui.New(func() {
		select {
		case sigChan <- os.Interrupt:
		default:
		}
	}, openSettingsDialog)

	err = hud.Start()
	if err != nil {
		log.Printf("Предупреждение: не удалось запустить мини UI: %v", err)
	}
	defer hud.Close()

	var isBusy bool

	onStart := func() {
		if isBusy {
			return
		}
		if cfg.SoundFeedback {
			feedback.PlayStartBeep()
		}
		fmt.Print("\r[🎙️ ЗАПИСЬ...] Говорите...                          \r")
		hud.SetRecording()
		recorder.Start()
	}

	onCancel := func() {
		_, _, _ = recorder.Stop()
		hud.SetIdle()
		fmt.Print("\r[Клик] Отмена записи / переключение клавиши        \r")
		time.Sleep(300 * time.Millisecond)
		fmt.Print("\r                                                    \r")
	}

	onStop := func() {
		if isBusy {
			return
		}
		isBusy = true
		hud.SetTranscribing()
		if cfg.SoundFeedback {
			feedback.PlayStopBeep()
		}

		go func() {
			defer func() {
				isBusy = false
			}()

			pcm, peakPercent, scale := recorder.Stop()
			if len(pcm) == 0 {
				hud.SetIdle()
				fmt.Print("\r                                                    \r")
				return
			}

			durationSec := float64(len(pcm)) / 32000.0

			// Must be at least ~0.2s of audio
			if len(pcm) < 6400 {
				hud.SetIdle()
				fmt.Print("\r                                                    \r")
				return
			}

			// Save to temporary WAV
			err := audio.SaveWAVFile(tempWavPath, pcm)
			if err != nil {
				hud.SetError("Ошибка WAV")
				fmt.Printf("\n[Ошибка сохранения WAV]: %v\n", err)
				return
			}

			fmt.Printf("\r[🎙️ АУДИО (%.1fс)]: Громкость: %.0f%% (усилено x%.1f)                \n", durationSec, peakPercent, scale)

			fmt.Print("[⏳ РАСПОЗНАВАНИЕ...] Подождите секунду...          \r")

			startT := time.Now()
			text, err := engine.Transcribe(tempWavPath)
			transcribeDur := time.Since(startT)

			if err != nil {
				hud.SetError("Ошибка GPU")
				fmt.Printf("\n[Ошибка распознавания]: %v\n", err)
				return
			}

			if text == "" {
				hud.SetNoSpeech()
				fmt.Print("\r[⚠️ Речь не распознана]                                        \n")
				return
			}

			// Voice command for Settings
			if isSettingsCommand(text) {
				fmt.Printf("\r[⚙️ ГОЛОСОВАЯ КОМАНДА (%dмс)]: «%s» -> Открытие настроек...\n", transcribeDur.Milliseconds(), text)
				openSettingsDialog()
				return
			}

			hud.SetDone(transcribeDur)
			fmt.Printf("\r[✨ ТЕКСТ (%dмс)]: «%s»                                            \n", transcribeDur.Milliseconds(), text)

			// Paste into the active window
			err = injector.PasteText(text)
			if err != nil {
				fmt.Printf("[Ошибка вставки]: %v\n", err)
			} else {
				if cfg.SoundFeedback {
					feedback.PlayDoneBeep()
				}
			}
		}()
	}

	listener = hook.NewKeyboardListener(onStart, onStop, onCancel)
	listener.SetTargetKey(cfg.Hotkey)

	err = listener.Start()
	if err != nil {
		log.Fatalf("Не удалось установить хук клавиатуры: %v", err)
	}

	<-sigChan

	fmt.Println("\nЗавершение работы OhMyVoice...")
	_ = os.Remove(tempWavPath)
}
