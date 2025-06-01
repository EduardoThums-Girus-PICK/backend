
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

## Sobre a construção da imagem

Abaixo está o arquivo `Dockerfile` utilizado para buildar a imagem, foi utilizado a técnica de multi-stage build para otimizar o tamanho final das layers, abaixo será explicado o funcionamento de cada comando agrupados por stage.

```Dockerfile
# go:1.24.3
FROM cgr.dev/chainguard/go:latest@sha256:86afb531f453caf27580a0c7a11ac7f6c423cc1599a7ef53645e7353353ae302 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o healthcheck ./healthcheck
# hadolint ignore=DL3059
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./server

FROM cgr.dev/chainguard/static:latest@sha256:633aabd19a2d1b9d4ccc1f4b704eb5e9d34ce6ad231a4f5b7f7a3af1307fdba8

ARG revision

LABEL \
  org.opencontainers.image.title="Girus Backend" \
  org.opencontainers.image.description="Backend for the Girus application" \
  org.opencontainers.image.authors="Eduardo Thums <eduardocristiano01@gmail.com>" \
  org.opencontainers.image.licenses="MIT" \
  org.opencontainers.image.version="1.0.0" \
  org.opencontainers.image.url="https://linuxtips.io/girus-labs/" \
  org.opencontainers.image.source="https://github.com/eduardothums/girus-pick" \
  org.opencontainers.image.documentation="https://github.com/eduardothums/girus-pick/README.md" \
  org.opencontainers.image.revision="$revision"

COPY --from=builder /app/server/server /app/healthcheck/healthcheck /usr/bin/

ENV PORT=8080
ENV GIN_MODE=release

HEALTHCHECK --interval=30s --timeout=10s --start-period=2s --retries=5 CMD ["/usr/bin/healthcheck"]
EXPOSE $PORT

ENTRYPOINT ["/usr/bin/server"]
```

### Stage de build

```Dockerfile
# go:1.24.3
FROM cgr.dev/chainguard/go:latest@sha256:86afb531f453caf27580a0c7a11ac7f6c423cc1599a7ef53645e7353353ae302 AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o healthcheck ./healthcheck
# hadolint ignore=DL3059
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o server ./server
```

1. O comando `FROM` define a imagem base do stage `builder`, utilizamos a imagem do time da chainguard por não conter vulnerabilidades e ser mais segura de uma forma geral, além disso é utilizado o sha256 digest do repositório para garantir a integradade da versão caso haja uma atualização forçada.

2. Os comandos `COPY go.mod go.sum ./` e `RUN go mod download` copiam os arquivos necessários e fazem o download das dependências do projeto, dessa forma caso haja alguma mudança no nosso código fonte, não será preciso re-buildar a layer novamente, otimizando o tempo de build e armazenamento das layers.

3. O comando `COPY . .` copia tudo do contexto de build atual para dentro da imagem, isso é possivel pois o nosso arquivo `.dockerignore` ignora tudo por padrão e libera apenas a cópia de arquivos especificos, precavendo a adição de arquivos futuros que não deveriam ir para a imagem.

4. Os comandos `RUN ... go build` compilam o código goland do servidor e do script de healthcheck para um binário.

### Stage final

```Dockerfile
FROM cgr.dev/chainguard/static:latest@sha256:633aabd19a2d1b9d4ccc1f4b704eb5e9d34ce6ad231a4f5b7f7a3af1307fdba8

ARG revision
ARG version

LABEL \
  org.opencontainers.image.title="Girus Backend" \
  org.opencontainers.image.description="Backend for the Girus application" \
  org.opencontainers.image.authors="Eduardo Thums <eduardocristiano01@gmail.com>" \
  org.opencontainers.image.licenses="MIT" \
  org.opencontainers.image.version="$version" \
  org.opencontainers.image.url="https://linuxtips.io/girus-labs/" \
  org.opencontainers.image.source="https://github.com/EduardoThums-Girus-PICK/backend" \
  org.opencontainers.image.documentation="https://github.com/EduardoThums-Girus-PICK/backend/README.md" \
  org.opencontainers.image.revision="$revision"

COPY --from=builder /app/server/server /app/healthcheck/healthcheck /usr/bin/

ENV PORT=8080
ENV GIN_MODE=release

HEALTHCHECK --interval=30s --timeout=10s --start-period=2s --retries=5 CMD ["/usr/bin/healthcheck"]
EXPOSE $PORT

ENTRYPOINT ["/usr/bin/server"]
```

1. A imagem base utilizada é a `static` da chainguard, pois além de não conter vulnerabilidades, ela é perfeita para casos onde não é preciso nenhum outro programa instalado na imagem apenas um binário que foi pré-compilado nos stages anteriores.

2. Utilizamos o `ARG revision` para passar por argumento no momento do build da imagem o sha256 do commit para ser adicionado nas labels.

3. No `LABEL` adicionamos diversas labels com informações relevantes da imagem, como versão, autores, documentação, endereço do código fonte etc.

4. Copiamos os conteudos do stage de build com o `COPY` trazendo para a imagem apenas os binários compilados do golang.

5. Adicionamos um healthcheck inbutido na imagem com o comando `HEALTHCHECK`

6. Definimos o binário `/usr/bin/server` com o `ENTRYPOINT` da imagem


## Fluxo do CI/CD

Utilizamos o GitHub Actions como plataforma de CI/CD do projeto, onde é realizado validações de segurança, boas práticas, build de imagems e publicações de releases através de tags do git.

Existem dois momentos onde os workflows definidos em `./github/workflows` são disparados:

1. `security_check.yaml`: quando há algum pull request aberto com a branch target apontando para a `main`
2. `release.yaml`: quando uma tag é criada no repositório

### security_check.yaml

Este workflow tem como objetivo:

1. Aplicar validações de segurança no código afim de encontrar vulnerabilidades de segurança nas dependências através da ferramenta [Trivy](https://trivy.dev/latest/)

2. Aplicar validações de segurança no build da imagem, afim de encontrar vulnerabilidades de segurança imagens base através da ferramenta [Trivy](https://trivy.dev/latest/)

3. Aplicar validações de boas práticas de criação de imagens com a ajuda do [Hadolint](https://github.com/hadolint/hadolint)

### release.yaml

Este workflow tem como objetivo:

1. Aplicar todas as etapas realizadas no workflow `security_check.yaml` para garantir que nenhuma vulnerabilidade veio a surgir entre o tempo de merge do pull request e a geração da tag

2. Buildar a imagem com a tag apontando para a tag do git

3. Fazer o push da imagem para o repositório no docker hub

4. Assinar a imagem utilizando o [Cosign](https://docs.sigstore.dev/cosign/)

5. Criar uma release com base na tag do git
