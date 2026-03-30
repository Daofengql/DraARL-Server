package handler

import (
	"log"
	"net/http"
	"strconv"
	"time"

	gormdb "draarl/internal/gormdb"

	"github.com/gin-gonic/gin"
)

// LogbookCreateRequest 创建通联日志请求
type LogbookCreateRequest struct {
	MyCallSign   string  `json:"my_callsign" binding:"required"`  // 我方呼号（必填）
	TimeUTC      string  `json:"time_utc" binding:"required"`     // UTC时间，格式：2006-01-02 15:04:05
	TxFrequency  float64 `json:"tx_frequency" binding:"required"` // 发射频率 (MHz)
	RxFrequency  float64 `json:"rx_frequency"`                    // 接收频率 (MHz)，不填则等于发射频率
	CQZone       int     `json:"cq_zone"`                         // CQ分区
	ITUZone      int     `json:"itu_zone"`                        // ITU分区
	Mode         string  `json:"mode" binding:"required"`         // 通信模式
	CallSign     string  `json:"callsign" binding:"required"`     // 对方呼号
	TheirRST     string  `json:"their_rst"`                       // 对方信号报告
	TheirPower   *int    `json:"their_power"`                     // 对方功率 (W)
	TheirQTH     string  `json:"their_qth"`                       // 对方QTH
	TheirRadio   string  `json:"their_radio"`                     // 对方电台型号
	TheirAntenna string  `json:"their_antenna"`                   // 对方天线
	MyRST        string  `json:"my_rst"`                          // 我方信号报告
	MyPower      *int    `json:"my_power"`                        // 我方功率 (W)
	MyQTH        string  `json:"my_qth"`                          // 我方QTH
	MyRadio      string  `json:"my_radio"`                        // 我方电台型号
	MyAntenna    string  `json:"my_antenna"`                      // 我方天线
	Notes        string  `json:"notes"`                           // 备注
}

// LogbookUpdateRequest 更新通联日志请求
type LogbookUpdateRequest struct {
	MyCallSign   string  `json:"my_callsign"`
	TimeUTC      string  `json:"time_utc"`
	TxFrequency  float64 `json:"tx_frequency"`
	RxFrequency  float64 `json:"rx_frequency"`
	CQZone       int     `json:"cq_zone"`
	ITUZone      int     `json:"itu_zone"`
	Mode         string  `json:"mode"`
	CallSign     string  `json:"callsign"`
	TheirRST     string  `json:"their_rst"`
	TheirPower   *int    `json:"their_power"`
	TheirQTH     string  `json:"their_qth"`
	TheirRadio   string  `json:"their_radio"`
	TheirAntenna string  `json:"their_antenna"`
	MyRST        string  `json:"my_rst"`
	MyPower      *int    `json:"my_power"`
	MyQTH        string  `json:"my_qth"`
	MyRadio      string  `json:"my_radio"`
	MyAntenna    string  `json:"my_antenna"`
	Notes        string  `json:"notes"`
}

// LogbookQueryRequest 查询参数
type LogbookQueryRequest struct {
	Page      int     `form:"page" binding:"min=1"`
	PageSize  int     `form:"page_size" binding:"min=1,max=100"`
	StartTime string  `form:"start_time"` // UTC时间，格式：2006-01-02 15:04:05
	EndTime   string  `form:"end_time"`   // UTC时间，格式：2006-01-02 15:04:05
	CallSign  string  `form:"callsign"`   // 对方呼号（模糊匹配）
	Frequency float64 `form:"frequency"`  // 频率（匹配发射或接收）
	Mode      string  `form:"mode"`       // 通信模式
}

// GetLogbooks 获取当前用户的通联日志列表
func GetLogbooks(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var req LogbookQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 设置默认值
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 10
	}

	// 构建查询参数
	params := gormdb.LogbookQueryParams{
		UserID:    uint(userID.(int)),
		Page:      req.Page,
		PageSize:  req.PageSize,
		CallSign:  req.CallSign,
		Frequency: req.Frequency,
		Mode:      req.Mode,
	}

	// 解析时间
	if req.StartTime != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
		if err == nil {
			params.StartTime = &t
		}
	}
	if req.EndTime != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
		if err == nil {
			params.EndTime = &t
		}
	}

	repo := gormdb.NewLogbookRepository()
	logbooks, total, err := repo.ListByUser(params)
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(logbooks))
	for _, lb := range logbooks {
		items = append(items, logbookToJSON(lb))
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total":     total,
			"items":     items,
			"page":      req.Page,
			"page_size": req.PageSize,
		},
	})
}

