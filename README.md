TODO

## Como verificar a sua assinatura

Para verificar a autenticidade da imagem é possível utilizar o programa `cosign` utilizando a parte pública da chave utilizada para assinar a imagem.

```bash
cosign verify --key https://raw.githubusercontent.com/EduardoThums-Girus-PICK/cosign-pub-key/refs/heads/main/cosign.pub eduardothums/girus:backend-v1.0.0
```
