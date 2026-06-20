package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetTransparentProxyStatus(ctx *gin.Context) {
	status := service.GetTransparentProxyStatus()
	common.ResponseSuccess(ctx, gin.H{
		"status": status,
	})
}

func PostSwitchProxyMode(ctx *gin.Context) {
	var data struct {
		Mode string `json:"mode"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	mode := service.ProxyMode(data.Mode)
	status, err := service.SwitchProxyMode(mode, "", ctx.ClientIP())
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"status": status,
	})
}

func GetAvailableProxyModes(ctx *gin.Context) {
	modes := service.GetAvailableProxyModes()
	common.ResponseSuccess(ctx, gin.H{
		"modes": modes,
	})
}

func PostRemoteSwitchProxyMode(ctx *gin.Context) {
	apiKey := ctx.GetHeader("X-API-Key")
	if apiKey == "" {
		apiKey = ctx.Query("apiKey")
	}

	if !service.ValidateRemoteApiKey(apiKey) {
		common.Response(ctx, common.UNAUTHORIZED, "invalid API key")
		return
	}

	var data struct {
		Mode string `json:"mode"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	mode := service.ProxyMode(data.Mode)
	status, err := service.SwitchProxyMode(mode, apiKey, ctx.ClientIP())
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"status": status,
	})
}

func GetRemoteApiKey(ctx *gin.Context) {
	setting := service.GetSetting()
	hasKey := setting.RemoteApiKey != ""
	common.ResponseSuccess(ctx, gin.H{
		"hasKey":    hasKey,
		"keyExists": hasKey,
	})
}

func PostRemoteApiKey(ctx *gin.Context) {
	var data struct {
		Key string `json:"key"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	err = service.SetRemoteApiKey(data.Key)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, nil)
}

func PostGenerateRemoteApiKey(ctx *gin.Context) {
	key := service.GenerateRemoteApiKey()
	common.ResponseSuccess(ctx, gin.H{
		"key": key,
	})
}
