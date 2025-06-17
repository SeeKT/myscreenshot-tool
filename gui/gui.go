package gui

import (
	"context"
	"fmt"
	"log"
	"os"
	"strconv"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/layout"
	"fyne.io/fyne/v2/widget"

	"myscreenshot-tool/config"
	"myscreenshot-tool/screenshot"
)

// AppContext はアプリケーションの状態と設定、Fyneのウィンドウなどを保持します。
type AppContext struct {
	App         fyne.App
	Window      fyne.Window
	Config      *config.Config // アプリケーション設定
	CaptureCtx  context.Context
	CaptureStop context.CancelFunc // 撮影停止用のキャンセル関数
	IsCapturing bool               // 撮影中かどうかを示すフラグ
	CaptureMu   sync.Mutex         // 撮影状態変更の排他制御

	// GUI Widgets
	saveDirEntry      *widget.Entry
	intervalEntry     *widget.Entry
	durationEntry     *widget.Entry
	windowSelect      *widget.Select // ウィンドウタイトル一覧からの選択
	startButton       *widget.Button
	stopButton        *widget.Button
	statusLabel       *widget.Label
	captureCountLabel *widget.Label
	countdownLabel    *widget.Label

	selectedWindowInfo screenshot.WindowInfo // ユーザーが選択したウィンドウのHWNDとタイトル
}

// NewApp は新しいアプリケーションコンテキストを作成し、GUIを初期化します。
func NewApp(cfg *config.Config) *AppContext {
	a := app.New()
	w := a.NewWindow("Go Screenshot Tool")

	appCtx := &AppContext{
		App:    a,
		Window: w,
		Config: cfg,
	}

	appCtx.createUI()       // UIコンポーネントを構築
	appCtx.loadConfigToUI() // 設定をUIにロード
	appCtx.updateControlButtons()

	w.SetFixedSize(true)             // ウィンドウサイズを固定 (必要に応じて調整)
	w.Resize(fyne.NewSize(500, 450)) // ウィンドウの初期サイズ

	// ウィンドウが閉じられたときの処理
	w.SetOnClosed(func() {
		// アプリケーション終了時に設定を保存
		if err := config.SaveConfig(appCtx.Config); err != nil {
			log.Printf("Failed to save config on exit: %v", err)
		}
		// 撮影中であれば停止シグナルを送る
		appCtx.CaptureMu.Lock()
		if appCtx.IsCapturing && appCtx.CaptureStop != nil {
			appCtx.CaptureStop()
		}
		appCtx.CaptureMu.Unlock()
	})

	return appCtx
}

