
# Desafio PICK Girus

O arquivo `Dockerfile` da aplicação criado como parte da resolução do primeiro desafio PICK da 2ª turma de 2024.

O objetivo era criar um imagem docker de uma aplicação em golang de forma otimizada, utilizando as melhores práticas de segurança e com o menor tamanho possível.

## Backend Girus

O backend é o coração da plataforma GIRUS, responsável por orquestrar os ambientes Kubernetes para cada laboratório. Desenvolvido em Go com o framework Gin, ele gerencia o ciclo de vida dos laboratórios, fornece endpoints RESTful para o frontend, e implementa a validação automática das tarefas.

## Principais Componentes do Backend

- **LabManager**: Orquestra a criação, monitoramento e exclusão de recursos Kubernetes
- **TemplateManager**: Gerencia os templates de laboratórios disponíveis
- **API Handlers**: Implementa os endpoints RESTful e WebSocket
- **Validators**: Verifica o progresso das tarefas e fornece feedback imediato

## Endpoints Principais

- `/api/v1/templates`: Retorna a lista de templates de laboratórios disponíveis
- `/api/v1/labs`: Cria um novo laboratório
- `/api/v1/labs/{namespace}/{pod}/validate`: Valida uma tarefa específica
- `/ws/terminal/{namespace}/{pod}`: Endpoint WebSocket para o terminal interativo

## Como buildar a imagem

Para buildar a imagem serão necessários os seguintes pré-requisitos:

- Docker

Siga o seguinte passo-a-passo:

1. Clone o repositório

```bash
git clone https://github.com/EduardoThums-Girus-PICK/backend.git girus-backend
cd girus-backend
```

2. Execute o comando `docker image build` para buildar a imagem no seu local

```bash
docker image build -t girus-backend .
```

3. Verifique que a imagem foi criada com o comando `inspect`

```bash
docker image inspect girus-backend
```

## Como executar a imagem

A imagem foi criada com o intuito de ser executada dentro de um ecossistema Kubernetes, caso tente executala como um container docker através do comando `docker container run` encontrará o seguinte erro:

```bash
2025/05/31 21:58:37 Iniciando o Girus Server v0.1
2025/05/31 21:58:37 Erro ao carregar kubeconfig: stat /home/nonroot/.kube/config: no such file or directory
2025/05/31 21:58:37 Erro ao criar gerenciador de laboratórios: falha ao obter configuração do cluster: stat /home/nonroot/.kube/config: no such file or directory
```

**Importante**: o pod necessita de permissões de `cluster-admin` para ser possivel rodar de forma correta.

Siga o passo-a-passo para executar a imagem utilizando o comando `kubectl apply`.

1. Aplique o manifesto que cria o `ServiceAccount`, o `ClusterRoleBinding` e o `Pod` 
```bash
cat <<EOF | kubectl apply -f -
apiVersion: v1
kind: ServiceAccount
metadata:
  name: girus-sa
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: girus-cluster-rolebinding
subjects:
  - kind: ServiceAccount
    name: girus-sa
    namespace: default
roleRef:
  kind: ClusterRole
  name: cluster-admin
  apiGroup: rbac.authorization.k8s.io
---
apiVersion: v1
kind: Pod
metadata:
  name: girus-backend
  labels:
    app: girus-backend
    app.kubernetes.io/part-of: girus
spec:
  serviceAccountName: girus-sa
  containers:
  - name: backend
    image: eduardothums/girus:backend-v1.0.1
    env:
    - name: PORT
      value: "8080"
    - name: GIN_MODE
      value: "release"
    ports:
      - containerPort: 8080
---
apiVersion: v1
kind: Service
metadata:
  name: girus-backend
spec:
  selector:
    app: girus-backend
  ports:
    - port: 8080
  type: ClusterIP
EOF
```

2. Aguarde até que o pod tenha inicializado

```bash
kubectl wait pod --all --for=condition=Ready -l app.kubernetes.io/part-of=girus --timeout 60s
```

3. Inspecione os logs do pod

```bash
kubectl logs girus-backend
```

4. Faça um port-foward para ser possivel chamar a API através do localhost

```bash
kubectl port-forward services/girus-backend 8080:8080
```

5. Em outro terminal chame a API no endpoint de healthcheck

```bash
curl http://localhost:8080/api/v1/health
```

## Como verificar a sua assinatura

Para verificar a autenticidade da imagem é possível utilizar o programa `cosign` utilizando a parte pública da chave utilizada para assinar a imagem.

```bash
cosign verify --key https://raw.githubusercontent.com/EduardoThums-Girus-PICK/cosign-pub-key/refs/heads/main/cosign.pub eduardothums/girus:backend-v1.0.0
```
