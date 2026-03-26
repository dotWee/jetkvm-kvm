package kvm

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

func ginCreateTestContext(w http.ResponseWriter, r *http.Request) (*gin.Context, *gin.Engine) {
	c, router := gin.CreateTestContext(w)
	c.Request = r
	return c, router
}

