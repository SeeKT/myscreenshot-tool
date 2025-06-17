package screenshot

import (
	"fmt"
	"image"
	"image/png"
	"log"
	"os"
	"path/filepath"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// ウィンドウハンドル (HWND) を使いやすくするための型エイリアス
type HWND syscall.Handle

// RECT 構造体 (ウィンドウの座標情報)
type RECT struct {
	Left   int32
	Top    int32
	Right  int32
	Bottom int32
}

// Windows API のインポート
// これらは go.mod で golang.org/x/sys/windows を指定していれば利用可能
var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")

	enumWindowsProc         = user32.NewProc("EnumWindows")
	getWindowTextProc       = user32.NewProc("GetWindowTextW")
	getWindowTextLengthProc = user32.NewProc("GetWindowTextLengthW")
	isWindowVisibleProc     = user32.NewProc("IsWindowVisible")
	getWindowRectProc       = user32.NewProc("GetWindowRect")
	getWindowDCProc         = user32.NewProc("GetWindowDC")
	releaseDCProc           = user32.NewProc("ReleaseDC")
	printWindowProc         = user32.NewProc("PrintWindow")      // より信頼性の高いスクリーンショット取得方法
	getDesktopWindowProc    = user32.NewProc("GetDesktopWindow") // デスクトップウィンドウのハンドルを取得

	createCompatibleDCSingleProc = gdi32.NewProc("CreateCompatibleDC")
	createCompatibleBitmapProc   = gdi32.NewProc("CreateCompatibleBitmap")
	selectObjectProc             = gdi32.NewProc("SelectObject")
	deleteObjectProc             = gdi32.NewProc("DeleteObject")
	deleteDCProc                 = gdi32.NewProc("DeleteDC")
	bitBltProc                   = gdi32.NewProc("BitBlt") // PrintWindowが使えない場合のフォールバック
	getDIBitsProc                = gdi32.NewProc("GetDIBits")
)

// ウィンドウ情報を格納する構造体
type WindowInfo struct {
	HWND  HWND
	Title string
}

// EnumWindows のコールバック関数で使用するスライス
var windowList []WindowInfo

// EnumWindowsCallback は EnumWindows API のコールバック関数です。
// 見つかったウィンドウのハンドルとタイトルを取得し、windowList に追加します。
func EnumWindowsCallback(hwnd HWND, lParam uintptr) uintptr {
	// ウィンドウが可視であるかチェック
	ret, _, _ := isWindowVisibleProc.Call(uintptr(hwnd))
	if ret == 0 { // IsWindowVisible は非表示のウィンドウでは0を返す
		return 1 // true を返し、列挙を継続
	}

	// ウィンドウタイトルの長さを取得
	textLen, _, _ := getWindowTextLengthProc.Call(uintptr(hwnd))
	if textLen == 0 { // タイトルがないウィンドウはスキップ
		return 1
	}

	// タイトルバッファの準備
	buf := make([]uint16, textLen+1) // null終端のため+1

	// ウィンドウタイトルを取得
	getWindowTextProc.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(textLen+1))
	title := syscall.UTF16ToString(buf)

	// システムウィンドウやプログラムマネージャーなどを除外するための簡単なフィルタ
	// 必要に応じてより厳密なフィルタリングを追加
	if title == "Program Manager" || title == "Default IME" || title == "" {
		return 1
	}

	windowList = append(windowList, WindowInfo{HWND: hwnd, Title: title})
	return 1 // true を返し、列挙を継続
}

// GetWindowList は現在開いているウィンドウのリストを取得します。
func GetWindowList() ([]WindowInfo, error) {
	windowList = nil // リストをクリア
	// EnumWindows 関数はコールバック関数を呼び出し、すべてのトップレベルウィンドウを列挙する
	ret, _, err := enumWindowsProc.Call(syscall.NewCallback(EnumWindowsCallback), 0)
	if ret == 0 {
		return nil, fmt.Errorf("EnumWindows failed: %w", err)
	}
	return windowList, nil
}

