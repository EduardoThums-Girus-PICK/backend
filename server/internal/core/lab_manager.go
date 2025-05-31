package core

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"encoding/base64"
	"sync"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
	"k8s.io/utils/pointer"
)

var config *Config

func init() {
	var err error
	config, err = LoadConfig()
	if err != nil {
		log.Printf("Erro ao carregar configuração: %v", err)
		config = NewConfig() // Usar config padrão em caso de erro
	}
}

type LabManager struct {
	clientset *kubernetes.Clientset
	config    *rest.Config
	templates *TemplateManager
	mu        sync.RWMutex
	userLabMap map[string]string
}

// contextWithTimeout cria um contexto com timeout para operações Kubernetes
func contextWithTimeout() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

func NewLabManager() (*LabManager, error) {
	// Inicializar o gerador de números aleatórios
	rand.Seed(time.Now().UnixNano())
	
	// Configuração para acessar o cluster
	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback para desenvolvimento local
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Printf("Erro ao carregar kubeconfig: %v", err)
			return nil, fmt.Errorf("falha ao obter configuração do cluster: %v", err)
		}
		log.Printf("Usando kubeconfig: %s", kubeconfig)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Erro ao criar clientset: %v", err)
		return nil, fmt.Errorf("falha ao criar cliente Kubernetes: %v", err)
	}

	// Verificar conexão com o cluster
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err = clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, fmt.Errorf("erro ao conectar ao cluster Kubernetes: %v", err)
	}
	log.Printf("Conexão com o cluster estabelecida com sucesso")

	lm := &LabManager{
		clientset: clientset,
		config:    config,
		templates: NewTemplateManager(),
		userLabMap: make(map[string]string),
	}

	// Carregar templates de laboratório
	if err := lm.templates.LoadTemplates(clientset); err != nil {
		log.Printf("Aviso: Erro ao carregar templates: %v", err)
	}

	return lm, nil
}