// createUI はGUIコンポーネントを構築し、ウィンドウに配置します。
func (ac *AppContext) createUI() {
	// --- 保存先設定 ---
	ac.saveDirEntry = widget.NewEntry()
	ac.saveDirEntry.PlaceHolder = "Select save directory" // 修正
	ac.saveDirEntry.OnChanged = func(s string) {
		ac.Config.SaveDirectory = s
	}

	browseButton := widget.NewButton("Browse", func() {
		dialog.ShowFolderOpen(func(uri fyne.ListableURI, err error) {
			if err != nil {
				dialog.ShowError(err, ac.Window)
				return
			}
			if uri != nil {
				ac.saveDirEntry.SetText(uri.Path())
			}
		}, ac.Window)
	})

	saveDirContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(450, 35)),
		ac.saveDirEntry,
		browseButton,
	)

	// --- 取得間隔設定 (ミリ秒) ---
	ac.intervalEntry = widget.NewEntry()
	ac.intervalEntry.SetPlaceHolder("Interval (ms, e.g., 1000 for 1 shot/sec)") // プレースホルダーのテキストも更新
	ac.intervalEntry.Validator = func(s string) error {
		val, err := strconv.Atoi(s)
		if err != nil {
			return fmt.Errorf("must be a positive integer") // エラーメッセージは変わらず
		}
		// 修正: 最小値を100msに設定
		if val < 100 {
			return fmt.Errorf("interval must be at least 100ms") // 新しいエラーメッセージ
		}
		return nil
	}
	ac.intervalEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err == nil {
			ac.Config.IntervalMs = val
		}
	}

	// --- 取得時間設定 (分) ---
	ac.durationEntry = widget.NewEntry()
	ac.durationEntry.SetPlaceHolder("Capture Duration (minutes, 0 for manual stop)")
	ac.durationEntry.Validator = func(s string) error {
		val, err := strconv.Atoi(s)
		if err != nil || val < 0 {
			return fmt.Errorf("must be a non-negative integer")
		}
		return nil
	}
	ac.durationEntry.OnChanged = func(s string) {
		val, err := strconv.Atoi(s)
		if err == nil {
			ac.Config.CaptureDuration = val
		}
	}

	// --- ウィンドウ選択 ---
	ac.windowSelect = widget.NewSelect([]string{}, func(s string) {
		// ここで選択された文字列からHWNDを特定する必要がある
		// これは後で showWindowSelectionDialog で処理する
		// 現在は showWindowSelectionDialog で直接 ac.Config.SelectedWindow を更新しているので、
		// ここでは何もしなくても良い
	})
	ac.windowSelect.PlaceHolder = "Select a window to capture" // 修正
	ac.windowSelect.Disable()                                  // 初期状態では無効 (selectWindowButton でダイアログを開く)

	selectWindowButton := widget.NewButton("Select Window", func() {
		ac.showWindowSelectionDialog()
	})

	windowSelectionContainer := container.New(layout.NewGridWrapLayout(fyne.NewSize(450, 35)),
		ac.windowSelect,
		selectWindowButton,
	)

	// --- コントロールボタン ---
	ac.startButton = widget.NewButton("Start Capture", ac.startCapture)
	ac.stopButton = widget.NewButton("Stop Capture", ac.stopCapture)
	ac.stopButton.Disable() // 初期状態では停止ボタンは無効

	controlButtons := container.New(layout.NewGridWrapLayout(fyne.NewSize(200, 35)),
		ac.startButton,
		ac.stopButton,
	)

	// --- ステータス表示 ---
	ac.statusLabel = widget.NewLabel("Status: Idle")
	ac.statusLabel.TextStyle = fyne.TextStyle{Bold: true}
	ac.captureCountLabel = widget.NewLabel("Screenshots: 0")
	ac.countdownLabel = widget.NewLabel("Remaining: --:--:--")

	statusContainer := container.NewVBox(
		ac.statusLabel,
		ac.captureCountLabel,
		ac.countdownLabel,
	)

	// --- レイアウト ---
	content := container.NewVBox(
		widget.NewLabel("Screenshot Settings:"),
		container.New(layout.NewFormLayout(),
			widget.NewLabel("Save Directory:"), saveDirContainer,
			widget.NewLabel("Interval (ms):"), ac.intervalEntry,
			widget.NewLabel("Duration (min):"), ac.durationEntry,
			widget.NewLabel("Target Window:"), windowSelectionContainer,
		),
		widget.NewSeparator(),
		controlButtons,
		widget.NewSeparator(),
		statusContainer,
	)

	ac.Window.SetContent(content)
}

// loadConfigToUI はConfig構造体の値をUI要素にロードします。
func (ac *AppContext) loadConfigToUI() {
	ac.saveDirEntry.SetText(ac.Config.SaveDirectory)
	ac.intervalEntry.SetText(strconv.Itoa(ac.Config.IntervalMs))
	ac.durationEntry.SetText(strconv.Itoa(ac.Config.CaptureDuration))

	if ac.Config.SelectedWindow.HWND != 0 {
		ac.selectedWindowInfo = screenshot.WindowInfo{
			HWND:  screenshot.HWND(ac.Config.SelectedWindow.HWND),
			Title: ac.Config.SelectedWindow.Title,
		}

		ac.windowSelect.PlaceHolder = fmt.Sprintf("Selected: %s", ac.Config.SelectedWindow.Title) // 修正
	}
}