// GetLogbook 获取单条通联日志（用户只能查看自己的）
func GetLogbook(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	logbook, err := repo.GetByIDAndUser(uint(id), uint(userID.(int)))
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	if logbook == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "通联日志不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    logbookToJSON(logbook),
	})
}

// CreateLogbook 创建通联日志
func CreateLogbook(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var req LogbookCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// 解析时间
	timeUTC, err := time.Parse("2006-01-02 15:04:05", req.TimeUTC)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "时间格式错误，应为：2006-01-02 15:04:05",
		})
		return
	}

	// 如果接收频率为0，则等于发射频率
	rxFrequency := req.RxFrequency
	if rxFrequency == 0 {
		rxFrequency = req.TxFrequency
	}

	logbook := &gormdb.Logbook{
		UserID:       userID.(int),
		MyCallSign:   req.MyCallSign,
		TimeUTC:      timeUTC,
		TxFrequency:  req.TxFrequency,
		RxFrequency:  rxFrequency,
		CQZone:       req.CQZone,
		ITUZone:      req.ITUZone,
		Mode:         req.Mode,
		CallSign:     req.CallSign,
		TheirRST:     req.TheirRST,
		TheirPower:   req.TheirPower,
		TheirQTH:     req.TheirQTH,
		TheirRadio:   req.TheirRadio,
		TheirAntenna: req.TheirAntenna,
		MyRST:        req.MyRST,
		MyPower:      req.MyPower,
		MyQTH:        req.MyQTH,
		MyRadio:      req.MyRadio,
		MyAntenna:    req.MyAntenna,
		Notes:        req.Notes,
	}

	repo := gormdb.NewLogbookRepository()
	if err := repo.Create(logbook); err != nil {
		log.Printf("创建通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "创建通联日志失败",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"code":    201,
		"message": "创建成功",
		"data":    logbookToJSON(logbook),
	})
}

// UpdateLogbook 更新通联日志（用户只能更新自己的）
func UpdateLogbook(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	var req LogbookUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	logbook, err := repo.GetByIDAndUser(uint(id), uint(userID.(int)))
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	if logbook == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "通联日志不存在或无权修改",
		})
		return
	}

	// 更新字段
	if req.MyCallSign != "" {
		logbook.MyCallSign = req.MyCallSign
	}
	if req.TimeUTC != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.TimeUTC)
		if err == nil {
			logbook.TimeUTC = t
		}
	}
	if req.TxFrequency > 0 {
		logbook.TxFrequency = req.TxFrequency
	}
	if req.RxFrequency > 0 {
		logbook.RxFrequency = req.RxFrequency
	}
	if req.CQZone > 0 {
		logbook.CQZone = req.CQZone
	}
	if req.ITUZone > 0 {
		logbook.ITUZone = req.ITUZone
	}
	if req.Mode != "" {
		logbook.Mode = req.Mode
	}
	if req.CallSign != "" {
		logbook.CallSign = req.CallSign
	}
	if req.TheirRST != "" {
		logbook.TheirRST = req.TheirRST
	}
	if req.TheirPower != nil {
		logbook.TheirPower = req.TheirPower
	}
	if req.TheirQTH != "" {
		logbook.TheirQTH = req.TheirQTH
	}
	if req.TheirRadio != "" {
		logbook.TheirRadio = req.TheirRadio
	}
	if req.TheirAntenna != "" {
		logbook.TheirAntenna = req.TheirAntenna
	}
	if req.MyRST != "" {
		logbook.MyRST = req.MyRST
	}
	if req.MyPower != nil {
		logbook.MyPower = req.MyPower
	}
	if req.MyQTH != "" {
		logbook.MyQTH = req.MyQTH
	}
	if req.MyRadio != "" {
		logbook.MyRadio = req.MyRadio
	}
	if req.MyAntenna != "" {
		logbook.MyAntenna = req.MyAntenna
	}
	if req.Notes != "" {
		logbook.Notes = req.Notes
	}

	if err := repo.Update(logbook); err != nil {
		log.Printf("更新通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新通联日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data":    logbookToJSON(logbook),
	})
}