func (lm *LabManager) CreateLabEnvironment(userId string, templateName string) error {
	// Obter o template do laboratório
	template := lm.templates.GetTemplate(templateName)
	if template == nil {
		return fmt.Errorf("template %s não encontrado", templateName)
	}

	// Gerar nome do namespace e pod
	namespace := fmt.Sprintf("lab-%s", userId)
	podName := generateUniquePodName("lab", userId)

	// Criar namespace se não existir
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := lm.clientset.CoreV1().Namespaces().Create(ctx,
		&v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: namespace,
				Labels: map[string]string{
					"createdBy": "girus",
					"userId":    userId,
				},
			},
		},
		metav1.CreateOptions{},
	)
	if err != nil && !strings.Contains(err.Error(), "already exists") {
		return fmt.Errorf("erro ao criar namespace: %v", err)
	}

	// Limpar recursos existentes se necessário
	// Se um pod com o mesmo nome já existe, vamos excluí-lo primeiro
	ctx, cancel = contextWithTimeout()
	defer cancel()
	_, err = lm.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err == nil {
		// Pod existe, vamos excluí-lo
		ctx, cancel = contextWithTimeout()
		defer cancel()
		err = lm.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{
			GracePeriodSeconds: pointer.Int64(0), // Forçar exclusão imediata
		})
		if err != nil {
			return fmt.Errorf("erro ao limpar pod existente: %v", err)
		}
		log.Printf("Pod existente %s/%s excluído para recriação", namespace, podName)

		// Aguardar a exclusão do pod antes de prosseguir
		time.Sleep(2 * time.Second)
	}

	// Verificar se ConfigMap de script já existe e excluir se necessário
	configMapName := "lab-files"
	ctx, cancel = contextWithTimeout()
	defer cancel()
	_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configMapName, metav1.GetOptions{})
	if err == nil {
		// ConfigMap existe, excluir
		ctx, cancel = contextWithTimeout()
		defer cancel()
		err = lm.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, configMapName, metav1.DeleteOptions{})
		if err != nil {
			return fmt.Errorf("erro ao excluir ConfigMap existente: %v", err)
		}
	}

	// Criar ConfigMap com arquivos do laboratório
	fileData := make(map[string]string)
	fileData["welcome.md"] = fmt.Sprintf("# Bem-vindo ao Laboratório %s\n\n%s\n\n## Tarefas\n\n%s",
		template.Title, template.Description, formatTasks(template.Tasks))

	ctx, cancel = contextWithTimeout()
	defer cancel()
	_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Create(ctx,
		&v1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name: configMapName,
			},
			Data: fileData,
		},
		metav1.CreateOptions{},
	)
	if err != nil {
		return fmt.Errorf("erro ao criar ConfigMap: %v", err)
	}

	// Verificar se é um lab de Kubernetes
	isKubernetesLab := strings.Contains(templateName, "kubernetes")

	// Verificar se é um lab de Docker
	isDockerLab := strings.Contains(strings.ToLower(templateName), "docker")

	// Verificar se é um lab de AWS/LocalStack
	isLocalStackLab := strings.Contains(strings.ToLower(templateName), "aws") || 
	                  strings.Contains(strings.ToLower(templateName), "localstack")

	// Para labs que não são de Kubernetes, criar o script de inicialização
	if !isKubernetesLab && !isDockerLab && !isLocalStackLab {
		// Verificar se ConfigMap de inicialização já existe e excluir se necessário
		k3sInitScriptName := "k3s-init-script"
		ctx, cancel = contextWithTimeout()
		defer cancel()
		_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, k3sInitScriptName, metav1.GetOptions{})
		if err == nil {
			// ConfigMap existe, excluir
			ctx, cancel = contextWithTimeout()
			defer cancel()
			err = lm.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, k3sInitScriptName, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("erro ao excluir ConfigMap de inicialização existente: %v", err)
			}
		}

		// Criar ConfigMap com script de inicialização do K3s
		ctx, cancel = contextWithTimeout()
		defer cancel()
		_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Create(ctx,
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: k3sInitScriptName,
				},
				Data: map[string]string{
					"init.sh": generateK3sInitScript(),
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("erro ao criar ConfigMap de inicialização: %v", err)
		}
	} else {
		log.Printf("Lab de Kubernetes, Docker ou AWS/LocalStack detectado, pulando criação do script de inicialização para %s", templateName)
	}

	// Para labs Docker, criar e montar um script de inicialização Docker
	if isDockerLab {
		// Verificar se ConfigMap de inicialização já existe e excluir se necessário
		dockerInitScriptName := "docker-init-script"
		ctx, cancel = contextWithTimeout()
		defer cancel()
		_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Get(ctx, dockerInitScriptName, metav1.GetOptions{})
		if err == nil {
			// ConfigMap existe, excluir
			ctx, cancel = contextWithTimeout()
			defer cancel()
			err = lm.clientset.CoreV1().ConfigMaps(namespace).Delete(ctx, dockerInitScriptName, metav1.DeleteOptions{})
			if err != nil {
				return fmt.Errorf("erro ao excluir ConfigMap de inicialização Docker existente: %v", err)
			}
		}

		// Criar ConfigMap com script de inicialização do Docker
		ctx, cancel = contextWithTimeout()
		defer cancel()
		_, err = lm.clientset.CoreV1().ConfigMaps(namespace).Create(ctx,
			&v1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name: dockerInitScriptName,
				},
				Data: map[string]string{
					"init-docker.sh": generateDockerInitScript(),
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil && !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("erro ao criar ConfigMap de inicialização Docker: %v", err)
		}
		log.Printf("Script de inicialização Docker criado para o laboratório %s", templateName)
	}

	// Criar o pod no Kubernetes
	log.Printf("Criando pod %s no namespace %s", podName, namespace)
	
	// Obter a imagem correta para o template
	var labImage string
	
	// Verificar se o template tem uma imagem personalizada definida
	if template != nil && template.Image != "" {
		labImage = template.Image
		log.Printf("Usando imagem personalizada do template: %s", labImage)


	} else {
		// Caso contrário, usar a lógica de seleção de imagem padrão
		labImage = GetImageForTemplate(templateName, "")
	}
	
	log.Printf("Usando imagem %s para o laboratório", labImage)
	
	// Verificar se é um lab de Kubernetes
	isKubernetesLab = strings.Contains(templateName, "kubernetes")
	
	// Verificar se é um lab de Docker
	isDockerLab = strings.Contains(strings.ToLower(templateName), "docker")

	// Verificar se é um lab de AWS/LocalStack
	isLocalStackLab = strings.Contains(strings.ToLower(templateName), "aws") || 
	                  strings.Contains(strings.ToLower(templateName), "localstack")

	// Definir comando com base no tipo de laboratório
	var command []string
	if isKubernetesLab {
		// Para Kubernetes, usar o entrypoint da imagem
		command = []string{"/bin/bash", "-c", "entrypoint.sh"}
	} else if isDockerLab {
		command = []string{"/bin/bash", "-c", "/scripts/init-docker.sh"}
	} else if isLocalStackLab {
		// Para LocalStack, usar o entrypoint da imagem
		command = []string{"/bin/bash", "-c", "/entrypoint.sh"}
	} else {
		command = []string{"/bin/bash", "-c", "/scripts/init.sh"}
	}
	
	// Definir volumes e volume mounts com base no tipo de laboratório
	var volumeMounts []v1.VolumeMount
	var volumes []v1.Volume
	
	if isKubernetesLab {
		// Para labs de Kubernetes, não precisa montar scripts de inicialização
		volumeMounts = []v1.VolumeMount{
			{
				Name:      "lab-files",
				MountPath: "/lab-files",
			},
		}
		
		volumes = []v1.Volume{
			{
				Name: "lab-files",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "lab-files",
						},
					},
				},
			},
		}
	} else if isDockerLab {
		// Para labs Docker, montar apenas o script de inicialização Docker
		volumeMounts = []v1.VolumeMount{
			{
				Name:      "docker-init-script",
				MountPath: "/scripts",
			},
			{
				Name:      "lab-files",
				MountPath: "/lab-files",
			},
		}
		
		volumes = []v1.Volume{
			{
				Name: "docker-init-script",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "docker-init-script",
						},
						DefaultMode: pointer.Int32(0755), // Tornar executável
					},
				},
			},
			{
				Name: "lab-files",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "lab-files",
						},
					},
				},
			},
		}
	} else if isLocalStackLab {
		// Para labs LocalStack, apenas montar os arquivos do laboratório
		volumeMounts = []v1.VolumeMount{
			{
				Name:      "lab-files",
				MountPath: "/lab-files",
			},
		}
		
		volumes = []v1.Volume{
			{
				Name: "lab-files",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "lab-files",
						},
					},
				},
			},
		}
	} else {
		// Para outros labs, montar os scripts de inicialização padrão
		volumeMounts = []v1.VolumeMount{
			{
				Name:      "k3s-init-script",
				MountPath: "/scripts",
			},
			{
				Name:      "lab-files",
				MountPath: "/lab-files",
			},
		}
		
		volumes = []v1.Volume{
			{
				Name: "k3s-init-script",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "k3s-init-script",
						},
						DefaultMode: pointer.Int32(0755), // Tornar executável
					},
				},
			},
			{
				Name: "lab-files",
				VolumeSource: v1.VolumeSource{
					ConfigMap: &v1.ConfigMapVolumeSource{
						LocalObjectReference: v1.LocalObjectReference{
							Name: "lab-files",
						},
					},
				},
			},
		}
	}

	// Definer o readiness probe do lab
	var readinessProbe *v1.Probe
	
	if isKubernetesLab {
		// Para labs de Kubernetes é necessário aguardar um certo tempo até que o cluster interno esteja inicializado corretamente
		readinessProbe = &v1.Probe{
			ProbeHandler: v1.ProbeHandler{
				Exec: &v1.ExecAction{
					Command: []string{"kubectl", "wait", "--for=condition=Ready", "nodes", "--all", "--timeout=60s"},
				},
			},
			InitialDelaySeconds: 120,
			PeriodSeconds:       10,
			TimeoutSeconds:      5,
			FailureThreshold:    10,
		}
	}

	// Definir recursos do pod
	pod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: podName,
			Labels: map[string]string{
				"app":      "girus-lab",
				"user":     userId,
				"template": templateName,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "lab",
					Image: labImage,
					Command: command,
					Ports: []v1.ContainerPort{
						{
							ContainerPort: 22,
							Protocol:      "TCP",
						},
					},
					SecurityContext: &v1.SecurityContext{
						Privileged: pointer.Bool(true), // Necessário para Docker-in-Docker e Kubernetes
					},
					Resources: v1.ResourceRequirements{
						Requests: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("200m"),
							v1.ResourceMemory: resource.MustParse("512Mi"),
						},
						Limits: v1.ResourceList{
							v1.ResourceCPU:    resource.MustParse("500m"),
							v1.ResourceMemory: resource.MustParse("1Gi"),
						},
					},
					VolumeMounts: volumeMounts,
					ReadinessProbe: readinessProbe,
				},
			},
			Volumes: volumes,
		},
	}

	// Criar o pod
	ctx, cancel = contextWithTimeout()
	defer cancel()
	_, err = lm.clientset.CoreV1().Pods(namespace).Create(ctx, pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("erro ao criar pod: %v", err)
	}

	log.Printf("Laboratório criado com sucesso: namespace=%s, pod=%s", namespace, podName)
	return nil
}