// updateControlButtons は現在の撮影状態に基づいてボタンの有効/無効を切り替えます。
func (ac *AppContext) updateControlButtons() {
	ac.CaptureMu.Lock()
	defer ac.CaptureMu.Unlock()

	if ac.IsCapturing {
		ac.startButton.Disable()
		ac.stopButton.Enable()
		ac.saveDirEntry.Disable()
		ac.intervalEntry.Disable()
		ac.durationEntry.Disable()
	} else {
		ac.startButton.Enable()
		ac.stopButton.Disable()
		ac.saveDirEntry.Enable()
		ac.intervalEntry.Enable()
		ac.durationEntry.Enable()
	}
}

// showWindowSelectionDialog は利用可能なウィンドウ一覧を表示し、ユーザーに選択させます。
func (ac *AppContext) showWindowSelectionDialog() {
	windows, err := screenshot.GetWindowList()
	if err != nil {
		dialog.ShowError(fmt.Errorf("failed to get window list: %w", err), ac.Window)
		return
	}

	if len(windows) == 0 {
		dialog.ShowInformation("No Windows Found", "No visible application windows were found.", ac.Window)
		return
	}

	// 表示用の文字列スライスと、HWNDへのマッピング
	var titles []string
	hwndMap := make(map[string]screenshot.WindowInfo)
	for _, win := range windows {
		title := fmt.Sprintf("[%d] %s", win.HWND, win.Title) // HWNDも表示に含める
		titles = append(titles, title)
		hwndMap[title] = win
	}

	// 選択ダイアログを作成
	list := widget.NewList(
		func() int { return len(titles) },
		func() fyne.CanvasObject { return widget.NewLabel("template") },
		func(id widget.ListItemID, obj fyne.CanvasObject) {
			obj.(*widget.Label).SetText(titles[id])
		},
	)

	// 選択されたときの処理
	list.OnSelected = func(id widget.ListItemID) {
		selectedTitle := titles[id]
		selectedWinInfo := hwndMap[selectedTitle]
		ac.selectedWindowInfo = selectedWinInfo
		ac.windowSelect.PlaceHolder = fmt.Sprintf("Selected: %s", selectedWinInfo.Title) // Selectのプレースホルダーを更新
		ac.Config.SelectedWindow.HWND = uintptr(selectedWinInfo.HWND)
		ac.Config.SelectedWindow.Title = selectedWinInfo.Title
	}

	// 既存の選択があればスクロールして表示 (id の問題修正)
	if ac.selectedWindowInfo.HWND != 0 {
		for i, title := range titles {
			// 表示文字列が HWND を含んでいるので、それと照合
			if title == fmt.Sprintf("[%d] %s", ac.selectedWindowInfo.HWND, ac.selectedWindowInfo.Title) {
				list.ScrollTo(widget.ListItemID(i))
				break
			}
		}
	}

	// ダイアログとして表示
	confirmDialog := dialog.NewCustomConfirm("Select Window", "Select", "Cancel", container.NewScroll(list), func(b bool) {
		// ダイアログが閉じられたときの処理 (ここでは特に何もしない)
	}, ac.Window)

	// リストが小さすぎる場合があるため、最低限のサイズを設定
	confirmDialog.Resize(fyne.NewSize(400, 300))
	confirmDialog.Show()
}

// startCapture はスクリーンショット撮影を開始します。
func (ac *AppContext) startCapture() {
	ac.CaptureMu.Lock()
	if ac.IsCapturing {
		ac.CaptureMu.Unlock()
		return // 既に撮影中
	}
	ac.IsCapturing = true
	ac.CaptureMu.Unlock()

	ac.updateControlButtons()
	ac.statusLabel.SetText("Status: Capturing...")
	ac.captureCountLabel.SetText("Screenshots: 0")
	ac.countdownLabel.SetText("Remaining: calculating...")

	if ac.selectedWindowInfo.HWND == 0 {
		dialog.ShowError(fmt.Errorf("Please select a window to capture."), ac.Window)
		ac.stopCapture() // エラーの場合は停止状態に戻す
		return
	}

	// コンテキストを再作成 (以前のキャンセル関数をクリア)
	ac.CaptureCtx, ac.CaptureStop = context.WithCancel(context.Background())

	go ac.runCaptureLoop() // 別Goroutineで撮影ループを実行
}