// GetWindowTitle は指定されたHWNDのタイトルを取得します。
func GetWindowTitle(hwnd HWND) (string, error) {
	textLen, _, _ := getWindowTextLengthProc.Call(uintptr(hwnd))
	if textLen == 0 {
		return "", nil // タイトルがない場合
	}
	buf := make([]uint16, textLen+1)
	ret, _, err := getWindowTextProc.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), uintptr(textLen+1))
	if ret == 0 {
		return "", fmt.Errorf("GetWindowTextW failed: %w", err)
	}
	return syscall.UTF16ToString(buf), nil
}

// CaptureWindow は指定されたウィンドウのスクリーンショットを撮影し、image.Imageとして返します。
// PrintWindow API を優先的に使用します。
func CaptureWindow(hwnd HWND) (image.Image, error) {
	var rect RECT
	ret, _, err := getWindowRectProc.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&rect)))
	if ret == 0 {
		return nil, fmt.Errorf("GetWindowRect failed: %w", err)
	}

	width := int(rect.Right - rect.Left)
	height := int(rect.Bottom - rect.Top)

	// ウィンドウのDC (Device Context) を取得
	windowDC, _, err := getWindowDCProc.Call(uintptr(hwnd))
	if windowDC == 0 {
		return nil, fmt.Errorf("GetWindowDC failed: %w", err)
	}
	defer releaseDCProc.Call(uintptr(hwnd), windowDC) // 使用後に解放

	// 互換性のあるメモリDCを作成
	memDC, _, err := createCompatibleDCSingleProc.Call(windowDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC failed: %w", err)
	}
	defer deleteDCProc.Call(memDC) // 使用後に解放

	// 互換性のあるビットマップを作成
	hBitmap, _, err := createCompatibleBitmapProc.Call(windowDC, uintptr(width), uintptr(height))
	if hBitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap failed: %w", err)
	}
	defer deleteObjectProc.Call(hBitmap) // 使用後に解放

	// ビットマップをメモリDCに選択
	oldBitmap, _, err := selectObjectProc.Call(memDC, hBitmap)
	if oldBitmap == 0 { // oldBitmapは以前選択されていたオブジェクトのハンドル、エラーではない
		log.Printf("SelectObject returned 0, might be an issue. Error: %v", err)
	}
	defer selectObjectProc.Call(memDC, oldBitmap) // 元のビットマップに戻す

	// PrintWindow API を使用してウィンドウの内容をメモリDCに描画
	// PrintWindow は BitBlt よりも信頼性が高く、最小化されたウィンドウや重なったウィンドウも正しくキャプチャできる場合がある
	// PRF_CLIENT | PRF_NONCLIENT はクライアント領域と非クライアント領域の両方を含むことを意味する
	const PW_RENDERFULLWINDOW = 0x00000002 // PrintWindow flags (PRF_CLIENT | PRF_NONCLIENT | PRF_ERASEBKGND | PRF_CHILDREN)
	// ret を result に変更し、初期化子 := を使用
	result, _, _ := printWindowProc.Call(uintptr(hwnd), memDC, PW_RENDERFULLWINDOW)
	if result == 0 { // 修正: ret を result に変更
		// PrintWindow が失敗した場合、BitBlt でフォールバック (ただし、BitBltは一部のシナリオで問題がある)
		log.Printf("PrintWindow failed for HWND %d. Falling back to BitBlt. Error: %v", hwnd, err)
		// BitBlt (Source: windowDC, Dest: memDC)
		const SRCCOPY = 0x00CC0020
		bitBltProc.Call(memDC, 0, 0, uintptr(width), uintptr(height), windowDC, uintptr(rect.Left), uintptr(rect.Top), SRCCOPY)
	}

	// HBITMAP から Go の image.Image に変換
	img, err := bitmapToImage(HBITMAP(hBitmap), width, height)
	if err != nil {
		return nil, fmt.Errorf("failed to convert bitmap to image: %w", err)
	}

	return img, nil
}

