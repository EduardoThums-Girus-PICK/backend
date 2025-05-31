package core

import (
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	ReadBufferSize:  1024,
	WriteBufferSize: 1024,
	CheckOrigin: func(r *http.Request) bool {
		return true // Permitir conexões de qualquer origem em ambiente de desenvolvimento
	},
}

// HandleWebSocket gerencia conexões WebSocket genéricas
func (s *Server) HandleWebSocket(c *gin.Context) {
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Erro ao fazer upgrade da conexão: %v", err)
		return
	}
	defer conn.Close()

	log.Printf("Nova conexão WebSocket estabelecida")

	// Loop para manter a conexão ativa
	for {
		messageType, message, err := conn.ReadMessage()
		if err != nil {
			log.Printf("Erro ao ler mensagem: %v", err)
			break
		}

		log.Printf("Mensagem recebida: %s", message)

		// Responde com a mesma mensagem (echo)
		if err := conn.WriteMessage(messageType, message); err != nil {
			log.Printf("Erro ao enviar mensagem: %v", err)
			break
		}
	}

	log.Printf("Conexão WebSocket encerrada")
}

// Registrar a nova rota no SetupRoutes
func (s *Server) SetupWebSocketRoutes(router *gin.Engine) {
	// Rota específica para WebSocket
	router.GET("/ws", s.HandleWebSocket)
}
