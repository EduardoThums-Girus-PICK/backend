package api

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/linuxtips/girus/internal/core"
)

// Handler gerencia as requisições HTTP
type Handler struct {
	labManager *core.LabManager
}

// NewHandler cria um novo handler
func NewHandler(labManager *core.LabManager) *Handler {
	return &Handler{
		labManager: labManager,
	}
}

// CreateLabRequest representa a solicitação para criar um laboratório
type CreateLabRequest struct {
	UserId     string `json:"userId"`
	TemplateId string `json:"templateId"`
}

// HandleCreateLab lida com a criação de um laboratório
func (h *Handler) HandleCreateLab(c *gin.Context) {
	var req CreateLabRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("erro ao ler solicitação: %v", err)})
		return
	}

	// Se userId não for fornecido, gerar um identificador aleatório
	if req.UserId == "" {
		req.UserId = generateUserId()
	}

	// Criar ambiente de laboratório
	if err := h.labManager.CreateLabEnvironment(req.UserId, req.TemplateId); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"userId":     req.UserId,
		"templateId": req.TemplateId,
		"message":    "Laboratório criado com sucesso",
	})
}

// HandleListTemplates retorna a lista de templates disponíveis
func (h *Handler) HandleListTemplates(c *gin.Context) {
	templates := h.labManager.GetAvailableTemplates()
	c.JSON(http.StatusOK, gin.H{
		"templates": templates,
	})
}

// ValidateTaskRequest representa a solicitação para validar uma tarefa
type ValidateTaskRequest struct {
	TemplateId string `json:"templateId"`
	TaskIndex  int    `json:"taskIndex"`
}

// ValidateLabRequest representa a solicitação para validar o laboratório completo
type ValidateLabRequest struct {
	TemplateId string `json:"templateId"`
}

// HandleValidateTask lida com a validação de tarefas do laboratório
func (h *Handler) HandleValidateTask(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	var req ValidateTaskRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("erro ao ler solicitação: %v", err)})
		return
	}

	// Obter pod
	pod, err := h.labManager.GetPod(namespace, podName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
		return
	}

	// Obter template
	template := h.labManager.GetTemplate(req.TemplateId)
	if template == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template não encontrado"})
		return
	}

	// Verificar índice da tarefa
	if req.TaskIndex < 0 || req.TaskIndex >= len(template.Tasks) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Índice de tarefa inválido"})
		return
	}

	// Validar a tarefa
	task := template.Tasks[req.TaskIndex]
	success, message := h.labManager.ValidateTask(pod, task)

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": message,
	})
}

// HandleValidateLab lida com a validação do laboratório completo
func (h *Handler) HandleValidateLab(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	var req ValidateLabRequest
	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("erro ao ler solicitação: %v", err)})
		return
	}

	// Obter pod
	pod, err := h.labManager.GetPod(namespace, podName)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
		return
	}

	// Validar o laboratório completo
	success, message := h.labManager.ValidateLabCompletion(pod, req.TemplateId)

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": message,
	})
}

// generateUserId gera um ID aleatório para o usuário
func generateUserId() string {
	b := make([]byte, 4)
	rand.Read(b)
	return hex.EncodeToString(b)
}
