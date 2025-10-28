package handlers

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

type InfoHandler struct{}

func NewInfoHandler() *InfoHandler {
	return &InfoHandler{}
}

func (h *InfoHandler) Help(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "help.html", gin.H{
		"title": "Help",
		"user":  user,
	})
}

func (h *InfoHandler) About(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "about.html", gin.H{
		"title": "About",
		"user":  user,
	})
}

func (h *InfoHandler) Contact(c *gin.Context) {
	user, _ := GetCurrentUser(c)

	c.HTML(http.StatusOK, "contact.html", gin.H{
		"title": "Contact",
		"user":  user,
	})
}