// SaveScreenshotWithCounter は指定されたimage.Image、保存先ディレクトリ、およびシーケンスカウンターを元にPNG形式で画像を保存します。
func SaveScreenshotWithCounter(img image.Image, saveDir string, counter int) (string, error) {
	if err := os.MkdirAll(saveDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create save directory %s: %w", saveDir, err)
	}

	// タイムスタンプからミリ秒を除去
	timestamp := time.Now().Format("2006-01-02_15-04-05") // 年月日_時-分-秒
	// ファイル名を screenshot_YYYY-MM-DD_HH-MM-SS_0000.png 形式に変更
	fileName := fmt.Sprintf("screenshot_%s_%04d.png", timestamp, counter)
	filePath := filepath.Join(saveDir, fileName)

	file, err := os.Create(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to create screenshot file %s: %w", filePath, err)
	}
	defer file.Close()

	if err := png.Encode(file, img); err != nil {
		return "", fmt.Errorf("failed to encode PNG image to file %s: %w", filePath, err)
	}

	return filePath, nil
}

// bitmapToImage はHBITMAPをGoのimage.Imageに変換します。
// この部分は、より複雑なWindows APIのBitBltやGetDIBitsを使った処理が含まれます。
// Go 1.24.4 と golang.org/x/sys を使って直接ビットマップデータにアクセスする例を示します。
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors *uint32 // RGBQUAD array, or palette
}

type HBITMAP syscall.Handle

func bitmapToImage(hBitmap HBITMAP, width, height int) (image.Image, error) {
	// デスクトップDCを取得 (BitBlt時に必要)
	desktopDC, _, err := getDesktopWindowProc.Call()
	if desktopDC == 0 {
		return nil, fmt.Errorf("GetDesktopWindow failed: %w", err)
	}
	desktopHDC, _, err := getWindowDCProc.Call(desktopDC)
	if desktopHDC == 0 {
		return nil, fmt.Errorf("GetWindowDC failed for desktop: %w", err)
	}
	defer releaseDCProc.Call(desktopDC, desktopHDC)

	// BITMAPINFO構造体を準備
	bmi := BITMAPINFO{
		BmiHeader: BITMAPINFOHEADER{
			BiSize:        uint32(unsafe.Sizeof(BITMAPINFOHEADER{})),
			BiWidth:       int32(width),
			BiHeight:      int32(-height), // 負の値でトップダウンDIBを指定
			BiPlanes:      1,
			BiBitCount:    32, // 32-bit (RGBA)
			BiCompression: 0,  // BI_RGB (圧縮なし)
		},
	}

	// ビットマップデータを格納するバッファ
	pixelData := make([]byte, width*height*4) // 4 bytes per pixel (RGBA)

	// GetDIBits を呼び出してビットマップデータを取得
	// DIB_RGB_COLORS を指定し、ビットマップをピクセルデータに変換
	result, _, err := getDIBitsProc.Call(
		uintptr(desktopHDC),                    // HDC
		uintptr(hBitmap),                       // HBITMAP
		0,                                      // Start Scan Line
		uintptr(height),                        // Number of Scan Lines
		uintptr(unsafe.Pointer(&pixelData[0])), // lpBits
		uintptr(unsafe.Pointer(&bmi)),          // lpBMI
		0x00000000,                             // DIB_RGB_COLORS
	)

	if result == 0 { // 修正: ret を result に変更
		return nil, fmt.Errorf("GetDIBits failed: %w", err)
	}

	// RGBA形式の画像を作成
	// WindowsのDIBはBGRA形式で保存されることが多いので、RGBAに変換する必要がある
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			idx := (y*width + x) * 4
			// BGRA to RGBA
			img.Pix[idx] = pixelData[idx+2]   // R
			img.Pix[idx+1] = pixelData[idx+1] // G
			img.Pix[idx+2] = pixelData[idx]   // B
			img.Pix[idx+3] = pixelData[idx+3] // A (アルファチャンネル)
		}
	}

	return img, nil
}
