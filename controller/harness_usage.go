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

package controller

import (
	"net/http"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/i18n"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/service"

	"github.com/gin-gonic/gin"
)

// GetHarnessUsage backs GET /api/oauth/usage, the endpoint Claude Code's /usage
// page reads. It reports the same subscription snapshot the relay response
// headers carry, in Anthropic's own JSON shape.
//
// The wallet balance is read here (the relay path gets it from the pre-consume
// billing result), and only to state whether overage is available.
func GetHarnessUsage(c *gin.Context) {
	userId := c.GetInt("id")
	if userId <= 0 {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": gin.H{
				"type":    "authentication_error",
				"message": "invalid token",
			},
		})
		return
	}

	walletQuota, err := model.GetUserQuota(userId, false)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": gin.H{
				"type":    "api_error",
				"message": common.TranslateMessage(c, i18n.MsgDatabaseError),
			},
		})
		return
	}

	response := service.BuildHarnessUsageResponse(userId, walletQuota)
	if response == nil {
		// No opted-in subscription, or the feature is off. An empty object is the
		// truthful answer: every optional field is absent, so the page renders no
		// bars instead of zeros that look like a real, exhausted quota.
		c.JSON(http.StatusOK, gin.H{})
		return
	}
	c.JSON(http.StatusOK, response)
}
