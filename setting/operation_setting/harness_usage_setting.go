/*
Copyright (C) 2023-2026 xingguangcuican6666

This file is part of new-api, which is licensed under the GNU Affero General
Public License v3.0. Original project: https://github.com/QuantumNous/new-api

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.
*/

package operation_setting

import (
	"strings"

	"github.com/QuantumNous/new-api/setting/config"
)

// HarnessUsageSetting 控制是否把订阅额度用量写成 Claude Code / Codex CLI 能识别的
// 响应头。两个 harness 都从响应头读取 5h / 7d / 模型独立额度并在状态栏画原生白条，
// 而经过网关的请求默认看不到这些数字。总开关默认关闭：上报属于可选增强，且一旦打开
// 会改变客户端看到的额度语义。
type HarnessUsageSetting struct {
	// Enabled 总开关，关闭时快照与写头都零开销跳过。
	Enabled bool `json:"enabled"`
	// CodexLimitId 是 Codex 额度头族的家族名，默认 codex，对应 x-codex-primary-*。
	// 管理员改成别的名字时，客户端需要同样的 limit id 才能读到；一般不用改。
	CodexLimitId string `json:"codex_limit_id"`
}

var harnessUsageSetting = HarnessUsageSetting{
	Enabled:      false,
	CodexLimitId: "codex",
}

func init() {
	config.GlobalConfig.Register("harness_usage_setting", &harnessUsageSetting)
}

func GetHarnessUsageSetting() *HarnessUsageSetting {
	return &harnessUsageSetting
}

// CodexHeaderPrefix 返回 Codex 额度头的前缀，例如 x-codex。
// 家族名只保留小写字母、数字与连字符；非法或为空时回退 codex。
func (s *HarnessUsageSetting) CodexHeaderPrefix() string {
	id := strings.ToLower(strings.TrimSpace(s.CodexLimitId))
	var b strings.Builder
	for _, r := range id {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			b.WriteRune(r)
		}
	}
	id = strings.Trim(b.String(), "-")
	if id == "" {
		id = "codex"
	}
	return "x-" + id
}