func (lm *LabManager) getKubeConfig() (*rest.Config, error) {
	return lm.config, nil
}

// Função de utilidade para converter mapa em EnvVars do Kubernetes
func createEnvVarsFromMap(envMap map[string]string) []v1.EnvVar {
	envVars := []v1.EnvVar{}
	
	// Adicionar variáveis de ambiente para UTF-8
	baseEnvVars := map[string]string{
		"LANG": "C.UTF-8",
		"LC_ALL": "C.UTF-8",
		"TERM": "xterm-256color",
	}
	
	// Mesclar com as variáveis fornecidas na configuração
	for key, value := range baseEnvVars {
		if _, exists := envMap[key]; !exists {
			envMap[key] = value
		}
	}
	
	for key, value := range envMap {
		envVars = append(envVars, v1.EnvVar{
			Name:  key,
			Value: value,
		})
	}
	return envVars
}

// Função de utilidade para criar requisitos de recursos
func createResourceRequirements(resources ResourceConfig) v1.ResourceRequirements {
	reqs := v1.ResourceRequirements{}

	// Adicionar requisitos se configurados
	if resources.CpuRequest != "" || resources.MemoryRequest != "" {
		reqs.Requests = v1.ResourceList{}
		if resources.CpuRequest != "" {
			quantity, err := resource.ParseQuantity(resources.CpuRequest)
			if err == nil {
				reqs.Requests[v1.ResourceCPU] = quantity
			}
		}
		if resources.MemoryRequest != "" {
			quantity, err := resource.ParseQuantity(resources.MemoryRequest)
			if err == nil {
				reqs.Requests[v1.ResourceMemory] = quantity
			}
		}
	}

	// Adicionar limites se configurados
	if resources.CpuLimit != "" || resources.MemoryLimit != "" {
		reqs.Limits = v1.ResourceList{}
		if resources.CpuLimit != "" {
			quantity, err := resource.ParseQuantity(resources.CpuLimit)
			if err == nil {
				reqs.Limits[v1.ResourceCPU] = quantity
			}
		}
		if resources.MemoryLimit != "" {
			quantity, err := resource.ParseQuantity(resources.MemoryLimit)
			if err == nil {
				reqs.Limits[v1.ResourceMemory] = quantity
			}
		}
	}

	return reqs
}

// GetAvailableTemplates retorna todos os templates disponíveis
func (lm *LabManager) GetAvailableTemplates() []*LabTemplate {
	return lm.templates.ListTemplates()
}

