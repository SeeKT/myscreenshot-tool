// Copyright (c) 2025 SeeKT
// This source code is licensed under the MIT license found in the LICENSE file in the root directory of this source tree.
package config

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"
)

// Config はアプリケーションの設定を保持する構造体です。
type Config struct {
	SaveDirectory   string `json:"save_directory"`
	IntervalMs      int    `json:"interval_ms"`      // スクリーンショット取得間隔（ミリ秒）
	CaptureDuration int    `json:"capture_duration"` // 撮影継続時間（分、0で手動停止）

	// 選択されたウィンドウの情報を保持する構造体
	SelectedWindow struct {
		HWND  uintptr `json:"hwnd"`
		Title string  `json:"title"`
	} `json:"selected_window"`
}

// WindowSetting は選択されたウィンドウの識別情報を保持します。
type WindowSetting struct {
	HWND  uintptr `json:"hwnd"`  // ウィンドウハンドル
	Title string  `json:"title"` // ウィンドウタイトル
}

// NewDefaultConfig はデフォルトの設定値を返します。
func NewDefaultConfig() *Config {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// エラーが発生した場合のフォールバックとしてカレントディレクトリを使用
		log.Printf("Warning: Could not get user home directory: %v. Using current directory for screenshots.", err)
		homeDir = "."
	}

	return &Config{
		SaveDirectory:   filepath.Join(homeDir, "screenshots"), // ユーザーのホームディレクトリに"screenshots"フォルダ
		IntervalMs:      1000,                                  // 1秒 (1000ミリ秒)
		CaptureDuration: 60,                                    // 1時間 (60分)
		SelectedWindow: WindowSetting{
			HWND:  0, // デフォルトでは未選択
			Title: "",
		},
	}
}

// ConfigFilePath は設定ファイルのパスを返します。
func ConfigFilePath() (string, error) {
	configDir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user config directory: %w", err)
	}
	appConfigDir := filepath.Join(configDir, "myscreenshot-tool") // アプリケーション固有のディレクトリ
	if err := os.MkdirAll(appConfigDir, 0755); err != nil {
		return "", fmt.Errorf("failed to create config directory %s: %w", appConfigDir, err)
	}
	return filepath.Join(appConfigDir, "config.json"), nil
}

// LoadConfig は設定ファイルを読み込みます。ファイルが存在しない場合はデフォルト設定を返します。
func LoadConfig() (*Config, error) {
	cfgPath, err := ConfigFilePath()
	if err != nil {
		return nil, fmt.Errorf("failed to get config file path: %w", err)
	}

	cfg := NewDefaultConfig() // まずデフォルト設定をロード

	data, err := os.ReadFile(cfgPath)
	if err != nil {
		if os.IsNotExist(err) {
			// ファイルが存在しない場合はデフォルト設定を返して終了
			fmt.Printf("Config file not found at %s. Using default settings.\n", cfgPath)
			return cfg, nil
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", cfgPath, err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to unmarshal config data: %w", err)
	}
	fmt.Printf("Config loaded from %s\n", cfgPath)
	return cfg, nil
}

// SaveConfig は現在の設定をファイルに保存します。
func SaveConfig(cfg *Config) error {
	cfgPath, err := ConfigFilePath()
	if err != nil {
		return fmt.Errorf("failed to get config file path: %w", err)
	}

	data, err := json.MarshalIndent(cfg, "", "  ") // JSONを整形して保存
	if err != nil {
		return fmt.Errorf("failed to marshal config data: %w", err)
	}

	if err := os.WriteFile(cfgPath, data, 0644); err != nil {
		return fmt.Errorf("failed to write config file %s: %w", cfgPath, err)
	}
	fmt.Printf("Config saved to %s\n", cfgPath)
	return nil
}

// GetIntervalDuration はミリ秒単位の IntervalMs を time.Duration に変換して返します。
func (c *Config) GetIntervalDuration() time.Duration {
	return time.Duration(c.IntervalMs) * time.Millisecond
}

// GetCaptureDuration は分単位の CaptureDuration を time.Duration に変換して返します。
func (c *Config) GetCaptureDuration() time.Duration {
	return time.Duration(c.CaptureDuration) * time.Minute
}
