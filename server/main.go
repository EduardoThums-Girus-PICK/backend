package main

import (
	"log"
	"os"
	"strconv"

	"github.com/linuxtips/girus/server/internal/core"
)

var version = "0.1"

func main() {
	log.Printf("Iniciando o Girus Server v%s", version)

	// Inicializar configuração
	portStr := os.Getenv("PORT")
	if portStr == "" {
		portStr = "8080"
	}
	
	// Converter porta de string para int
	port, err := strconv.Atoi(portStr)
	if err != nil {
		log.Fatalf("Porta inválida: %s, erro: %v", portStr, err)
	}

	// Inicializar configuração
	config := &core.Config{
		Port: port,
	}

	// Inicializar o servidor
	server := core.NewServer(config)

	// Iniciar o servidor
	if err := server.Run(); err != nil {
		log.Fatalf("Erro ao iniciar o servidor: %v", err)
	}
} 