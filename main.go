// Copyright (c) 2025 SeeKT
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
package main

import (
	"log"
	"myscreenshot-tool/config"
	"myscreenshot-tool/gui" // guiパッケージをインポート
)

func main() {
	// 設定のロード
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("Failed to load configuration: %v", err)
	}

	// GUIアプリケーションの初期化と実行
	appCtx := gui.NewApp(cfg)
	appCtx.Run()

	// アプリケーションが終了すると、SetOnClosed で設定を保存する処理が実行される
}
