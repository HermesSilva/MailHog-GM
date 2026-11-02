# Changelog

Todas as mudanças relevantes deste fork (**MailHog-GM**) sobre o
[MailHog](https://github.com/mailhog/MailHog) original são documentadas aqui.

O formato segue [Keep a Changelog](https://keepachangelog.com/pt-BR/1.0.0/) e o
projeto adota [Versionamento Semântico](https://semver.org/lang/pt-BR/).

> MailHog-GM é um trabalho derivado do MailHog (criado por Ian Kent, licença
> MIT). Os direitos do software base pertencem aos autores originais; veja
> `LICENSE.md`.

## [Unreleased]

### Added
- **Filtro de domínio de destinatário**: `-smtp-allowed-domains`
  (`MH_SMTP_ALLOWED_DOMAINS`, padrão `pildra.com.br`). Destinatários de outros
  domínios são recusados com `550` e a mensagem não é armazenada.

### Changed
- **Mudança de comportamento:** destinatários fora de `pildra.com.br` passam a ser
  recusados por padrão. Use `MH_SMTP_ALLOWED_DOMAINS=` (vazio) para aceitar todos.
- **CI/CD:** publicar a imagem agora aciona o deploy em `Tootega/InfraVPS`. Antes,
  a imagem nova só chegava ao servidor com um disparo manual naquele repositório.

## [1.0.0-gm] - 2026-07-04

Primeira versão do fork, adaptando o MailHog para atuar como **substituto do
Gmail em ambientes de teste** ("placebo do Gmail").

### Added
- **Autenticação SMTP nível Gmail**: `STARTTLS` no servidor SMTP com certificado
  TLS self-signed gerado em memória (`config/tls.go`), listener SMTPS opcional
  (TLS implícito) via `smtp.ListenTLS` e flag `-smtps-bind-addr`.
- Mecanismos de AUTH `PLAIN`, `LOGIN` e `XOAUTH2` anunciados no EHLO; suporte a
  `XOAUTH2` adicionado à biblioteca SMTP (`smtp/protocol.go`).
- Modos de autenticação: **placebo** (aceita qualquer credencial, padrão) e
  **estrito** via arquivo `user:password` (`-smtp-auth-file`).
- Novas flags/variáveis de ambiente: `-smtp-auth-allow-any`
  (`MH_SMTP_AUTH_ALLOW_ANY`), `-smtp-auth-file` (`MH_SMTP_AUTH_FILE`),
  `-smtp-require-tls` (`MH_SMTP_REQUIRE_TLS`), `-smtp-tls-cert`
  (`MH_SMTP_TLS_CERT`), `-smtp-tls-key` (`MH_SMTP_TLS_KEY`), `-smtps-bind-addr`
  (`MH_SMTPS_BIND_ADDR`).
- Projeto de testes de integração C# .NET (`test/MailHogGM.Tests`) cobrindo
  confirmação de e-mail, token de reset de senha e ordenação — envia via SMTP
  (STARTTLS + AUTH) e lê pela API HTTP.
- `docker-compose.yml` que sobe o MailHog-GM e roda os testes de integração num
  único comando.
- Pipeline de CI/CD (`.github/workflows/cd-email.yml`): testes como gate, build
  e push da imagem no GHCR e deploy no Docker Swarm.

### Changed
- **API retorna o e-mail mais novo primeiro**: `GET /api/v1/messages`,
  `GET /api/v2/messages` e `GET /api/v2/search` passam a ordenar por `Created`
  decrescente em qualquer backend (`memory`/`mongodb`/`maildir`).
- `Dockerfile` reescrito para compilar o fork a partir do **código-fonte local**
  (GOPATH + `vendor/`), em vez de baixar o MailHog upstream via `go install`.
- `Deploy/stack/e-mail.yaml` passa a usar a imagem do fork
  (`ghcr.io/hermessilva/mailhog-gm`), adiciona a porta de submissão `587`
  (STARTTLS estilo Gmail) e mantém as portas `25`/`2525` e o armazenamento
  `maildir` existentes.

### Preserved
- Licença MIT e copyright original de Ian Kent (`LICENSE.md`).
- Compatibilidade com a API e a UI existentes do MailHog.