// ExecuteCommandInPod executa um comando em um pod e retorna a saída
func ExecuteCommandInPod(pod *v1.Pod, command []string) (string, string, error) {
	// Garantir que o pod existe e está pronto
	if pod == nil {
		return "", "", fmt.Errorf("pod nulo fornecido para ExecuteCommandInPod")
	}

	log.Printf("Executando comando no pod %s/%s: %v", pod.Namespace, pod.Name, command)

	config, err := rest.InClusterConfig()
	if err != nil {
		// Fallback para desenvolvimento local
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig)
		if err != nil {
			log.Printf("Erro ao obter configuração do Kubernetes: %v", err)
			return "", "", fmt.Errorf("falha ao obter configuração do cluster: %v", err)
		}
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Printf("Erro ao criar cliente Kubernetes: %v", err)
		return "", "", fmt.Errorf("falha ao criar cliente Kubernetes: %v", err)
	}

	// Verificar se o pod está em execução
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podStatus, err := clientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		log.Printf("Erro ao obter status do pod %s/%s: %v", pod.Namespace, pod.Name, err)
		return "", "", fmt.Errorf("falha ao obter status do pod: %v", err)
	}

	if podStatus.Status.Phase != v1.PodRunning {
		log.Printf("Pod %s/%s não está em execução (status atual: %s)", pod.Namespace, pod.Name, podStatus.Status.Phase)
		return "", "", fmt.Errorf("pod não está em execução (status: %s)", podStatus.Status.Phase)
	}

	// Verificar se o pod tem contêineres prontos
	if len(podStatus.Status.ContainerStatuses) == 0 || !podStatus.Status.ContainerStatuses[0].Ready {
		log.Printf("Contêiner principal no pod %s/%s não está pronto", pod.Namespace, pod.Name)
		return "", "", fmt.Errorf("contêiner principal não está pronto")
	}

	// Adicionar configuração de ambiente para UTF-8 e reduzir a complexidade da linha de comando
	commandStr := strings.Join(command[2:], " ")
	wrappedCommand := []string{"/bin/bash", "-c", fmt.Sprintf("export LC_ALL=C.UTF-8 && export LANG=C.UTF-8 && %s", commandStr)}
	
	log.Printf("Comando final para execução: %v", wrappedCommand)
	
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(pod.Name).
		Namespace(pod.Namespace).
		SubResource("exec")

	req.VersionedParams(&v1.PodExecOptions{
		Command: wrappedCommand,
		Stdin:   false,
		Stdout:  true,
		Stderr:  true,
		TTY:     false,
	}, scheme.ParameterCodec)

	var stdout, stderr bytes.Buffer
	exec, err := remotecommand.NewSPDYExecutor(config, "POST", req.URL())
	if err != nil {
		log.Printf("Erro ao criar executor SPDY: %v", err)
		return "", "", fmt.Errorf("erro ao criar executor: %v", err)
	}

	err = exec.Stream(remotecommand.StreamOptions{
		Stdin:  nil,
		Stdout: &stdout,
		Stderr: &stderr,
		Tty:    false,
	})

	stdoutStr := stdout.String()
	stderrStr := stderr.String()
	
	if err != nil {
		log.Printf("Erro durante execução do comando: %v, stdout: %s, stderr: %s", 
			err, truncateString(stdoutStr, 100), truncateString(stderrStr, 100))
	} else {
		log.Printf("Comando executado com sucesso. stdout: %s, stderr: %s", 
			truncateString(stdoutStr, 100), truncateString(stderrStr, 100))
	}

	return stdoutStr, stderrStr, err
}

