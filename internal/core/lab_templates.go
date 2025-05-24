package core

import (
	"fmt"
	"log"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v2"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

// LabTemplate define o template completo de um laboratório
type LabTemplate struct {
	Name        string         `json:"name" yaml:"name"`
	Title       string         `json:"title" yaml:"title"`
	Description string         `json:"description" yaml:"description"`
	Duration    string         `json:"duration" yaml:"duration"`
	Tasks       []Task         `json:"tasks" yaml:"tasks"`
	Files       []TemplateFile `json:"files" yaml:"files"`
	YoutubeVideo string        `json:"youtubeVideo" yaml:"youtubeVideo"`
	Image       string         `json:"image" yaml:"image"`
	TimerEnabled bool          `json:"timerEnabled" yaml:"timerEnabled"`
	MaxDuration  string        `json:"maxDuration" yaml:"maxDuration"` // Formato: "30m", "2h", etc.
}

// TemplateFile define um arquivo de conteúdo do template
type TemplateFile struct {
	Path string `json:"path" yaml:"path"`
}

// Task define uma tarefa dentro do laboratório
type Task struct {
	Name        string      `json:"name" yaml:"name"`
	Description string      `json:"description" yaml:"description"`
	Steps       []string    `json:"steps" yaml:"steps"`
	Tips        []Tip       `json:"tips,omitempty" yaml:"tips,omitempty"`
	Validation  []Validator `json:"validation" yaml:"validation"`
}

// Tip define uma dica associada a uma tarefa
type Tip struct {
	Type    string `json:"type" yaml:"type"`
	Title   string `json:"title" yaml:"title"`
	Content string `json:"content" yaml:"content"`
}

// Validator define como uma tarefa é validada
type Validator struct {
	Command        string `json:"command" yaml:"command"`
	ExpectedOutput string `json:"expectedOutput" yaml:"expectedOutput"`
	ErrorMessage   string `json:"errorMessage" yaml:"errorMessage"`
}

// TemplateManager gerencia os templates de laboratório
type TemplateManager struct {
	templates map[string]*LabTemplate
}

// NewTemplateManager cria um novo gerenciador de templates
func NewTemplateManager() *TemplateManager {
	return &TemplateManager{
		templates: make(map[string]*LabTemplate),
	}
}

// LoadTemplates carrega templates do ConfigMap
func (tm *TemplateManager) LoadTemplates(clientset kubernetes.Interface) error {
	// Buscar ConfigMaps com label app=girus-lab-template
	ctx, cancel := contextWithTimeout()
	defer cancel()
	configMaps, err := clientset.CoreV1().ConfigMaps("girus").List(ctx, metav1.ListOptions{
		LabelSelector: "app=girus-lab-template",
	})
	if err != nil {
		return fmt.Errorf("erro ao buscar templates de laboratório: %v", err)
	}

	for _, cm := range configMaps.Items {
		for key, content := range cm.Data {
			if filepath.Ext(key) == ".yaml" || filepath.Ext(key) == ".yml" {
				template := &LabTemplate{}
				if err := yaml.Unmarshal([]byte(content), template); err != nil {
					log.Printf("Erro ao desserializar template %s: %v", key, err)
					continue
				}
				
				// Log detalhado para depuração
				log.Printf("Template carregado: %s (%s) com %d tarefas", template.Name, template.Title, len(template.Tasks))
				for i, task := range template.Tasks {
					log.Printf("Tarefa %d: %s - Tips: %d", i, task.Name, len(task.Tips))
					for j, tip := range task.Tips {
						log.Printf("  Tip %d: %s (tipo: %s)", j, tip.Title, tip.Type)
					}
				}
				
				tm.templates[template.Name] = template
			}
		}
	}

	log.Printf("Carregados %d templates de laboratório", len(tm.templates))
	return nil
}

// GetTemplate retorna um template pelo nome
func (tm *TemplateManager) GetTemplate(name string) *LabTemplate {
	return tm.templates[name]
}

// ListTemplates retorna a lista de templates disponíveis
func (tm *TemplateManager) ListTemplates() []*LabTemplate {
	templates := make([]*LabTemplate, 0, len(tm.templates))
	for _, tpl := range tm.templates {
		templates = append(templates, tpl)
	}
	return templates
}

// ValidateTaskCompletion valida se uma tarefa foi concluída
func (tm *TemplateManager) ValidateTaskCompletion(pod *v1.Pod, task Task) (bool, string) {
	for _, validator := range task.Validation {
		// Executar comando de validação no pod
		command := []string{"/bin/sh", "-c", validator.Command}
		stdout, stderr, err := ExecuteCommandInPod(pod, command)

		if err != nil {
			return false, fmt.Sprintf("Erro ao validar a task! Veja se você concluiu o que foi pedido para a task.")
		}

		if stderr != "" {
			return false, fmt.Sprintf("Erro ao validar a task! Veja se você concluiu o que foi pedido para a task.")
		}

		// Limpar a saída do comando e o valor esperado (remover quebras de linha e espaços)
		cleanStdout := strings.TrimSpace(stdout)
		cleanExpected := strings.TrimSpace(validator.ExpectedOutput)
		
		// Verificar se a saída corresponde ao esperado
		if cleanStdout != cleanExpected {
			log.Printf("Validação falhou. Esperado: '%s', Recebido: '%s'", cleanExpected, cleanStdout)
			return false, validator.ErrorMessage
		}
	}

	return true, "Tarefa concluída com sucesso! 🎉"
}