// DeleteLogbook 删除通联日志（用户只能删除自己的）
func DeleteLogbook(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	if err := repo.DeleteByUser(uint(id), uint(userID.(int))); err != nil {
		if err == gormdb.ErrRecordNotFound {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    404,
				"message": "通联日志不存在或无权删除",
			})
			return
		}
		log.Printf("删除通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除通联日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// BatchDeleteLogbooks 批量删除通联日志（用户只能删除自己的）
func BatchDeleteLogbooks(c *gin.Context) {
	userID, exists := c.Get("user_id")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{
			"code":    401,
			"message": "未授权",
		})
		return
	}

	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请选择要删除的记录",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	deleted, err := repo.BatchDeleteByUser(req.IDs, uint(userID.(int)))
	if err != nil {
		log.Printf("批量删除通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "批量删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
		"data": gin.H{
			"deleted": deleted,
		},
	})
}

// ========== 管理员接口 ==========

// AdminGetLogbooks 管理员获取所有通联日志列表
func AdminGetLogbooks(c *gin.Context) {
	var req LogbookQueryRequest
	if err := c.ShouldBindQuery(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	// 设置默认值
	if req.Page == 0 {
		req.Page = 1
	}
	if req.PageSize == 0 {
		req.PageSize = 10
	}

	// 构建查询参数
	params := gormdb.LogbookQueryParams{
		Page:      req.Page,
		PageSize:  req.PageSize,
		CallSign:  req.CallSign,
		Frequency: req.Frequency,
		Mode:      req.Mode,
	}

	// 解析时间
	if req.StartTime != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.StartTime)
		if err == nil {
			params.StartTime = &t
		}
	}
	if req.EndTime != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.EndTime)
		if err == nil {
			params.EndTime = &t
		}
	}

	// 解析用户ID筛选
	userIDStr := c.Query("user_id")
	if userIDStr != "" {
		userID, err := strconv.ParseUint(userIDStr, 10, 32)
		if err == nil {
			params.UserID = uint(userID)
		}
	}

	// 解析用户名筛选（模糊匹配）
	params.Username = c.Query("username")

	repo := gormdb.NewLogbookRepository()
	logbooks, total, err := repo.List(params)
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	// 转换为响应格式
	items := make([]gin.H, 0, len(logbooks))
	for _, lb := range logbooks {
		items = append(items, logbookToJSONWithUser(lb))
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data": gin.H{
			"total":     total,
			"items":     items,
			"page":      req.Page,
			"page_size": req.PageSize,
		},
	})
}

// AdminGetLogbook 管理员获取单条通联日志
func AdminGetLogbook(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	logbook, err := repo.GetByID(uint(id))
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	if logbook == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "通联日志不存在",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "成功",
		"data":    logbookToJSONWithUser(logbook),
	})
}

// AdminUpdateLogbook 管理员更新通联日志
func AdminUpdateLogbook(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	var req LogbookUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	logbook, err := repo.GetByID(uint(id))
	if err != nil {
		log.Printf("获取通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "获取通联日志失败",
		})
		return
	}

	if logbook == nil {
		c.JSON(http.StatusNotFound, gin.H{
			"code":    404,
			"message": "通联日志不存在",
		})
		return
	}

	// 更新字段
	if req.MyCallSign != "" {
		logbook.MyCallSign = req.MyCallSign
	}
	if req.TimeUTC != "" {
		t, err := time.Parse("2006-01-02 15:04:05", req.TimeUTC)
		if err == nil {
			logbook.TimeUTC = t
		}
	}
	if req.TxFrequency > 0 {
		logbook.TxFrequency = req.TxFrequency
	}
	if req.RxFrequency > 0 {
		logbook.RxFrequency = req.RxFrequency
	}
	if req.CQZone > 0 {
		logbook.CQZone = req.CQZone
	}
	if req.ITUZone > 0 {
		logbook.ITUZone = req.ITUZone
	}
	if req.Mode != "" {
		logbook.Mode = req.Mode
	}
	if req.CallSign != "" {
		logbook.CallSign = req.CallSign
	}
	if req.TheirRST != "" {
		logbook.TheirRST = req.TheirRST
	}
	if req.TheirPower != nil {
		logbook.TheirPower = req.TheirPower
	}
	if req.TheirQTH != "" {
		logbook.TheirQTH = req.TheirQTH
	}
	if req.TheirRadio != "" {
		logbook.TheirRadio = req.TheirRadio
	}
	if req.TheirAntenna != "" {
		logbook.TheirAntenna = req.TheirAntenna
	}
	if req.MyRST != "" {
		logbook.MyRST = req.MyRST
	}
	if req.MyPower != nil {
		logbook.MyPower = req.MyPower
	}
	if req.MyQTH != "" {
		logbook.MyQTH = req.MyQTH
	}
	if req.MyRadio != "" {
		logbook.MyRadio = req.MyRadio
	}
	if req.MyAntenna != "" {
		logbook.MyAntenna = req.MyAntenna
	}
	if req.Notes != "" {
		logbook.Notes = req.Notes
	}

	if err := repo.Update(logbook); err != nil {
		log.Printf("更新通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "更新通联日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "更新成功",
		"data":    logbookToJSONWithUser(logbook),
	})
}