// Função utilitária para truncar strings longas nos logs
func truncateString(s string, maxLen int) string {
	s = strings.TrimSpace(s)
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// generateRestrictedKubeConfig gera um kubeconfig com permissões limitadas para o laboratório
func generateRestrictedKubeConfig() string {
	// Obter configuração do host
	config, err := rest.InClusterConfig()
	if err != nil {
		log.Printf("Erro ao obter configuração in-cluster: %v", err)
		// Fallback para kubeconfig local
		kubeconfig := os.Getenv("KUBECONFIG")
		if kubeconfig == "" {
			kubeconfig = filepath.Join(os.Getenv("HOME"), ".kube", "config")
		}
		
		// Ler o conteúdo do arquivo
		content, err := os.ReadFile(kubeconfig)
		if err != nil {
			log.Printf("Erro ao ler kubeconfig: %v", err)
			return ""
		}
		
		return string(content)
	}
	
	// Criar um kubeconfig básico usando a configuração interna
	// Este kubeconfig usa a conta de serviço montada no pod para autenticação
	kubeConfig := fmt.Sprintf(`apiVersion: v1
kind: Config
clusters:
- cluster:
    certificate-authority-data: %s
    server: %s
  name: kubernetes
contexts:
- context:
    cluster: kubernetes
    user: lab-user
  name: lab-context
current-context: lab-context
users:
- name: lab-user
  user:
    token: %s
`, base64.StdEncoding.EncodeToString(config.TLSClientConfig.CAData), config.Host, config.BearerToken)

	return kubeConfig
}

// formatTasks formata as tarefas para o formato YAML
func formatTasks(tasks []Task) string {
	var sb strings.Builder
	for _, task := range tasks {
		sb.WriteString(fmt.Sprintf("- name: %s\n", task.Name))
		sb.WriteString(fmt.Sprintf("  description: %s\n", task.Description))

		sb.WriteString("  steps:\n")
		for _, step := range task.Steps {
			sb.WriteString(fmt.Sprintf("  - %s\n", step))
		}
	}
	return sb.String()
}

// GetPod obtém um pod pelo namespace e nome
func (lm *LabManager) GetPod(namespace, podName string) (*v1.Pod, error) {
	ctx, cancel := contextWithTimeout()
	defer cancel()
	return lm.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
}

// GetTemplate obtém um template pelo nome
func (lm *LabManager) GetTemplate(templateId string) *LabTemplate {
	return lm.templates.GetTemplate(templateId)
}

// ValidateTask valida uma tarefa em um pod
func (lm *LabManager) ValidateTask(pod *v1.Pod, task Task) (bool, string) {
	return lm.templates.ValidateTaskCompletion(pod, task)
}

// ValidateLabCompletion valida se todas as tarefas do laboratório foram concluídas
func (lm *LabManager) ValidateLabCompletion(pod *v1.Pod, templateId string) (bool, string) {
	template := lm.GetTemplate(templateId)
	if template == nil {
		return false, "Template do laboratório não encontrado"
	}

	// Verificar cada tarefa do template
	totalTasks := len(template.Tasks)
	completedTasks := 0
	failedTasks := []string{}

	for i, task := range template.Tasks {
		success, message := lm.ValidateTask(pod, task)
		if success {
			completedTasks++
		} else {
			failedTasks = append(failedTasks, fmt.Sprintf("Tarefa %d (%s): %s", i+1, task.Name, message))
		}
	}

	// Verificar resultado
	if completedTasks == totalTasks {
		return true, fmt.Sprintf("Parabéns! Todas as %d tarefas do laboratório '%s' foram concluídas com sucesso!", totalTasks, template.Title)
	} else {
		failedMessage := fmt.Sprintf("Progresso: %d/%d tarefas concluídas. Tarefas pendentes:\n- %s", 
			completedTasks, totalTasks, strings.Join(failedTasks, "\n- "))
		return false, failedMessage
	}
}

// generateK3sInitScript gera um script que inicializa um cluster K3s
func generateK3sInitScript() string {
	return `#!/bin/bash
set -e

echo "============================================================"
echo "Inicializando ambiente Kubernetes pré-configurado"
echo "============================================================"

echo "A imagem linuxtips/girus-kind-single-node:0.1 já possui o Kubernetes completamente configurado"
echo "Não é necessário realizar nenhuma configuração adicional"

echo "============================================================"
echo "Cluster Kubernetes já está pronto para uso!"
echo "============================================================"
echo ""
echo "* Execute 'kubectl get pods' para ver os pods em execução"
echo "* Execute 'kubectl get all' para ver todos os recursos"
echo "============================================================"

# Manter o container em execução
tail -f /dev/null
`
}

// generateDockerInitScript gera um script de inicialização Docker
func generateDockerInitScript() string {
	return `#!/bin/bash
set -e

echo "============================================================"
echo "Inicializando ambiente Docker para o laboratório"
echo "============================================================"

# Criar um diretório para logs com as permissões corretas
mkdir -p /tmp/docker-logs
chmod 777 /tmp/docker-logs

echo "Iniciando o daemon Docker diretamente..."
# Usar /tmp para logs pois tem permissões corretas
sudo dockerd &>/tmp/docker-logs/dockerd.log &
DOCKER_PID=$!

# Verificar se o processo está rodando
if ! ps -p $DOCKER_PID > /dev/null; then
    echo "Falha ao iniciar o Docker. Verificando logs:"
    tail -n 20 /tmp/docker-logs/dockerd.log
    echo "Tentando método alternativo..."
    sudo dockerd > /tmp/docker-logs/dockerd.log 2>&1 &
    sleep 3
fi

# Aguardar o Docker iniciar
echo "Aguardando o Docker iniciar..."
max_attempts=30
for i in $(seq 1 $max_attempts); do
    if docker info &>/dev/null; then
        echo "Docker iniciado com sucesso!"
        docker ps
        break
    fi
    
    if [ $i -eq $max_attempts ]; then
        echo "Timeout aguardando o Docker iniciar. Verifique os logs:"
        tail -n 20 /tmp/docker-logs/dockerd.log
        echo "Continuando mesmo assim..."
    fi
    
    echo -n "."
    sleep 1
done

echo "============================================================"
echo "Ambiente Docker pronto para uso!"
echo "============================================================"
echo ""
echo "* Execute 'docker ps' para listar contêineres"
echo "* Execute 'docker run hello-world' para testar"
echo "* Execute 'docker images' para listar imagens"
echo "============================================================"

# Manter o contêiner em execução
tail -f /dev/null
`
}

// generateUniquePodName gera um nome único para o pod baseado no prefixo e no ID do usuário
func generateUniquePodName(prefix, userId string) string {
	// Criar um nome único com timestamp
	timestamp := time.Now().UnixNano() / int64(time.Millisecond)
	randomSuffix := rand.Intn(1000)
	uniqueName := fmt.Sprintf("%s-%s-%d-%d", prefix, userId, timestamp, randomSuffix)
	
	// Garantir que o nome seja compatível com DNS (max 63 caracteres, só minúsculas, etc)
	// Remover caracteres inválidos
	reg := regexp.MustCompile(`[^a-z0-9-]`)
	uniqueName = reg.ReplaceAllString(strings.ToLower(uniqueName), "")
	
	// Limitar a 63 caracteres (máximo para nomes de recursos Kubernetes)
	if len(uniqueName) > 63 {
		uniqueName = uniqueName[:63]
	}
	
	// Garantir que não termine com hífen
	uniqueName = strings.TrimSuffix(uniqueName, "-")
	
	return uniqueName
}

// DeletePod exclui um pod específico
func (lm *LabManager) DeletePod(namespace, podName string) error {
	// Verificar se o pod existe
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := lm.clientset.CoreV1().Pods(namespace).Get(ctx, podName, metav1.GetOptions{})
	if err != nil {
		log.Printf("Pod %s/%s não encontrado, pode já ter sido excluído", namespace, podName)
		return nil
	}

	// Excluir o pod
	ctx, cancel = contextWithTimeout()
	defer cancel()
	err = lm.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, metav1.DeleteOptions{
		GracePeriodSeconds: pointer.Int64(5),
	})
	if err != nil {
		return fmt.Errorf("erro ao excluir pod: %v", err)
	}

	log.Printf("Pod %s/%s excluído com sucesso", namespace, podName)
	return nil
}

