package core

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/gin-contrib/cors"
	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
)

type Server struct {
	config     *Config
	router     *gin.Engine
	http       *http.Server
	labManager *LabManager
}

func NewServer(config *Config) *Server {
	router := gin.Default()

	// Configurar CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	labManager, err := NewLabManager()
	if err != nil {
		log.Fatalf("Erro ao criar gerenciador de laboratórios: %v", err)
	}

	server := &Server{
		config:     config,
		router:     router,
		labManager: labManager,
		http: &http.Server{
			Addr:    fmt.Sprintf(":%d", config.Port),
			Handler: router,
		},
	}

	// Configurar rotas
	setupRoutes(router, server)
	setupWebSocketRoutes(router, server)

	return server
}

func (s *Server) Start() error {
	return s.http.ListenAndServe()
}

func (s *Server) Shutdown() error {
	return s.http.Shutdown(context.Background())
}

func setupRoutes(router *gin.Engine, server *Server) {
	// Rota de verificação de saúde
	router.GET("/api/v1/health", healthCheck)

	// Rotas autenticadas (usar middleware de autenticação quando estiver implementado)
	api := router.Group("/api/v1")
	{
		// Auth routes
		api.POST("/login", login)
		api.POST("/register", register)

		// Lab Routes
		api.GET("/labs", listLabs)
		api.GET("/labs/:id", getLab)
		api.POST("/labs", func(c *gin.Context) {
			createLab(c, server)
		})
		api.GET("/labs/current", func(c *gin.Context) {
			getCurrentLab(c, server)
		})
		api.DELETE("/labs/current", func(c *gin.Context) {
			server.DeleteCurrentLab(c)
		})

		// Template Routes
		api.GET("/templates", func(c *gin.Context) {
			templates := server.labManager.GetAvailableTemplates()
			c.JSON(200, gin.H{
				"templates": templates,
			})
		})
		api.GET("/templates/:id", func(c *gin.Context) {
			templateId := c.Param("id")
			template := server.labManager.GetTemplate(templateId)
			if template == nil {
				c.JSON(404, gin.H{"error": "Template não encontrado"})
				return
			}
			c.JSON(200, template)
		})

		// Agrupar rotas que usam namespace/pod para evitar conflito
		podApi := api.Group("/pods/:namespace/:pod")
		{
			// Status do pod
			podApi.GET("/status", func(c *gin.Context) {
				getPodStatus(c, server)
			})

			// Validação de tarefas e laboratório
			podApi.POST("/validate", func(c *gin.Context) {
				server.validateLabTask(c)
			})
			podApi.POST("/validate-lab", func(c *gin.Context) {
				server.validateLabCompletion(c)
			})
		}
	}
}

func setupWebSocketRoutes(router *gin.Engine, server *Server) {
	// Grupo de rotas de WebSocket
	ws := router.Group("/ws")
	{
		ws.GET("", func(c *gin.Context) {
			// Upgrade para websocket
			upgrader := websocket.Upgrader{
				CheckOrigin: func(r *http.Request) bool {
					return true // Aceita todas as origens em desenvolvimento
				},
				ReadBufferSize:  1024,
				WriteBufferSize: 1024,
			}

			conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
			if err != nil {
				log.Printf("Erro no upgrade do WebSocket: %v", err)
				return
			}
			defer conn.Close()

			// Enviar uma mensagem de status e configurar parâmetros da conexão
			if err := conn.WriteMessage(websocket.TextMessage, []byte("WebSocket server is running")); err != nil {
				log.Printf("Erro ao enviar mensagem inicial: %v", err)
				return
			}

			// Configurar ping/pong para manter a conexão ativa
			conn.SetPingHandler(func(message string) error {
				err := conn.WriteControl(websocket.PongMessage, []byte(message), time.Now().Add(time.Second*5))
				if err != nil {
					log.Printf("Erro ao enviar pong: %v", err)
				}
				return nil
			})

			// Configurar mensagens de fechamento corretas
			conn.SetCloseHandler(func(code int, text string) error {
				log.Printf("Conexão fechada pelo cliente: %d, %s", code, text)
				message := websocket.FormatCloseMessage(code, "")
				conn.WriteControl(websocket.CloseMessage, message, time.Now().Add(time.Second))
				return nil
			})

			// Loop para manter a conexão ativa
			for {
				messageType, message, err := conn.ReadMessage()
				if err != nil {
					// Verificar se é erro comum de desconexão
					if websocket.IsUnexpectedCloseError(err,
						websocket.CloseGoingAway,
						websocket.CloseNormalClosure,
						websocket.CloseNoStatusReceived) {
						log.Printf("Erro de leitura WebSocket: %v", err)
					}
					break
				}

				// Echo das mensagens recebidas
				if err := conn.WriteMessage(messageType, message); err != nil {
					// Apenas logar erros não relacionados a fechamento
					if !websocket.IsCloseError(err,
						websocket.CloseNormalClosure,
						websocket.CloseGoingAway) {
						log.Printf("Erro ao responder mensagem: %v", err)
					}
					break
				}
			}
		})
		ws.GET("/terminal/:namespace/:pod", func(c *gin.Context) {
			server.handleWebSocketTerminal(c)
		})
	}
}

// Handlers
func healthCheck(c *gin.Context) {
	c.JSON(200, gin.H{
		"status": "healthy",
	})
}