// stopCapture はスクリーンショット撮影を停止します。
func (ac *AppContext) stopCapture() {
	ac.CaptureMu.Lock()
	if !ac.IsCapturing {
		ac.CaptureMu.Unlock()
		return // 撮影中でない
	}
	ac.IsCapturing = false
	if ac.CaptureStop != nil {
		ac.CaptureStop() // 撮影Goroutineに停止を通知
	}
	ac.CaptureMu.Unlock()

	ac.updateControlButtons()
	ac.statusLabel.SetText("Status: Stopped")
	ac.countdownLabel.SetText("Remaining: --:--:--")
}

// runCaptureLoop は実際のスクリーンショット撮影ループを実行します。
func (ac *AppContext) runCaptureLoop() {
	defer func() {
		ac.CaptureMu.Lock()
		ac.IsCapturing = false // ループ終了時にフラグをリセット
		ac.CaptureMu.Unlock()
		ac.updateControlButtons() // UIを更新
		ac.statusLabel.SetText("Status: Idle")
	}()

	interval := ac.Config.GetIntervalDuration()
	captureDuration := ac.Config.GetCaptureDuration()
	targetHWND := ac.selectedWindowInfo.HWND
	saveDir := ac.Config.SaveDirectory

	if interval <= 0 {
		log.Println("Invalid interval specified. Stopping capture.")
		dialog.ShowError(fmt.Errorf("capture interval must be positive"), ac.Window)
		return
	}

	if err := os.MkdirAll(saveDir, 0755); err != nil {
		dialog.ShowError(fmt.Errorf("failed to create save directory %s: %w", saveDir, err), ac.Window)
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	var timer *time.Timer
	if captureDuration > 0 {
		timer = time.NewTimer(captureDuration)
		defer timer.Stop()
	}

	captureCount := 0
	fileSequenceCounter := 0 // スクリーンショットを保存するたびに増加

	startTime := time.Now()
	lastSecond := startTime.Second() // 追加: 前回の秒を記録

	go func() {
		for {
			select {
			case <-ac.CaptureCtx.Done():
				return // 撮影が停止された
			case <-time.After(time.Second): // 1秒ごとにカウントダウンを更新
				if captureDuration > 0 {
					elapsed := time.Since(startTime)
					remaining := captureDuration - elapsed
					if remaining < 0 {
						remaining = 0
					}
					ac.countdownLabel.SetText(fmt.Sprintf("Remaining: %s", formatDuration(remaining)))
				} else {
					ac.countdownLabel.SetText("Remaining: Manual Stop")
				}
			}
		}
	}()

	for {
		select {
		case <-ac.CaptureCtx.Done():
			log.Println("Capture loop finished due to cancellation.")
			return
		case <-func() <-chan time.Time {
			if timer != nil {
				return timer.C
			}
			return nil
		}():
			if captureDuration > 0 {
				log.Println("Capture duration elapsed. Stopping capture.")
				return
			}
		case t := <-ticker.C: // ticker.C から現在の時刻を取得
			// 秒が更新されたかチェックし、カウンターをリセット
			currentSecond := t.Second()
			if currentSecond != lastSecond {
				fileSequenceCounter = 0 // 秒が変わったらカウンターをリセット
				lastSecond = currentSecond
			}

			img, err := screenshot.CaptureWindow(targetHWND)
			if err != nil {
				log.Printf("Error capturing screenshot for HWND %d: %v\n", targetHWND, err)
				continue
			}

			_, err = screenshot.SaveScreenshotWithCounter(img, saveDir, fileSequenceCounter)
			if err != nil {
				log.Printf("Error saving screenshot: %v\n", err)
			} else {
				captureCount++
				fileSequenceCounter++ // カウンターをインクリメント
				ac.captureCountLabel.SetText(fmt.Sprintf("Screenshots: %d", captureCount))
			}
		}
	}
}

// formatDuration は time.Duration を HH:MM:SS 形式の文字列に変換
func formatDuration(d time.Duration) string {
	d = d.Round(time.Second) // 秒単位に丸める
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute
	d -= m * time.Minute
	s := d / time.Second
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// Run はFyneアプリケーションを実行します。
func (ac *AppContext) Run() {
	ac.Window.ShowAndRun()
}
