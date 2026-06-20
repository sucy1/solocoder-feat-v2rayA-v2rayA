package controller

import (
	"github.com/gin-gonic/gin"
	"github.com/v2rayA/v2rayA/common"
	"github.com/v2rayA/v2rayA/server/service"
)

func GetVisualRouting(ctx *gin.Context) {
	config, err := service.GetVisualRouting()
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}
	common.ResponseSuccess(ctx, gin.H{
		"routing": config,
	})
}

func PutVisualRouting(ctx *gin.Context) {
	var config service.VisualRoutingConfig
	err := ctx.ShouldBindJSON(&config)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	err = service.SaveVisualRouting(&config)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, nil)
}

func PostRoutingReorder(ctx *gin.Context) {
	var data struct {
		FromIndex int `json:"fromIndex"`
		ToIndex   int `json:"toIndex"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	config, err := service.GetVisualRouting()
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	config, err = service.ReorderRules(config, data.FromIndex, data.ToIndex)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	err = service.SaveVisualRouting(config)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"routing": config,
	})
}

func PostRoutingImport(ctx *gin.Context) {
	var data struct {
		Format  string `json:"format"`
		Content string `json:"content"`
	}
	err := ctx.ShouldBindJSON(&data)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	var config *service.VisualRoutingConfig

	switch data.Format {
	case "surge":
		config, err = service.ImportSurgeRules(data.Content)
	case "clash":
		config, err = service.ImportClashRules(data.Content)
	default:
		common.ResponseError(ctx, logError("unsupported format"))
		return
	}

	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"routing": config,
	})
}

func GetRoutingJSON(ctx *gin.Context) {
	config, err := service.GetVisualRouting()
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	jsonStr, err := service.ConvertVisualToJSON(config)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"json": jsonStr,
	})
}

func PostVisualToJSON(ctx *gin.Context) {
	var config service.VisualRoutingConfig
	err := ctx.ShouldBindJSON(&config)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	jsonStr, err := service.ConvertVisualToJSON(&config)
	if err != nil {
		common.ResponseError(ctx, logError(err))
		return
	}

	common.ResponseSuccess(ctx, gin.H{
		"json": jsonStr,
	})
}

func PostVisualToRoutingA(ctx *gin.Context) {
	var config service.VisualRoutingConfig
	err := ctx.ShouldBindJSON(&config)
	if err != nil {
		common.ResponseError(ctx, logError("bad request"))
		return
	}

	routingA := service.ConvertVisualToRoutingA(&config)

	common.ResponseSuccess(ctx, gin.H{
		"routingA": routingA,
	})
}

func GetRoutingColors(ctx *gin.Context) {
	colors := map[string]interface{}{
		"ruleTypes": map[string]string{
			"domain":   "#3eaf7c",
			"ip":       "#42b983",
			"port":     "#f0a020",
			"protocol": "#722ed1",
			"network":  "#13c2c2",
			"process":  "#eb2f96",
			"geosite":  "#1890ff",
			"geoip":    "#fa8c16",
		},
		"outbounds": map[string]string{
			"proxy":  "#409eff",
			"direct": "#67c23a",
			"block":  "#f56c6c",
			"reject": "#f56c6c",
		},
	}
	common.ResponseSuccess(ctx, colors)
}
