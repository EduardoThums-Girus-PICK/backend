package core

import (
	"log"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port            int
	DatabaseURL     string
	KubernetesHost  string
	JWTSecret       string
	EnvironmentName string
	Lab             LabConfig `json:"lab" yaml:"lab"`
}

type LabConfig struct {
	DefaultImage     string            `json:"defaultImage" yaml:"defaultImage"`
	PodNamePrefix    string            `json:"podNamePrefix" yaml:"podNamePrefix"`
	ContainerName    string            `json:"containerName" yaml:"containerName"`
	Command          []string          `json:"command" yaml:"command"`
	PodResources     ResourceConfig    `json:"resources" yaml:"resources"`
	EnvVars          map[string]string `json:"envVars" yaml:"envVars"`
	Privileged       bool              `json:"privileged" yaml:"privileged"`
	TemplatesDir     string            `json:"templatesDir" yaml:"templatesDir"`
	ContentMountPath string            `json:"contentMountPath" yaml:"contentMountPath"`
}

type ResourceConfig struct {
	CpuRequest    string `json:"cpuRequest" yaml:"cpuRequest"`
	CpuLimit      string `json:"cpuLimit" yaml:"cpuLimit"`
	MemoryRequest string `json:"memoryRequest" yaml:"memoryRequest"`
	MemoryLimit   string `json:"memoryLimit" yaml:"memoryLimit"`
}

func NewConfig() *Config {
	port, _ := strconv.Atoi(getEnv("PORT", "8080"))

	return &Config{
		Port:            port,
		DatabaseURL:     getEnv("DATABASE_URL", "postgresql://localhost:5432/girus?sslmode=disable"),
		KubernetesHost:  getEnv("KUBERNETES_HOST", ""),
		JWTSecret:       getEnv("JWT_SECRET", "your-secret-key"),
		EnvironmentName: getEnv("ENV", "development"),
	}
}

func getEnv(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func LoadConfig() (*Config, error) {
	config := NewConfig()

	// Configure valores padrão
	if config.Lab.DefaultImage == "" {
		config.Lab.DefaultImage = "ubuntu:latest"
	}
	if config.Lab.PodNamePrefix == "" {
		config.Lab.PodNamePrefix = "lab"
	}
	if config.Lab.ContainerName == "" {
		config.Lab.ContainerName = "linux-lab"
	}
	if len(config.Lab.Command) == 0 {
		config.Lab.Command = []string{"sleep", "infinity"}
	}
	if config.Lab.EnvVars == nil {
		config.Lab.EnvVars = map[string]string{
			"TERM": "xterm-256color",
		}
	}
	if config.Lab.TemplatesDir == "" {
		config.Lab.TemplatesDir = "/tmp/lab-templates"
	}
	if config.Lab.ContentMountPath == "" {
		config.Lab.ContentMountPath = "/lab"
	}

	return config, nil
}

func GetLabImage() string {
	// Primeiro tenta ler da variável de ambiente
	if envImage := os.Getenv("LAB_DEFAULT_IMAGE"); envImage != "" {
		return envImage
	}

	// Em seguida, tenta ler da configuração
	cfg, err := LoadConfig()
	if err == nil && cfg.Lab.DefaultImage != "" {
		return cfg.Lab.DefaultImage
	}

	// Se nada for configurado, usa o valor padrão
	return "ubuntu:latest"
}

// GetImageForTemplate seleciona a imagem apropriada para o template
func GetImageForTemplate(templateName string, customImage string) string {
	// Se uma imagem personalizada for especificada, usá-la
	if customImage != "" {
		log.Printf("Usando imagem personalizada %s para o template %s", customImage, templateName)
		return customImage
	}

	// Se o template for relacionado a Kubernetes, usar a imagem pré-configurada
	if strings.Contains(templateName, "kubernetes") {
		// Usar a imagem padrão para Kubernetes que já está configurada
		return "linuxtips/girus-kind-single-node:0.1"
	}
	
	// Para templates "default", também podemos usar a imagem Kubernetes
	if templateName == "default" {
		log.Printf("Usando imagem linuxtips/girus-kind-single-node:0.1 para o template default")
		return "linuxtips/girus-kind-single-node:0.1"
	}
	
	// Para templates Linux, Terraform e Docker, usar a imagem DevOps
	if strings.Contains(strings.ToLower(templateName), "linux") ||
		strings.Contains(strings.ToLower(templateName), "terraform") ||
		strings.Contains(strings.ToLower(templateName), "docker") {
		log.Printf("Usando imagem linuxtips/girus-devops:0.1 para o template %s", templateName)
		return "linuxtips/girus-devops:0.1"
	}
	
	// Para outros templates, usar a imagem padrão
	return GetLabImage()
}