// RemoveLabByUserID remove o laboratório associado a um ID de usuário
func (lm *LabManager) RemoveLabByUserID(userID string) {
	labInfo, found := lm.GetLabByUserID(userID)
	if !found {
		log.Printf("Nenhum laboratório encontrado para o usuário %s", userID)
		return
	}

	// Excluir pods no namespace
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podList, err := lm.clientset.CoreV1().Pods(labInfo.Namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("Erro ao listar pods: %v", err)
		return
	}

	// Excluir cada pod encontrado
	for _, pod := range podList.Items {
		if err := lm.DeletePod(labInfo.Namespace, pod.Name); err != nil {
			log.Printf("Erro ao excluir pod %s: %v", pod.Name, err)
		}
	}

	// Atualizar o mapa de usuários
	lm.mu.Lock()
	delete(lm.userLabMap, userID)
	lm.mu.Unlock()

	log.Printf("Laboratório do usuário %s removido com sucesso", userID)
}

// Adicionar uma estrutura para armazenar informações do laboratório
type LabInfo struct {
	Namespace      string `json:"namespace"`
	PodName        string `json:"podName"`
	TemplateID     string `json:"templateId"`
	Status         string `json:"status"`
	YoutubeVideo   string `json:"youtubeVideo,omitempty"`
	TimerEnabled   bool   `json:"timerEnabled,omitempty"`
	StartTime      string `json:"startTime,omitempty"`
	ExpirationTime string `json:"expirationTime,omitempty"`
	RemainingTime  int64  `json:"remainingTime,omitempty"` // Em segundos
}

// GetLabByUserID retorna as informações do laboratório associado a um usuário
func (lm *LabManager) GetLabByUserID(userID string) (LabInfo, bool) {
	// Verificar se o namespace existe
	namespace := fmt.Sprintf("lab-%s", userID)
	
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := lm.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		log.Printf("Namespace %s não encontrado: %v", namespace, err)
		return LabInfo{}, false
	}

	// Buscar pods no namespace
	ctx, cancel = contextWithTimeout()
	defer cancel()
	podList, err := lm.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: "app=girus-lab",
	})
	if err != nil || len(podList.Items) == 0 {
		log.Printf("Nenhum pod encontrado no namespace %s: %v", namespace, err)
		return LabInfo{}, false
	}

	// Retornar informações do primeiro pod encontrado
	pod := podList.Items[0]
	templateID := pod.Labels["template"]
	
	// Obter o template para pegar a URL do vídeo
	template := lm.GetTemplate(templateID)
	var youtubeVideo string
	var timerEnabled bool
	var startTime, expirationTime string
	var remainingTime int64
	
	if template != nil {
		youtubeVideo = template.YoutubeVideo
		timerEnabled = template.TimerEnabled
		
		// Se o timer estiver habilitado, calcular informações de tempo
		if timerEnabled {
			// Obter o tempo de início como a data de criação do pod
			startTimeObj := pod.CreationTimestamp.Time
			startTime = startTimeObj.Format(time.RFC3339)
			
			// Calcular o tempo de expiração baseado na MaxDuration
			var maxDuration time.Duration
			if template.MaxDuration != "" {
				maxDuration, err = time.ParseDuration(template.MaxDuration)
				if err != nil {
					log.Printf("Erro ao analisar MaxDuration '%s': %v", template.MaxDuration, err)
					maxDuration = 1 * time.Hour // Padrão: 1 hora
				}
			} else {
				maxDuration = 1 * time.Hour // Padrão: 1 hora
			}
			
			expirationTimeObj := startTimeObj.Add(maxDuration)
			expirationTime = expirationTimeObj.Format(time.RFC3339)
			
			// Calcular tempo restante em segundos
			now := time.Now()
			if now.Before(expirationTimeObj) {
				remainingTime = int64(expirationTimeObj.Sub(now).Seconds())
			} else {
				remainingTime = 0
			}
		}
	}
	
	return LabInfo{
		Namespace:      namespace,
		PodName:        pod.Name,
		TemplateID:     templateID,
		Status:         string(pod.Status.Phase),
		YoutubeVideo:   youtubeVideo,
		TimerEnabled:   timerEnabled,
		StartTime:      startTime,
		ExpirationTime: expirationTime,
		RemainingTime:  remainingTime,
	}, true
}

// CreateLab cria um novo laboratório para o usuário
func (lm *LabManager) CreateLab(userID, templateID string) (string, string, error) {
	// Verificar se o template existe
	template := lm.GetTemplate(templateID)
	if template == nil {
		return "", "", fmt.Errorf("template não encontrado: %s", templateID)
	}

	// Criar namespace se não existir
	namespace := fmt.Sprintf("lab-%s", userID)
	ctx, cancel := contextWithTimeout()
	defer cancel()
	_, err := lm.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		// Namespace não existe, criar
		ctx, cancel = contextWithTimeout()
		defer cancel()
		_, err = lm.clientset.CoreV1().Namespaces().Create(
			ctx,
			&v1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"user-id": userID,
					},
				},
			},
			metav1.CreateOptions{},
		)
		if err != nil {
			return "", "", fmt.Errorf("erro ao criar namespace: %v", err)
		}
	}

	// Gerar um nome de pod único
	podName := generateUniquePodName("lab", userID)
	log.Printf("[Lab Manager] Criando novo pod com nome único: %s", podName)

	// Verificar se existem pods antigos deste usuário no namespace - opcional
	ctx, cancel = contextWithTimeout()
	defer cancel()
	podList, err := lm.clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
		LabelSelector: fmt.Sprintf("user=%s", userID),
	})
	if err == nil && len(podList.Items) > 0 {
		log.Printf("[Lab Manager] Encontrados %d pods antigos para o usuário %s", len(podList.Items), userID)
		// Podemos opcionalmente iniciar a limpeza dos pods antigos em segundo plano
		go func() {
			for _, oldPod := range podList.Items {
				// Não excluir o pod que acabamos de criar
				if oldPod.Name != podName {
					log.Printf("[Lab Manager] Excluindo pod antigo: %s", oldPod.Name)
					_ = lm.DeletePod(namespace, oldPod.Name)
				}
			}
		}()
	}

	// Registrar o templateID para este usuário
	lm.mu.Lock()
	lm.userLabMap[userID] = templateID
	lm.mu.Unlock()

	// ... resto do código ...

	return namespace, podName, nil
}

