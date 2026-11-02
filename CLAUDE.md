# CLAUDE.md — MailHog-GM

Fork de [MailHog](https://github.com/mailhog/MailHog). Objetivo: servir de **substituto drop-in do Gmail em ambientes de teste** — uma app configurada para enviar via Gmail deve conseguir apontar para o MailHog-GM sem mudar o modo de autenticação, e uma API deve permitir buscar os emails recebidos (mais novo primeiro).

## Objetivos do fork

1. **Autenticação SMTP nível Gmail** — aceitar os mesmos mecanismos que o Gmail exige, para substituir `smtp.gmail.com` em testes sem reconfigurar o cliente.
2. **API de recebimento/busca** — expor emails capturados via HTTP, sempre **ordenados do mais novo para o mais antigo**.
3. **README** — creditar o MailHog original e descrever as alterações do fork.

## Arquitetura (onde está o quê)

O núcleo NÃO está na raiz — está vendorizado em `vendor/github.com/mailhog/*` (vendoring estilo GOPATH, sem `go.mod`):

- `MailHog-Server/smtp/session.go` — sessão SMTP; `validateAuthentication` (linha ~70) hoje **sempre aceita**; anuncia só `PLAIN` (`session.go:57`).
- `smtp/protocol.go` — máquina de estados SMTP; suporta estados `AUTHPLAIN` / `AUTHLOGIN` / `AUTHLOGIN2`.
- `MailHog-Server/api/v1.go` e `v2.go` — endpoints REST. `/api/v1/messages` e `/api/v2/messages` retornam `Storage.List`.
- `storage/{memory,mongodb,maildir}.go` — backends. `InMemory.List` já devolve mais novo primeiro; `MongoDB` usa `Sort("-created")`; **`Maildir` usa ModTime, verificar ordem**.
- `http/server.go` — HTTP Basic Auth do UI/API (`AuthFile`, `BasicAuthHandler`).
- `main.go` (raiz) — wiring: flags, listeners HTTP e SMTP.

> Editar código vendorizado exige estratégia consciente: alterar in-place no `vendor/` (fork real dos submódulos) e commitar. Não há `go.mod` para `replace`.

## Diretiva 1 — Autenticação SMTP nível Gmail

Estado atual: `validateAuthentication` retorna `ok` para qualquer credencial; sem TLS obrigatório.

Alvo:
- Anunciar mecanismos compatíveis com clientes Gmail: **`PLAIN`, `LOGIN`** e idealmente **`XOAUTH2`** (`GetAuthenticationMechanismsHandler`, `session.go:57`).
- Suportar **STARTTLS** (587) e **SMTPS/TLS implícito** (465), pois clientes Gmail exigem canal cifrado antes do AUTH. Usar cert self-signed configurável.
- `validateAuthentication`: validar contra credenciais configuráveis (flag/env/arquivo). Modo default de teste = aceitar qualquer (compatibilidade), modo estrito = checar credencial.
- Novas flags de config em `MailHog-Server/config` e wiring em `main.go`.

## Diretiva 2 — API: mais novo primeiro

- Garantir que **todos** os endpoints de listagem/busca retornem ordenado por `Created` desc.
- `InMemory` já OK; conferir `Maildir.List`; `MongoDB` já `-created`.
- Testar `/api/v1/messages`, `/api/v2/messages`, `/api/v2/search`.

## Diretiva 3 — README

- Manter crédito claro ao MailHog original e ao MailCatcher.
- Documentar: propósito do fork, novas flags de auth SMTP, garantia de ordenação da API.

## Status (implementado)

- **Auth SMTP**: `session.go` anuncia `PLAIN LOGIN XOAUTH2`, oferece STARTTLS (cert self-signed auto em `config/tls.go`), valida credenciais em modo estrito (`-smtp-auth-file`) ou aceita qualquer (`-smtp-auth-allow-any`, padrão). XOAUTH2 adicionado em `smtp/protocol.go`. Listener SMTPS opcional (`-smtps-bind-addr`, `smtp.ListenTLS`).
- **API mais novo primeiro**: `api.go:sortMessagesDesc` aplicado em v1 `messages`, v2 `messages` e `search`.
- **Flags novas** (todas com env `MH_*`): `smtp-auth-allow-any`, `smtp-auth-file`, `smtp-require-tls`, `smtp-tls-cert`, `smtp-tls-key`, `smtps-bind-addr`, `smtp-allowed-domains`.
- **Filtro de domínio**: `session.go:validateRecipient` recusa (550) destinatários fora de `-smtp-allowed-domains` (padrão `pildra.com.br`; vazio desabilita) e `acceptMessage` descarta DATA sem destinatário aceito.
- **Pendente**: compilar/testar com toolchain Go (não instalado neste ambiente).

## UI (GMail Placebo)

A UI web foi renomeada para **"GMail Placebo"** com um tema elegante (acento estilo Gmail). Os assets são embutidos via **go-bindata** em `vendor/github.com/mailhog/MailHog-UI/assets/assets.go`. As fontes editáveis ficam em `_uisrc/assets/` (templates `layout.html`/`index.html` e `css/style.css`).

Regenerar o bindata após editar `_uisrc/` (precisa de Docker):

```bash
docker run --rm -v "${PWD}:/go/src/github.com/mailhog/MailHog" \
  -w /go/src/github.com/mailhog/MailHog golang:1.20-alpine sh -c \
  "apk add --no-cache git >/dev/null; export GO111MODULE=on GOFLAGS=-mod=mod; \
   go install github.com/kevinburke/go-bindata/v4/go-bindata@latest; \
   cd _uisrc && /go/bin/go-bindata -pkg assets \
   -o ../vendor/github.com/mailhog/MailHog-UI/assets/assets.go assets/..."
```

## Regras de trabalho

- Preservar compatibilidade com API/UI existentes do MailHog.
- Alterações no `vendor/` são intencionais e versionadas neste fork.
- Idioma: comentários e docs em pt-BR; identificadores em inglês.
