package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sync"
	"time"

	"github.com/v2rayA/v2rayA/core/v2ray"
	"github.com/v2rayA/v2rayA/db/configure"
	"github.com/v2rayA/v2rayA/pkg/util/log"
)

type ProxyMode string

const (
	ProxyModeGlobal  ProxyMode = "global"
	ProxyModeRule    ProxyMode = "rule"
	ProxyModeDirect  ProxyMode = "direct"
)

type TransparentProxyStatus struct {
	CurrentMode   ProxyMode `json:"currentMode"`
	Enabled       bool      `json:"enabled"`
	TransparentType string    `json:"transparentType"`
	LastChangedAt time.Time `json:"lastChangedAt"`
	LastChangedBy string    `json:"lastChangedBy"`
}

var (
	proxyStatusMu      sync.RWMutex
	proxyStatus        *TransparentProxyStatus
	proxyModeToTransparent = map[ProxyMode]configure.TransparentMode{
		ProxyModeGlobal: configure.TransparentProxy,
		ProxyModeRule:   configure.TransparentFollowRule,
		ProxyModeDirect: configure.TransparentClose,
	}
	transparentToProxyMode = map[configure.TransparentMode]ProxyMode{
		configure.TransparentProxy:      ProxyModeGlobal,
		configure.TransparentFollowRule: ProxyModeRule,
		configure.TransparentClose:      ProxyModeDirect,
		configure.TransparentWhitelist:  ProxyModeRule,
		configure.TransparentGfwlist:    ProxyModeRule,
	}
)

func GetTransparentProxyStatus() *TransparentProxyStatus {
	proxyStatusMu.RLock()
	defer proxyStatusMu.RUnlock()

	setting := configure.GetSettingNotNil()
	mode := getProxyModeFromSetting(setting)

	if proxyStatus == nil {
		proxyStatus = &TransparentProxyStatus{
			CurrentMode:   mode,
			Enabled:       v2ray.IsTransparentOn(setting),
			TransparentType: string(setting.TransparentType),
			LastChangedAt: time.Now(),
		}
	} else {
		proxyStatus.CurrentMode = mode
		proxyStatus.Enabled = v2ray.IsTransparentOn(setting)
		proxyStatus.TransparentType = string(setting.TransparentType)
	}

	return &TransparentProxyStatus{
		CurrentMode:     proxyStatus.CurrentMode,
		Enabled:         proxyStatus.Enabled,
		TransparentType: proxyStatus.TransparentType,
		LastChangedAt:   proxyStatus.LastChangedAt,
		LastChangedBy:   proxyStatus.LastChangedBy,
	}
}

func getProxyModeFromSetting(setting *configure.Setting) ProxyMode {
	if mode, ok := transparentToProxyMode[setting.Transparent]; ok {
		return mode
	}
	return ProxyModeRule
}

func SwitchProxyMode(mode ProxyMode, apiKey string, remoteAddr string) (*TransparentProxyStatus, error) {
	transparentMode, ok := proxyModeToTransparent[mode]
	if !ok {
		return nil, fmt.Errorf("invalid proxy mode: %s", mode)
	}

	setting := configure.GetSettingNotNil()
	if setting.Transparent == transparentMode {
		status := GetTransparentProxyStatus()
		return status, nil
	}

	setting.Transparent = transparentMode

	err := configure.SetSetting(setting)
	if err != nil {
		return nil, fmt.Errorf("failed to save setting: %w", err)
	}

	err = v2ray.UpdateV2RayConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to update v2ray config: %w", err)
	}

	proxyStatusMu.Lock()
	defer proxyStatusMu.Unlock()

	if proxyStatus == nil {
		proxyStatus = &TransparentProxyStatus{}
	}

	proxyStatus.CurrentMode = mode
	proxyStatus.Enabled = v2ray.IsTransparentOn(setting)
	proxyStatus.TransparentType = string(setting.TransparentType)
	proxyStatus.LastChangedAt = time.Now()
	if apiKey != "" {
		proxyStatus.LastChangedBy = "api:" + remoteAddr
	} else {
		proxyStatus.LastChangedBy = "web"
	}

	log.Info("Proxy mode switched to %s by %s", mode, proxyStatus.LastChangedBy)

	return &TransparentProxyStatus{
		CurrentMode:     proxyStatus.CurrentMode,
		Enabled:         proxyStatus.Enabled,
		TransparentType: proxyStatus.TransparentType,
		LastChangedAt:   proxyStatus.LastChangedAt,
		LastChangedBy:   proxyStatus.LastChangedBy,
	}, nil
}

func ValidateRemoteApiKey(apiKey string) bool {
	if apiKey == "" {
		return false
	}

	setting := configure.GetSettingNotNil()
	if setting.RemoteApiKey == "" {
		return false
	}

	hash := sha256.Sum256([]byte(apiKey))
	keyHash := sha256.Sum256([]byte(setting.RemoteApiKey))

	return hex.EncodeToString(hash[:]) == hex.EncodeToString(keyHash[:])
}

func GetAvailableProxyModes() []map[string]interface{} {
	return []map[string]interface{}{
		{
			"mode":        string(ProxyModeGlobal),
			"name":        "全局代理",
			"description": "所有流量都走代理",
			"color":       "#409eff",
			"transparent": configure.TransparentProxy,
		},
		{
			"mode":        string(ProxyModeRule),
			"name":        "规则路由",
			"description": "根据规则自动选择代理或直连",
			"color":       "#67c23a",
			"transparent": configure.TransparentFollowRule,
		},
		{
			"mode":        string(ProxyModeDirect),
			"name":        "直连模式",
			"description": "所有流量都直连，不走代理",
			"color":       "#f56c6c",
			"transparent": configure.TransparentClose,
		},
	}
}

func SetRemoteApiKey(key string) error {
	setting := configure.GetSettingNotNil()
	setting.RemoteApiKey = key
	return configure.SetSetting(setting)
}

func GenerateRemoteApiKey() string {
	b := make([]byte, 32)
	for i := range b {
		b[i] = byte(time.Now().UnixNano() % 256)
	}
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])[:32]
}