func login(c *gin.Context) {
	// TODO: Implementar login
	c.JSON(501, gin.H{"message": "Not implemented"})
}

func register(c *gin.Context) {
	// TODO: Implementar registro
	c.JSON(501, gin.H{"message": "Not implemented"})
}

func listLabs(c *gin.Context) {
	// TODO: Implementar listagem de labs
	c.JSON(501, gin.H{"message": "Not implemented"})
}

func getLab(c *gin.Context) {
	// TODO: Implementar obtenção de lab específico
	c.JSON(501, gin.H{"message": "Not implemented"})
}

func createLab(c *gin.Context, server *Server) {
	userId := c.GetString("userId")
	if userId == "" {
		userId = "test-user" // Temporário para teste
	}

	// Obter o templateId do corpo da requisição
	var req struct {
		TemplateId string `json:"templateId"`
	}
	if err := c.BindJSON(&req); err != nil {
		log.Printf("Erro ao ler solicitação: %v", err)
		// Se não conseguir ler o templateId, continua com template padrão
		req.TemplateId = ""
	}

	log.Printf("Iniciando criação de laboratório para usuário: %s com template: %s", userId, req.TemplateId)
	err := server.labManager.CreateLabEnvironment(userId, req.TemplateId)
	if err != nil {
		log.Printf("Erro ao criar laboratório: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	log.Printf("Laboratório criado com sucesso para usuário: %s", userId)
	c.JSON(200, gin.H{
		"message":    "Laboratório criado com sucesso",
		"labId":      fmt.Sprintf("lab-%s", userId),
		"templateId": req.TemplateId,
	})
}

func getCurrentLab(c *gin.Context, server *Server) {
	// Usar um usuário de teste fixo para desenvolvimento
	userID := "test-user"

	// Construir namespace a partir do userID
	namespace := fmt.Sprintf("lab-%s", userID)

	log.Printf("[API] Buscando laboratório atual para o usuário %s", userID)

	// Listar todos os pods do usuário com os seletores corretos
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podList, err := server.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=girus-lab",
	})

	if err != nil {
		log.Printf("[API] Erro ao listar pods do usuário %s: %v", userID, err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Não foi possível encontrar laboratórios para este usuário"})
		return
	}

	log.Printf("[API] Encontrados %d pods para o usuário %s", len(podList.Items), userID)

	if len(podList.Items) == 0 {
		log.Printf("[API] Nenhum pod encontrado para o usuário %s", userID)
		c.JSON(http.StatusNotFound, gin.H{"error": "Nenhum laboratório ativo encontrado"})
		return
	}

	// Se houver mais de um pod, ordenar pela data de criação e pegar o mais recente
	var currentPod *corev1.Pod
	if len(podList.Items) > 1 {
		// Ordenar pods por data de criação (mais recente primeiro)
		sort.Slice(podList.Items, func(i, j int) bool {
			// Pods com CreationTimestamp mais recente vêm primeiro
			return podList.Items[i].CreationTimestamp.After(podList.Items[j].CreationTimestamp.Time)
		})
		currentPod = &podList.Items[0]
		log.Printf("[API] Múltiplos pods encontrados (%d), usando o mais recente: %s", len(podList.Items), currentPod.Name)
		
		// Opcionalmente, limpar os pods antigos em segundo plano
		go func() {
			for i := 1; i < len(podList.Items); i++ {
				oldPod := podList.Items[i]
				log.Printf("[API] Limpando pod antigo em segundo plano: %s", oldPod.Name)
				_ = server.labManager.DeletePod(namespace, oldPod.Name)
			}
		}()
	} else {
		currentPod = &podList.Items[0]
		log.Printf("[API] Um único pod encontrado: %s", currentPod.Name)
	}

	// Obter o templateId das etiquetas do pod
	templateId := currentPod.Labels["template"]
	log.Printf("[API] Laboratório encontrado: %s (template: %s)", currentPod.Name, templateId)

	// Verificar status detalhado do pod
	podStatus := string(currentPod.Status.Phase)
	log.Printf("[API] Status do pod: %s", podStatus)
	
	// Verificar se todos os contêineres estão prontos
	allContainersReady := true
	containersStatus := []gin.H{}
	
	// Adicionar informações detalhadas de cada contêiner
	for _, containerStatus := range currentPod.Status.ContainerStatuses {
		allContainersReady = allContainersReady && containerStatus.Ready
		containersStatus = append(containersStatus, gin.H{
			"name":  containerStatus.Name,
			"ready": containerStatus.Ready,
		})
	}

	// Obter o template para pegar a URL do vídeo
	template := server.labManager.GetTemplate(templateId)
	var youtubeVideo string
	if template != nil {
		youtubeVideo = template.YoutubeVideo
	}

	// Obter informações detalhadas do laboratório com informações sobre timer
	labInfo, found := server.labManager.GetLabByUserID(userID)
	
	// Resposta básica caso não seja possível obter as informações completas
	responseData := gin.H{
		"namespace":          namespace,
		"podName":           currentPod.Name,
		"templateId":        templateId,
		"status":            podStatus,
		"allContainersReady": allContainersReady,
		"containersStatus":   containersStatus,
		"creationTimestamp": currentPod.CreationTimestamp.Format(time.RFC3339),
		"youtubeVideo":      youtubeVideo,
	}
	
	// Adicionar tarefas do template à resposta se o template foi encontrado
	if template != nil {
		log.Printf("[API] Incluindo %d tarefas com dicas na resposta", len(template.Tasks))
		responseData["tasks"] = template.Tasks
	}
	
	// Se temos informações detalhadas do laboratório, adicionar dados do timer
	if found {
		responseData["timerEnabled"] = labInfo.TimerEnabled
		responseData["startTime"] = labInfo.StartTime
		responseData["expirationTime"] = labInfo.ExpirationTime
		responseData["remainingTime"] = labInfo.RemainingTime
	}

	// Retornar informações do laboratório
	c.JSON(http.StatusOK, responseData)
}

// NOTE: A função contextWithTimeout está definida em lab_manager.go

// Função handlePodStatus com contexto atualizado
func (s *Server) handleGetPodStatus(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	// Listar todos os pods do usuário
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podList, err := s.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=girus-lab,user=%s", namespace),
	})

	if err != nil {
		log.Printf("[API] Erro ao listar pods: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erro ao verificar status dos pods"})
		return
	}

	// Se não há pods, verificar por nome específico
	if len(podList.Items) == 0 && podName != "" {
		ctx, cancel = contextWithTimeout()
		defer cancel()
		pod, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
		if err != nil {
			log.Printf("[API] Pod %s não encontrado: %v", podName, err)
			c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
			return
		}
		podList.Items = append(podList.Items, *pod)
	}

	// Verificar se há pods que correspondem ao critério
	if len(podList.Items) == 0 {
		log.Printf("[API] Nenhum pod encontrado para namespace %s", namespace)
		c.JSON(http.StatusNotFound, gin.H{"error": "Nenhum pod encontrado"})
		return
	}

	// Verificar status de cada pod
	pods := []map[string]interface{}{}
	for _, pod := range podList.Items {
		podInfo := map[string]interface{}{
			"name":      pod.Name,
			"namespace": pod.Namespace,
			"phase":     pod.Status.Phase,
		}

		// Verificar se todos os contêineres estão prontos
		allContainersReady := true
		containersInfo := []map[string]interface{}{}
		for _, container := range pod.Status.ContainerStatuses {
			containerInfo := map[string]interface{}{
				"name":    container.Name,
				"ready":   container.Ready,
				"restartCount": container.RestartCount,
			}
			if !container.Ready {
				allContainersReady = false
			}
			containersInfo = append(containersInfo, containerInfo)
		}
		podInfo["allContainersReady"] = allContainersReady
		podInfo["containers"] = containersInfo

		pods = append(pods, podInfo)
	}

	log.Printf("[API] Todos os contêineres prontos: %v", pods[0]["allContainersReady"])
	c.JSON(http.StatusOK, gin.H{
		"pods": pods,
	})
}

