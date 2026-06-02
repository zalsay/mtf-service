package handlers

import (
	"testing"

	"fintrack-api/models"

	"github.com/gin-gonic/gin"
)

func TestAssignStrategyParamsUserIDFromContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("user_id", 42)

	req := models.SaveStrategyParamsRequest{UniqueKey: "tpl_test"}
	assignStrategyParamsUserID(c, &req)

	if req.UserID == nil || *req.UserID != 42 {
		t.Fatalf("UserID = %#v, want 42", req.UserID)
	}
}

func TestAssignStrategyParamsUserIDKeepsRequestValue(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(nil)
	c.Set("user_id", 42)

	userID := 7
	req := models.SaveStrategyParamsRequest{UniqueKey: "tpl_test", UserID: &userID}
	assignStrategyParamsUserID(c, &req)

	if req.UserID == nil || *req.UserID != 7 {
		t.Fatalf("UserID = %#v, want existing value 7", req.UserID)
	}
}