// AdminDeleteLogbook 管理员删除通联日志
func AdminDeleteLogbook(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseUint(idStr, 10, 32)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "无效的ID",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	if err := repo.Delete(uint(id)); err != nil {
		log.Printf("删除通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "删除通联日志失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
	})
}

// AdminBatchDeleteLogbooks 管理员批量删除通联日志
func AdminBatchDeleteLogbooks(c *gin.Context) {
	var req struct {
		IDs []uint `json:"ids" binding:"required"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请求参数错误",
		})
		return
	}

	if len(req.IDs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    400,
			"message": "请选择要删除的记录",
		})
		return
	}

	repo := gormdb.NewLogbookRepository()
	if err := repo.BatchDelete(req.IDs); err != nil {
		log.Printf("批量删除通联日志失败: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    500,
			"message": "批量删除失败",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    200,
		"message": "删除成功",
		"data": gin.H{
			"deleted": len(req.IDs),
		},
	})
}

// ========== 辅助函数 ==========

// logbookToJSON 转换为JSON响应格式
func logbookToJSON(lb *gormdb.Logbook) gin.H {
	return gin.H{
		"id":            lb.ID,
		"user_id":       lb.UserID,
		"my_callsign":   lb.MyCallSign,
		"time_utc":      lb.TimeUTC.UTC().Format("2006-01-02 15:04:05"),
		"tx_frequency":  lb.TxFrequency,
		"rx_frequency":  lb.RxFrequency,
		"cq_zone":       lb.CQZone,
		"itu_zone":      lb.ITUZone,
		"mode":          lb.Mode,
		"callsign":      lb.CallSign,
		"their_rst":     lb.TheirRST,
		"their_power":   lb.TheirPower,
		"their_qth":     lb.TheirQTH,
		"their_radio":   lb.TheirRadio,
		"their_antenna": lb.TheirAntenna,
		"my_rst":        lb.MyRST,
		"my_power":      lb.MyPower,
		"my_qth":        lb.MyQTH,
		"my_radio":      lb.MyRadio,
		"my_antenna":    lb.MyAntenna,
		"notes":         lb.Notes,
		"created_at":    lb.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at":    lb.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}

// logbookToJSONWithUser 转换为JSON响应格式（包含用户名）
func logbookToJSONWithUser(lb *gormdb.Logbook) gin.H {
	// 获取用户名
	userRepo := gormdb.NewUserRepository()
	user, _ := userRepo.GetUserByID(int(lb.UserID))
	username := ""
	if user != nil {
		username = user.Name
	}

	return gin.H{
		"id":            lb.ID,
		"user_id":       lb.UserID,
		"username":      username,
		"my_callsign":   lb.MyCallSign,
		"time_utc":      lb.TimeUTC.UTC().Format("2006-01-02 15:04:05"),
		"tx_frequency":  lb.TxFrequency,
		"rx_frequency":  lb.RxFrequency,
		"cq_zone":       lb.CQZone,
		"itu_zone":      lb.ITUZone,
		"mode":          lb.Mode,
		"callsign":      lb.CallSign,
		"their_rst":     lb.TheirRST,
		"their_power":   lb.TheirPower,
		"their_qth":     lb.TheirQTH,
		"their_radio":   lb.TheirRadio,
		"their_antenna": lb.TheirAntenna,
		"my_rst":        lb.MyRST,
		"my_power":      lb.MyPower,
		"my_qth":        lb.MyQTH,
		"my_radio":      lb.MyRadio,
		"my_antenna":    lb.MyAntenna,
		"notes":         lb.Notes,
		"created_at":    lb.CreatedAt.Format("2006-01-02 15:04:05"),
		"updated_at":    lb.UpdatedAt.Format("2006-01-02 15:04:05"),
	}
}