// WebSocketTerminal implementa as interfaces necessárias para funcionar com SPDY
type WebSocketTerminal struct {
	conn         *websocket.Conn
	terminalSize chan remotecommand.TerminalSize
}

// NewWebSocketTerminal cria um novo terminal baseado em WebSocket
func NewWebSocketTerminal(conn *websocket.Conn) *WebSocketTerminal {
	return &WebSocketTerminal{
		conn:         conn,
		terminalSize: make(chan remotecommand.TerminalSize, 1),
	}
}

// Read implementa io.Reader para WebSocketTerminal
func (t *WebSocketTerminal) Read(p []byte) (n int, err error) {
	// Ler mensagem do WebSocket
	_, message, err := t.conn.ReadMessage()
	if err != nil {
		return 0, err
	}

	// Verificar se é uma mensagem de resize
	if len(message) > 8 && string(message[0:8]) == `{"type":` {
		var resizeMessage struct {
			Type string `json:"type"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}

		if err := json.Unmarshal(message, &resizeMessage); err == nil {
			if resizeMessage.Type == "resize" {
				t.terminalSize <- remotecommand.TerminalSize{
					Width:  resizeMessage.Cols,
					Height: resizeMessage.Rows,
				}
				return 0, nil
			}
		}
	}

	copy(p, message)
	return len(message), nil
}

// Write implementa io.Writer para WebSocketTerminal
func (t *WebSocketTerminal) Write(p []byte) (n int, err error) {
	err = t.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		return 0, err
	}
	return len(p), nil
}

// Next implementa TerminalSizeQueue para WebSocketTerminal
func (t *WebSocketTerminal) Next() *remotecommand.TerminalSize {
	size := <-t.terminalSize
	return &size
}

// Função handleConnectTerminal com contexto atualizado
func (s *Server) handleConnectTerminal(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")
	container := c.Param("container")

	if container == "" {
		container = "lab" // Nome padrão do contêiner
	}

	// Obter objeto do pod
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podObj, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("[API] Erro ao obter pod: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao conectar ao terminal: %v", err)})
		return
	}

	// Verificar se o pod está pronto
	podReady := false
	allContainersReady := true

	// Verificar se todos os contêineres estão prontos
	if podObj.Status.Phase == corev1.PodRunning {
		podReady = true
		for _, containerStatus := range podObj.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allContainersReady = false
				break
			}
		}
	} else {
		podReady = false
	}

	if !podReady {
		log.Printf("Pod não está em execução, estado atual: %v", podObj.Status.Phase)
		c.JSON(http.StatusBadRequest, gin.H{"error": "O pod não está em execução ainda"})
		return
	}

	if !allContainersReady {
		log.Printf("Nem todos os contêineres do pod estão prontos")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nem todos os contêineres do pod estão prontos"})
		return
	}

	log.Printf("Pod encontrado e está rodando com todos os contêineres prontos. Iniciando upgrade para WebSocket")

	// Agora verificar se o contêiner solicitado existe
	containerExists := false
	for _, podContainer := range podObj.Spec.Containers {
		if podContainer.Name == container {
			containerExists = true
			break
		}
	}

	if !containerExists {
		containerNames := []string{}
		for _, podContainer := range podObj.Spec.Containers {
			containerNames = append(containerNames, podContainer.Name)
		}
		log.Printf("Contêiner '%s' não encontrado no pod. Contêineres disponíveis: %v", container, containerNames)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Contêiner '%s' não encontrado no pod", container)})
		return
	}

	// Upgrade para WebSocket
	upgrader := websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			return true // Permitir conexões de qualquer origem
		},
	}

	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Erro ao fazer upgrade da conexão para WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// Configurar requisição de terminal
	req := s.labManager.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Container: container,
		Command:   []string{"/bin/sh"},
		Stdin:     true,
		Stdout:    true,
		Stderr:    true,
		TTY:       true,
	}, scheme.ParameterCodec)

	// Estabelecer conexão exec
	executor, err := remotecommand.NewSPDYExecutor(s.labManager.config, "POST", req.URL())
	if err != nil {
		log.Printf("Erro ao criar executor SPDY: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Erro ao criar executor SPDY: %v", err)))
		return
	}

	// Criar os pipes para comunicação bidirecional
	wsTerminal := NewWebSocketTerminal(conn)

	// Executar o comando no pod
	err = executor.Stream(remotecommand.StreamOptions{
		Stdin:             wsTerminal,
		Stdout:            wsTerminal,
		Stderr:            wsTerminal,
		Tty:               true,
		TerminalSizeQueue: wsTerminal,
	})

	if err != nil {
		log.Printf("Erro durante a execução do stream: %v", err)
		conn.WriteMessage(websocket.TextMessage, []byte(fmt.Sprintf("Erro durante a execução do stream: %v", err)))
	}
}

// Função CleanupUserLabs com contexto atualizado
func (s *Server) CleanupUserLabs(namespace string) error {
	// Verificar se existem pods para limpar
	ctx, cancel := contextWithTimeout()
	defer cancel()
	pod, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, "lab-pod", metav1.GetOptions{})
	if err == nil {
		// Se o pod existe, removê-lo
		log.Printf("Removendo pod %s/%s", namespace, pod.Name)
		ctx, cancel = contextWithTimeout()
		defer cancel()
		err = s.labManager.clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Int64(0), // Remoção imediata
		})
		if err != nil {
			log.Printf("Erro ao remover pod %s/%s: %v", namespace, pod.Name, err)
		}
	}

	// Listar todos os pods do usuário
	ctx, cancel = contextWithTimeout()
	defer cancel()
	podList, err := s.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err == nil && len(podList.Items) > 0 {
		// Excluir todos os pods do usuário
		errCount := 0
		for _, pod := range podList.Items {
			log.Printf("[API] Excluindo pod %s/%s", namespace, pod.Name)
			deleteCtx, deleteCancel := contextWithTimeout()
			err = s.labManager.clientset.CoreV1().Pods(namespace).Delete(deleteCtx, pod.Name, metav1.DeleteOptions{})
			deleteCancel()
			if err != nil {
				log.Printf("[API] Erro ao excluir pod %s/%s: %v", namespace, pod.Name, err)
				errCount++
			}
		}
	}

	return nil
}

func (s *Server) handleWebSocketTerminal(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	log.Printf("Tentando conectar ao terminal do pod %s no namespace %s", podName, namespace)

	// Verificar se o pod existe e está pronto
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podObj, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("Erro ao encontrar pod: %v", err)
		c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
		return
	}

	// Verificações mais rigorosas do estado do pod
	if podObj.Status.Phase != corev1.PodRunning {
		log.Printf("Pod não está em execução. Status atual: %s", podObj.Status.Phase)
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Pod não está em execução (status: %s)", podObj.Status.Phase)})
		return
	}

	// Verificar se todos os contêineres estão prontos
	containersReady := true
	for _, containerStatus := range podObj.Status.ContainerStatuses {
		if !containerStatus.Ready {
			containersReady = false
			log.Printf("Contêiner %s no pod %s não está pronto", containerStatus.Name, podName)
			break
		}
	}

	if !containersReady {
		log.Printf("Nem todos os contêineres do pod estão prontos")
		c.JSON(http.StatusBadRequest, gin.H{"error": "Nem todos os contêineres do pod estão prontos"})
		return
	}

	log.Printf("Pod encontrado e está rodando com todos os contêineres prontos. Iniciando upgrade para WebSocket")

	// Upgrade para websocket
	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			origin := r.Header.Get("Origin")
			log.Printf("Requisição WebSocket - Origem: %s, URL: %s, Headers: %v",
				origin, r.URL.String(), r.Header)
			return true
		},
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}

	log.Printf("Tentando fazer upgrade da conexão para WebSocket")
	conn, err := upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Erro no upgrade do WebSocket: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	// Note: Removemos o defer conn.Close() para que o controle de fechamento da conexão
	// seja explícito e gerenciado pelo tratamento de erros e pela lógica de streaming

	log.Printf("WebSocket conectado com sucesso. Configurando exec no pod")

	// Configurar controle de fechamento adequado
	conn.SetCloseHandler(func(code int, text string) error {
		log.Printf("Cliente fechou conexão WebSocket: código=%d, razão=%s", code, text)
		return nil // Não fechamos a conexão aqui, pois isso pode impedir a leitura de mensagens pendentes
	})

	// Configurar exec para o pod
	req := s.labManager.clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec")

	req.VersionedParams(&corev1.PodExecOptions{
		Command: []string{"/bin/bash"},
		Stdin:   true,
		Stdout:  true,
		Stderr:  true,
		TTY:     true,
	}, scheme.ParameterCodec)

	config, err := s.labManager.getKubeConfig()
	if err != nil {
		log.Printf("Erro ao obter configuração Kubernetes: %v", err)
		sendWebSocketError(conn, fmt.Sprintf("Erro na configuração: %v", err))
		conn.Close() // Aqui podemos fechar, pois houve erro crítico
		return
	}

	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.Printf("Erro ao criar executor SPDY: %v", err)
		sendWebSocketError(conn, fmt.Sprintf("Erro ao criar executor: %v", err))
		conn.Close() // Aqui podemos fechar, pois houve erro crítico
		return
	}

	// Criar adaptadores para websocket
	wsHandler := &terminalHandler{
		conn:         conn,
		terminalSize: make(chan remotecommand.TerminalSize, 1),
	}

	// Configurar terminal size inicial
	size := remotecommand.TerminalSize{
		Width:  80,
		Height: 24,
	}
	wsHandler.terminalSize <- size

	// Executar o streaming em uma goroutine
	streamDone := make(chan error, 1)
	go func() {
		err := exec.Stream(remotecommand.StreamOptions{
			Stdin:             wsHandler,
			Stdout:            wsHandler,
			Stderr:            wsHandler,
			TerminalSizeQueue: wsHandler,
			Tty:               true,
		})
		streamDone <- err
	}()

	// Aguardar pelo término do streaming
	err = <-streamDone
	if err != nil {
		log.Printf("Erro no stream do terminal: %v", err)
		sendWebSocketError(conn, fmt.Sprintf("Erro no stream: %v", err))
	} else {
		log.Printf("Stream do terminal encerrado normalmente")
		sendWebSocketMessage(conn, "\r\nSessão do terminal encerrada. Digite 'exit' ou 'logout' para iniciar nova sessão.")
	}

	// Importante: NÃO fechamos a conexão WebSocket aqui
	// Vamos mantê-la aberta e deixar que o cliente decida quando fechar
	
	// Em vez de fechar a conexão, vamos iniciar um loop de ping para mantê-la viva
	log.Printf("Mantendo conexão WebSocket ativa após término do stream")
	
	// Iniciar loop de ping para manter conexão viva
	pingTicker := time.NewTicker(15 * time.Second)
	defer pingTicker.Stop()
	
	// Canal para detectar fechamento
	closeChan := make(chan struct{})
	
	// Goroutine para monitorar mensagens do cliente
	go func() {
		for {
			// Ler mensagens apenas para detectar fechamento
			if _, _, err := conn.ReadMessage(); err != nil {
				log.Printf("Conexão fechada pelo cliente ou erro: %v", err)
				close(closeChan)
				return
			}
		}
	}()
	
	// Loop principal para manter conexão
	for {
		select {
		case <-pingTicker.C:
			// Enviar ping
			if err := conn.WriteMessage(websocket.PingMessage, []byte{}); err != nil {
				log.Printf("Erro ao enviar ping: %v", err)
				conn.Close()
				return
			}
		case <-closeChan:
			// Cliente fechou a conexão
			log.Printf("Saindo do loop de manutenção de conexão")
			return
		}
	}
}

// Função auxiliar para enviar mensagens de erro via WebSocket
func sendWebSocketError(conn *websocket.Conn, message string) {
	formattedMessage := fmt.Sprintf("\r\n\x1b[1;31m%s\x1b[0m\r\n", message)
	conn.WriteMessage(websocket.TextMessage, []byte(formattedMessage))
}

// Função auxiliar para enviar mensagens informativas via WebSocket
func sendWebSocketMessage(conn *websocket.Conn, message string) {
	conn.WriteMessage(websocket.TextMessage, []byte(message))
}

// terminalHandler implementa as interfaces io.Reader e io.Writer
type terminalHandler struct {
	conn         *websocket.Conn
	terminalSize chan remotecommand.TerminalSize
}

func (t *terminalHandler) Read(p []byte) (n int, err error) {
	// Ler mensagem do WebSocket
	_, message, err := t.conn.ReadMessage()
	if err != nil {
		log.Printf("Erro ao ler mensagem do WebSocket: %v", err)
		return 0, err
	}
	log.Printf("Recebida mensagem do WebSocket: %d bytes", len(message))

	// Verificar se é uma mensagem de tipo "resize"
	if len(message) > 8 && string(message[0:8]) == `{"type":` {
		var resizeMessage struct {
			Type string `json:"type"`
			Cols uint16 `json:"cols"`
			Rows uint16 `json:"rows"`
		}

		if err := json.Unmarshal(message, &resizeMessage); err == nil {
			if resizeMessage.Type == "resize" {
				log.Printf("Recebida mensagem de resize: cols=%d, rows=%d",
					resizeMessage.Cols, resizeMessage.Rows)

				// Atualizar o tamanho do terminal
				t.terminalSize <- remotecommand.TerminalSize{
					Width:  resizeMessage.Cols,
					Height: resizeMessage.Rows,
				}

				// Não enviar esta mensagem para o terminal
				return 0, nil
			}
		}
	}

	// Copiar a mensagem para o buffer p
	copy(p, message)
	return len(message), nil
}

func (t *terminalHandler) Write(p []byte) (n int, err error) {
	// Enviar mensagem para o WebSocket
	log.Printf("Enviando mensagem para o WebSocket: %d bytes", len(p))
	err = t.conn.WriteMessage(websocket.TextMessage, p)
	if err != nil {
		log.Printf("Erro ao enviar mensagem para o WebSocket: %v", err)
		return 0, err
	}
	return len(p), nil
}

func (t *terminalHandler) Next() *remotecommand.TerminalSize {
	size := <-t.terminalSize
	return &size
}

func (s *Server) Run() error {
	router := gin.Default()

	// Configurar CORS
	router.Use(cors.New(cors.Config{
		AllowOrigins:     []string{"*"},
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
		AllowHeaders:     []string{"Origin", "Content-Type", "Accept", "Authorization"},
		ExposeHeaders:    []string{"Content-Length"},
		AllowCredentials: true,
		MaxAge:           12 * time.Hour,
	}))

	// Configurar rotas
	setupRoutes(router, s)
	setupWebSocketRoutes(router, s)
	
	// Iniciar monitoramento de laboratórios com timer
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel() // Garantir que o monitoramento será encerrado quando o servidor for encerrado
	s.labManager.StartTimerMonitor(ctx)
	log.Printf("Monitoramento de laboratórios com timer iniciado")

	log.Printf("Servidor iniciado na porta %s", s.config.Port)
	return router.Run(":" + fmt.Sprintf("%d", s.config.Port))
}

func (server *Server) StartLab(c *gin.Context) {
	userId := c.Param("userId")
	if userId == "" {
		c.JSON(400, gin.H{"error": "ID de usuário não fornecido"})
		return
	}

	// Buscar o templateId da query ou do body
	templateId := c.Query("templateId")

	// Se não foi fornecido na query, buscar no body
	if templateId == "" {
		var req struct {
			TemplateId string `json:"templateId"`
		}
		if c.BindJSON(&req) == nil && req.TemplateId != "" {
			templateId = req.TemplateId
		}
	}

	log.Printf("Iniciando criação de laboratório para usuário: %s", userId)
	err := server.labManager.CreateLabEnvironment(userId, templateId)
	if err != nil {
		log.Printf("Erro ao criar laboratório: %v", err)
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}

	c.JSON(200, gin.H{"message": "Laboratório criado com sucesso", "userId": userId, "templateId": templateId})
}

// validateLabTask verifica se uma tarefa foi concluída
func (s *Server) validateLabTask(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	// Parse da requisição
	var req struct {
		TemplateId string `json:"templateId"`
		TaskIndex  int    `json:"taskIndex"`
	}

	if err := c.BindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("Erro na requisição: %v", err)})
		return
	}

	// Obter o pod
	ctx, cancel := contextWithTimeout()
	defer cancel()
	pod, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
		return
	}

	// Obter o template
	template := s.labManager.templates.GetTemplate(req.TemplateId)
	if template == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Template não encontrado"})
		return
	}

	// Verificar se o índice da tarefa é válido
	if req.TaskIndex < 0 || req.TaskIndex >= len(template.Tasks) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Índice de tarefa inválido"})
		return
	}

	// Validar a tarefa
	task := template.Tasks[req.TaskIndex]
	success, message := s.labManager.templates.ValidateTaskCompletion(pod, task)

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": message,
	})
}

func (s *Server) validateLabCompletion(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	log.Printf("Validando laboratório completo: namespace=%s, pod=%s", namespace, podName)

	var req struct {
		TemplateId string `json:"templateId"`
	}

	if err := c.BindJSON(&req); err != nil {
		log.Printf("Erro ao ler solicitação de validação: %v", err)
		c.JSON(http.StatusBadRequest, gin.H{
			"success": false,
			"error":   fmt.Sprintf("Erro ao ler solicitação: %v", err),
		})
		return
	}

	log.Printf("Template a ser validado: %s", req.TemplateId)

	// Obter pod
	pod, err := s.labManager.GetPod(namespace, podName)
	if err != nil {
		log.Printf("Erro ao obter pod: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"success": false,
			"error":   "Pod não encontrado",
		})
		return
	}

	// Validar o laboratório completo
	success, message := s.labManager.ValidateLabCompletion(pod, req.TemplateId)
	
	log.Printf("Resultado da validação do laboratório: success=%v, message=%s", success, message)

	c.JSON(http.StatusOK, gin.H{
		"success": success,
		"message": message,
	})
}

// Função para obter o status do pod
func getPodStatus(c *gin.Context, server *Server) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")

	log.Printf("Verificando status do pod %s no namespace %s", podName, namespace)

	// Buscar o pod no Kubernetes
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podObj, err := server.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("Erro ao buscar pod: %v", err)
		c.JSON(http.StatusNotFound, gin.H{
			"status": "Unknown",
			"error":  "Pod não encontrado",
		})
		return
	}

	// Retornar o status atual do pod
	log.Printf("Status do pod %s: %s", podName, podObj.Status.Phase)
	c.JSON(http.StatusOK, gin.H{
		"status":              string(podObj.Status.Phase),
		"startTime":           podObj.Status.StartTime,
		"containerStatuses":   podObj.Status.ContainerStatuses,
		"initContainerStatus": podObj.Status.InitContainerStatuses,
	})
}

func deleteLab(c *gin.Context, server *Server) {
	// Obter o ID do usuário do contexto de autenticação ou do cookie
	userID := getUserIDFromContext(c)
	
	log.Printf("[API] Iniciando processo de exclusão do laboratório para usuário: %s", userID)
	
	// Se não encontrado, usar um valor de teste para desenvolvimento
	if userID == "" {
		userID = "test-user"
		log.Printf("[API] Usando userID de desenvolvimento: %s", userID)
	}

	// Verificar se o userID é válido
	if userID == "" {
		log.Printf("[API] Erro: userID está vazio")
		c.JSON(http.StatusBadRequest, gin.H{"error": "ID de usuário inválido"})
		return
	}

	// Construir namespace a partir do userID
	namespace := fmt.Sprintf("lab-%s", userID)

	log.Printf("[API] Solicitação para excluir laboratório do usuário %s no namespace %s", userID, namespace)

	defer func() {
		// Capturar e registrar qualquer pânico que ocorra durante a operação
		if r := recover(); r != nil {
			log.Printf("[API] Pânico recuperado na exclusão do laboratório: %v", r)
			c.JSON(http.StatusOK, gin.H{"message": "Laboratório encerrado com sucesso", "warning": "Ocorreu um erro interno"})
		}
	}()

	// Verificar se o namespace existe
	var namespaceExists bool
	ctx, cancel := contextWithTimeout()
	defer cancel()
	
	_, err := server.labManager.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		log.Printf("[API] Namespace %s não encontrado: %v", namespace, err)
		namespaceExists = false
	} else {
		namespaceExists = true
	}

	// Se namespace não existe, ainda podemos retornar sucesso para não bloquear o usuário
	if !namespaceExists {
		log.Printf("[API] Namespace %s não existe, retornando como sucesso", namespace)
		c.JSON(http.StatusOK, gin.H{"message": "Nenhum laboratório ativo encontrado"})
		return
	}

	// Primeiro excluir todos os pods no namespace
	log.Printf("[API] Excluindo todos os pods no namespace %s", namespace)
	
	// Listar todos os pods no namespace
	var podList *corev1.PodList
	var podListErr error
	
	// Tentar listar pods com retry
	for attempt := 1; attempt <= 3; attempt++ {
		ctx, cancel = contextWithTimeout()
		podList, podListErr = server.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
		cancel()
		
		if podListErr == nil {
			break
		}
		
		log.Printf("[API] Tentativa %d: Erro ao listar pods no namespace %s: %v", attempt, namespace, podListErr)
		time.Sleep(time.Second * time.Duration(attempt))
	}
	
	if podListErr != nil {
		log.Printf("[API] Erro persistente ao listar pods no namespace %s: %v", namespace, podListErr)
		// Mesmo com erro, retornamos sucesso para não bloquear o usuário
		c.JSON(http.StatusOK, gin.H{"message": "Laboratório encerrado com sucesso", "warning": "Não foi possível listar os recursos"})
		return
	}
	
	// Se não há pods, retorna sucesso imediatamente
	if len(podList.Items) == 0 {
		log.Printf("[API] Nenhum pod encontrado no namespace %s para excluir", namespace)
		c.JSON(http.StatusOK, gin.H{"message": "Laboratório encerrado com sucesso"})
		return
	}
	
	log.Printf("[API] Encontrados %d pods para excluir no namespace %s", len(podList.Items), namespace)
	
	// Excluir cada pod encontrado
	successCount := 0
	errorCount := 0
	
	// Usar um canal para controlar o timeout das operações
	doneChan := make(chan struct{})
	go func() {
		for _, pod := range podList.Items {
			if pod.Name != "" {
				log.Printf("[API] Excluindo pod %s/%s", namespace, pod.Name)
				deleteCtx, deleteCancel := contextWithTimeout()
				deleteErr := server.labManager.clientset.CoreV1().Pods(namespace).Delete(deleteCtx, pod.Name, metav1.DeleteOptions{
					GracePeriodSeconds: pointer.Int64(0), // Forçar exclusão imediata
				})
				deleteCancel()
				
				if deleteErr != nil {
					log.Printf("[API] Erro ao excluir pod %s/%s: %v", namespace, pod.Name, deleteErr)
					errorCount++
				} else {
					successCount++
				}
			}
		}
		close(doneChan)
	}()
	
	// Aguardar a conclusão da exclusão com timeout global
	select {
	case <-doneChan:
		log.Printf("[API] Exclusão de pods concluída. Excluídos com sucesso: %d, Erros: %d", successCount, errorCount)
	case <-time.After(20 * time.Second):
		log.Printf("[API] Timeout ao excluir pods. Excluídos até agora: %d, Erros: %d", successCount, errorCount)
	}
	
	// Não excluir o namespace, apenas retornar sucesso após excluir os pods
	c.JSON(http.StatusOK, gin.H{"message": "Laboratório encerrado com sucesso"})
}

// handleUserLabs retorna os laboratórios do usuário
func (server *Server) handleUserLabs(c *gin.Context) {
	namespace := c.Param("namespace")
	
	// Listar todos os pods do usuário
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podList, err := server.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("app=girus-lab,user=%s", namespace),
	})
	
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao listar pods: %v", err)})
		return
	}
	
	// Formatar resposta
	labs := []gin.H{}
	for _, pod := range podList.Items {
		templateName := pod.Labels["template"]
		
		lab := gin.H{
			"podName":     pod.Name,
			"namespace":   pod.Namespace,
			"templateId":  templateName,
			"status":      string(pod.Status.Phase),
			"createdAt":   pod.CreationTimestamp.Format(time.RFC3339),
		}
		
		labs = append(labs, lab)
	}
	
	c.JSON(http.StatusOK, gin.H{
		"labs": labs,
	})
}

// handleGetLabStatus obtém o status de um laboratório
func (s *Server) handleGetLabStatus(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")
	
	// Obter o pod
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podObj, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Lab não encontrado"})
		return
	}
	
	// Verificar status
	ready := podObj.Status.Phase == corev1.PodRunning
	
	c.JSON(http.StatusOK, gin.H{
		"status": string(podObj.Status.Phase),
		"ready": ready,
	})
}

// handleWebSocketStatus processa a conexão WebSocket para monitoramento de status
func (s *Server) handleWebSocketStatus(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")
	
	// Verificar se o pod existe
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := s.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Pod não encontrado"})
		return
	}
	
	// Resto da função... (implementação a ser adicionada quando necessário)
	c.JSON(http.StatusOK, gin.H{"status": "Monitoramento iniciado"})
}

// handleDeleteLab exclui um laboratório
func (server *Server) handleDeleteLab(c *gin.Context) {
	namespace := c.Param("namespace")
	podName := c.Param("pod")
	
	// Verificar se o pod existe
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := server.labManager.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "Lab não encontrado"})
		return
	}
	
	// Excluir o pod
	if err := server.labManager.DeletePod(namespace, podName); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao excluir lab: %v", err)})
		return
	}
	
	c.JSON(http.StatusOK, gin.H{"message": "Lab excluído com sucesso"})
}

// handleCleanupAllLabs limpa todos os recursos de laboratório de um usuário
func (server *Server) handleCleanupAllLabs(c *gin.Context) {
	namespace := c.Param("namespace")
	
	// Listar todos os pods do usuário
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podList, err := server.labManager.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Erro ao listar pods: %v", err)})
		return
	}
	
	// Excluir todos os pods do usuário
	deletedPods := 0
	for _, pod := range podList.Items {
		ctx, cancel := contextWithTimeout()
		defer cancel()
		err = server.labManager.clientset.CoreV1().Pods(namespace).Delete(ctx, pod.Name, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Int64(0),
		})
		if err != nil {
			log.Printf("Erro ao excluir pod %s: %v", pod.Name, err)
		} else {
			deletedPods++
		}
	}
	
	c.JSON(http.StatusOK, gin.H{
		"message": fmt.Sprintf("Limpeza concluída. %d pods removidos.", deletedPods),
	})
}

// getUserIDFromContext obtém o ID do usuário do contexto de autenticação ou cookies
func getUserIDFromContext(c *gin.Context) string {
	// Verificar cabeçalho de autenticação
	authHeader := c.GetHeader("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		// Exemplo simples: em produção, validar o token JWT
		token := strings.TrimPrefix(authHeader, "Bearer ")
		if token != "" {
			// Aqui seria feita validação do token
			return "user-from-token" // Placeholder
		}
	}
	
	// Verificar em cookies
	userID, err := c.Cookie("user_id")
	if err == nil && userID != "" {
		return userID
	}
	
	// Verificar em query params (apenas para desenvolvimento/testes)
	userID = c.Query("user_id")
	if userID != "" {
		return userID
	}
	
	return ""
}

// DeleteLab remove um laboratório ativo
func (server *Server) DeleteLab(c *gin.Context) {
	userId := c.Param("userId")
	forceDelete := c.Query("force") == "true"

	if userId == "" {
		// Se não há ID de usuário específico, deletar o laboratório atual
		labInfo, found := server.labManager.GetCurrentLab()
		if !found {
			c.JSON(404, gin.H{"error": "Laboratório não encontrado"})
			return
		}
		userId = labInfo.UserID
	}

	log.Printf("Solicitação para deletar laboratório do usuário: %s (force: %v)", userId, forceDelete)
	err := server.labManager.DeleteLabEnvironment(userId, forceDelete)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Erro ao deletar laboratório: %v", err)})
		return
	}

	c.JSON(200, gin.H{"message": "Laboratório deletado com sucesso"})
}

// DeleteCurrentLab exclui o laboratório atual (último acessado)
func (server *Server) DeleteCurrentLab(c *gin.Context) {
	// Verificar parâmetro force
	forceDelete := c.Query("force") == "true"
	
	labInfo, found := server.labManager.GetCurrentLab()
	if !found {
		c.JSON(404, gin.H{"error": "Nenhum laboratório encontrado"})
		return
	}
	
	log.Printf("Excluindo laboratório atual (userID: %s, force: %v)", labInfo.UserID, forceDelete)
	err := server.labManager.DeleteLabEnvironment(labInfo.UserID, forceDelete)
	if err != nil {
		c.JSON(500, gin.H{"error": fmt.Sprintf("Erro ao excluir laboratório: %v", err)})
		return
	}
	
	c.JSON(200, gin.H{"message": "Laboratório atual excluído com sucesso"})
}