// DeleteLabEnvironment exclui o ambiente de laboratório para o usuário especificado
func (lm *LabManager) DeleteLabEnvironment(userID string, forceDelete bool) error {
	lm.mu.Lock()
	labInfo, ok := lm.userLabMap[userID]
	if !ok {
		lm.mu.Unlock()
		return fmt.Errorf("laboratório não encontrado para o usuário: %s", userID)
	}
	
	// Obter informações completas sobre o laboratório
	fullLabInfo, found := lm.GetLabByUserID(userID)
	if found && fullLabInfo.TimerEnabled && fullLabInfo.ExpirationTime != "" {
		// Se o laboratório tem timer e o tempo expirou, força a exclusão
		expirationTime, err := time.Parse(time.RFC3339, fullLabInfo.ExpirationTime)
		if err == nil && time.Now().After(expirationTime) {
			forceDelete = true
			log.Printf("Laboratório %s expirou, forçando exclusão", fullLabInfo.PodName)
		}
	}

	namespace := fmt.Sprintf("lab-%s", userID)
	podName := labInfo

	// Remover mapeamento do usuário
	delete(lm.userLabMap, userID)
	lm.mu.Unlock()

	log.Printf("Excluindo laboratório para o usuário %s (namespace: %s, pod: %s, force: %v)", 
		userID, namespace, podName, forceDelete)

	// Verificar se o namespace existe
	ctx, cancel := contextWithTimeout()
	defer cancel()
	var err error
	_, err = lm.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
	if err != nil {
		if errors.IsNotFound(err) {
			log.Printf("Namespace %s não encontrado, nada a excluir", namespace)
			return nil
		}
		return fmt.Errorf("erro ao verificar namespace: %v", err)
	}

	// Excluir o pod
	ctx, cancel = contextWithTimeout()
	defer cancel()
	deletePolicy := metav1.DeletePropagationForeground
	var gracePeriod int64 = 30
	if forceDelete {
		gracePeriod = 0 // Exclusão forçada sem espera
	}
	deleteOpts := metav1.DeleteOptions{
		PropagationPolicy:  &deletePolicy,
		GracePeriodSeconds: pointer.Int64(gracePeriod),
	}
	err = lm.clientset.CoreV1().Pods(namespace).Delete(ctx, podName, deleteOpts)
	if err != nil && !errors.IsNotFound(err) {
		log.Printf("Erro ao excluir pod %s no namespace %s: %v", podName, namespace, err)
		// Continuar mesmo com erro para tentar excluir o namespace
	}

	// Excluir o namespace inteiro
	err = lm.clientset.CoreV1().Namespaces().Delete(ctx, namespace, deleteOpts)
	if err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("erro ao excluir namespace %s: %v", namespace, err)
	}

	log.Printf("Laboratório excluído com sucesso para o usuário %s", userID)
	return nil
}

// WaitForPodReady espera até que o pod esteja pronto
func WaitForPodReady(clientset kubernetes.Interface, pod *v1.Pod, timeout time.Duration) error {
	start := time.Now()
	for {
		// Verificar se atingimos o timeout
		if time.Since(start) > timeout {
			return fmt.Errorf("timeout esperando o pod ficar pronto")
		}

		// Verificar status atual do pod
		ctx, cancel := contextWithTimeout()
		defer cancel()
		podStatus, err := clientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
		if err != nil {
			return fmt.Errorf("erro ao verificar status do pod: %v", err)
		}

		// Verificar se o pod está em execução e todos os contêineres estão prontos
		if podStatus.Status.Phase == v1.PodRunning {
			allContainersReady := true
			for _, containerStatus := range podStatus.Status.ContainerStatuses {
				if !containerStatus.Ready {
					allContainersReady = false
					break
				}
			}
			if allContainersReady {
				return nil // Pod está pronto
			}
		}

		// Aguardar um pouco antes de verificar novamente
		time.Sleep(2 * time.Second)
	}
}

// isPodReady verifica se um pod está pronto
func isPodReady(clientset kubernetes.Interface, pod *v1.Pod) (bool, error) {
	ctx, cancel := contextWithTimeout()
	defer cancel()
	podStatus, err := clientset.CoreV1().Pods(pod.Namespace).Get(ctx, pod.Name, metav1.GetOptions{})
	if err != nil {
		return false, err
	}

	// Verificar se o pod está em execução e todos os contêineres estão prontos
	if podStatus.Status.Phase == v1.PodRunning {
		allContainersReady := true
		for _, containerStatus := range podStatus.Status.ContainerStatuses {
			if !containerStatus.Ready {
				allContainersReady = false
				break
			}
		}
		return allContainersReady, nil
	}
	return false, nil
}

// StartTimerMonitor inicia o monitoramento de laboratórios com timer
func (lm *LabManager) StartTimerMonitor(ctx context.Context) {
	log.Printf("Iniciando monitoramento de laboratórios com timer")
	
	// Verificar a cada 10 segundos em vez de 1 minuto
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()
	
	// Verificar imediatamente na inicialização
	lm.checkAndTerminateExpiredLabs()
	
	go func() {
		for {
			select {
			case <-ticker.C:
				lm.checkAndTerminateExpiredLabs()
			case <-ctx.Done():
				log.Printf("Monitoramento de timer encerrado")
				return
			}
		}
	}()
}

// checkAndTerminateExpiredLabs verifica e encerra laboratórios expirados
func (lm *LabManager) checkAndTerminateExpiredLabs() {
	log.Printf("Verificando laboratórios expirados")

	// Listar todos os namespaces
	ctx, cancel := contextWithTimeout()
	defer cancel()
	
	namespaces, err := lm.clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		log.Printf("Erro ao listar namespaces: %v", err)
		return
	}
	
	now := time.Now()
	labsVerificados := 0
	labsExpirados := 0
	
	for _, ns := range namespaces.Items {
		if !strings.HasPrefix(ns.Name, "lab-") {
			continue
		}
		
		// Extrair ID do usuário do nome do namespace
		userID := strings.TrimPrefix(ns.Name, "lab-")
		labsVerificados++
		
		// Obter informações do laboratório
		labInfo, found := lm.GetLabByUserID(userID)
		if !found {
			continue
		}
		
		// Verificar se o laboratório tem timer habilitado
		if !labInfo.TimerEnabled {
			continue
		}
		
		// Verificar se o tempo expirou
		if labInfo.ExpirationTime != "" {
			expirationTime, err := time.Parse(time.RFC3339, labInfo.ExpirationTime)
			if err != nil {
				log.Printf("Erro ao analisar tempo de expiração '%s': %v", labInfo.ExpirationTime, err)
				continue
			}
			
			if now.After(expirationTime) {
				labsExpirados++
				log.Printf("LABORATÓRIO EXPIRADO: %s (userID: %s, expirou em: %s)",
					labInfo.PodName, userID, expirationTime.Format(time.RFC3339))
				
				// Listar todos os pods no namespace
				ctx, cancel = contextWithTimeout()
				podList, podErr := lm.clientset.CoreV1().Pods(labInfo.Namespace).List(ctx, metav1.ListOptions{})
				cancel()
				
				if podErr != nil {
					log.Printf("Erro ao listar pods no namespace %s: %v", labInfo.Namespace, podErr)
					continue
				}
				
				log.Printf("Removendo %d pods do laboratório expirado %s", len(podList.Items), labInfo.PodName)
				
				// Remover cada pod individualmente usando ForceDelete para garantir
				for _, pod := range podList.Items {
					log.Printf("Removendo pod expirado: %s", pod.Name)
					ctx, cancel = contextWithTimeout()
					
					// Usar DeleteOptions com GracePeriodSeconds=0 para forçar remoção
					deletePolicy := metav1.DeletePropagationForeground
					deleteOptions := metav1.DeleteOptions{
						PropagationPolicy:  &deletePolicy,
						GracePeriodSeconds: pointer.Int64(0), // Forçar remoção imediata
					}
					
					err = lm.clientset.CoreV1().Pods(labInfo.Namespace).Delete(ctx, pod.Name, deleteOptions)
					cancel()
					
					if err != nil {
						log.Printf("Erro ao remover pod expirado %s: %v", pod.Name, err)
					} else {
						log.Printf("Pod %s removido com sucesso", pod.Name)
					}
				}
				
				// Remover mapeamento do usuário
				lm.mu.Lock()
				delete(lm.userLabMap, userID)
				lm.mu.Unlock()
				
				log.Printf("Laboratório %s removido por expiração de tempo", labInfo.PodName)
			} else {
				timeLeft := expirationTime.Sub(now)
				log.Printf("Laboratório %s (userID: %s) ainda tem %s de tempo restante", 
					labInfo.PodName, userID, timeLeft.Round(time.Second))
			}
		}
	}
	
	log.Printf("Verificação concluída: %d laboratórios verificados, %d expirados", labsVerificados, labsExpirados)
}

// GetCurrentLab retorna o laboratório atual (último acessado)
func (lm *LabManager) GetCurrentLab() (struct {
	UserID string
	LabID  string
}, bool) {
	lm.mu.RLock()
	defer lm.mu.RUnlock()

	// Como não temos um conceito real de "laboratório atual",
	// vamos retornar o primeiro laboratório encontrado no mapa
	for userID, labID := range lm.userLabMap {
		// Verificar se o laboratório ainda existe
		namespace := fmt.Sprintf("lab-%s", userID)
		ctx, cancel := contextWithTimeout()
		defer cancel()

		// Verificar se o namespace existe
		_, err := lm.clientset.CoreV1().Namespaces().Get(ctx, namespace, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				log.Printf("Namespace %s não encontrado, removendo do mapa", namespace)
				delete(lm.userLabMap, userID)
				continue
			}
			log.Printf("Erro ao verificar namespace %s: %v", namespace, err)
			continue
		}

		// Verificar se o pod existe
		_, err = lm.clientset.CoreV1().Pods(namespace).Get(ctx, labID, metav1.GetOptions{})
		if err != nil {
			if errors.IsNotFound(err) {
				log.Printf("Pod %s/%s não encontrado, removendo do mapa", namespace, labID)
				delete(lm.userLabMap, userID)
				continue
			}
			log.Printf("Erro ao verificar pod %s/%s: %v", namespace, labID, err)
			continue
		}

		return struct {
			UserID string
			LabID  string
		}{
			UserID: userID,
			LabID:  labID,
		}, true
	}

	return struct {
		UserID string
		LabID  string
	}{}, false
}

