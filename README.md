MailHog-GM
==========

> **Fork de [MailHog](https://github.com/mailhog/MailHog)**, originalmente criado por Ian Kent e colaboradores, inspirado no [MailCatcher](https://mailcatcher.me/). Todo o crédito do projeto base pertence aos autores do MailHog. Veja o README original em https://github.com/mailhog/MailHog.
>
> Este fork adapta o MailHog para atuar como **substituto do Gmail em ambientes de teste**:
>
> * **Autenticação SMTP nível Gmail** — aceita os mesmos mecanismos de auth (PLAIN/LOGIN/XOAUTH2 sobre STARTTLS/TLS) que clientes configurados para `smtp.gmail.com` esperam, permitindo redirecionar o envio para o MailHog-GM sem reconfigurar o cliente.
> * **API de recebimento/busca de emails** — os endpoints de listagem retornam sempre o **email mais novo primeiro**.
>
> As diretivas de desenvolvimento deste fork estão em [`CLAUDE.md`](CLAUDE.md).

## Intenção do projeto

O MailHog-GM existe para **substituir o Gmail (`smtp.gmail.com`) em ambientes de teste**, sem que a aplicação sob teste precise mudar de configuração. A ideia é ser um "placebo do Gmail": a mesma aplicação que em produção envia via Gmail aponta, em teste, para o MailHog-GM — que aceita a conexão com o mesmo nível de autenticação (STARTTLS/SMTPS + AUTH), captura os e-mails em vez de entregá-los, e os expõe por uma API HTTP para inspeção automatizada (mais novo primeiro).

O que foi feito neste fork, sobre o MailHog original:

1. **Autenticação SMTP nível Gmail** — `STARTTLS`/SMTPS com certificado self-signed gerado em memória, `AUTH PLAIN/LOGIN/XOAUTH2`, validação de credenciais opcional (modo estrito) ou aceitação de qualquer credencial (modo placebo, padrão).
2. **API ordenada (mais novo primeiro)** — todos os endpoints de listagem retornam por `Created` decrescente, em qualquer backend.
3. **Testes de integração ponta a ponta** — projeto C# .NET que envia e lê e-mails reais (confirmação de e-mail, token de reset de senha, ordenação).
4. **Empacotamento e entrega** — `Dockerfile` que compila o fork do código-fonte local, `docker compose` que sobe o serviço e roda os testes, e um pipeline de CI/CD que publica a imagem e faz deploy no Swarm, substituindo a stack de e-mail anterior.

### Recursos do fork (MailHog-GM)

#### Autenticação SMTP nível Gmail

O servidor SMTP anuncia `STARTTLS` e os mecanismos `AUTH PLAIN LOGIN XOAUTH2`, como o Gmail. Um certificado TLS **self-signed é gerado automaticamente em memória**, então `STARTTLS`/SMTPS funcionam sem configuração.

Flags / variáveis de ambiente:

| Flag | Env | Padrão | Descrição |
|------|-----|--------|-----------|
| `-smtp-auth-allow-any` | `MH_SMTP_AUTH_ALLOW_ANY` | `true` | Aceita **qualquer** credencial (modo placebo). Ignorado se `-smtp-auth-file` for usado. |
| `-smtp-auth-file` | `MH_SMTP_AUTH_FILE` | | Arquivo `user:password` por linha → modo estrito. |
| `-smtp-require-tls` | `MH_SMTP_REQUIRE_TLS` | `false` | Exige `STARTTLS` antes de `AUTH`/`MAIL`. |
| `-smtp-tls-cert` | `MH_SMTP_TLS_CERT` | | Certificado PEM (opcional; self-signed se vazio). |
| `-smtp-tls-key` | `MH_SMTP_TLS_KEY` | | Chave PEM (opcional). |
| `-smtps-bind-addr` | `MH_SMTPS_BIND_ADDR` | | Liga listener SMTPS (TLS implícito, ex.: `0.0.0.0:1465`). Vazio desabilita. |

> **Certificado self-signed:** clientes que validam a cadeia (ex.: `System.Net.Mail.SmtpClient` do .NET) rejeitam o cert gerado. Em teste, ou forneça um cert confiável via `-smtp-tls-cert`/`-smtp-tls-key`, ou desabilite a validação no cliente (ex.: `ServicePointManager.ServerCertificateValidationCallback = (s,c,ch,e) => true;`).

#### Filtro de domínio de destinatário

Por padrão, só são aceitas mensagens para destinatários (`RCPT TO`) de `pildra.com.br`. Os demais são recusados com `550` durante a sessão SMTP e nada é armazenado, portanto a mensagem não chega à API nem à UI. O domínio do remetente não é avaliado.

| Flag | Env | Padrão | Descrição |
|------|-----|--------|-----------|
| `-smtp-allowed-domains` | `MH_SMTP_ALLOWED_DOMAINS` | `pildra.com.br` | Domínios aceitos, separados por vírgula. Vazio desabilita o filtro. |

* A comparação ignora maiúsculas e exige o domínio exato: `sub.pildra.com.br` só é aceito se for listado.
* Em uma mensagem com vários destinatários, apenas os de fora são recusados. A mensagem é armazenada para os aceitos e os cabeçalhos `To`/`Cc` originais são preservados.
* O filtro vale para mensagens novas: as que já estão armazenadas não são removidas.

#### API — mais novo primeiro

Todos os endpoints de listagem retornam ordenados por `Created` decrescente (mais novo primeiro), em qualquer backend (`memory`/`mongodb`/`maildir`):

* `GET /api/v1/messages`
* `GET /api/v2/messages`
* `GET /api/v2/search?kind=...&query=...`

#### Testes (Docker)

Um projeto C# .NET (`test/MailHogGM.Tests`) envia e-mails de exemplo via SMTP (STARTTLS + AUTH) e os lê de volta pela API. Cobre: **confirmação de e-mail**, **token de reset de senha** e a **ordenação mais-novo-primeiro**.

Compilar o fork e rodar os testes num único comando:

```bash
docker compose up --build --abort-on-container-exit --exit-code-from tests
```

O `Dockerfile` compila o MailHog-GM a partir do código-fonte local (GOPATH + `vendor/`); o serviço `tests` executa `dotnet test` contra o container. Saída `0` = verde.

#### Deploy (CI/CD)

O workflow [`.github/workflows/cd-email.yml`](.github/workflows/cd-email.yml) roda os testes de integração como gate, publica a imagem no GHCR e faz o `docker stack deploy` no Swarm via SSH, com a tag do commit. A stack está em [`Deploy/stack/e-mail.yaml`](Deploy/stack/e-mail.yaml), expondo SMTP nas portas `25`/`2525` e submissão STARTTLS na `587`.

O deploy exige os secrets `STAGING_HOST`, `STAGING_USER`, `STAGING_SSH_KEY` e `STAGING_SSH_PASSPHRASE` neste repositório. Como o repositório é público, os logs do Actions também são: não imprima `docker service logs` no workflow, porque eles contêm endereços de e-mail.

### Créditos e licença

Este é um trabalho derivado (fork) do **MailHog**, criado por **Ian Kent** e colaboradores, licenciado sob **MIT**. O aviso de copyright e a licença originais são preservados em [`LICENSE.md`](LICENSE.md) (`Copyright (c) 2014 - 2016 Ian Kent`). Todos os direitos do software base pertencem aos seus autores originais; as alterações deste fork são distribuídas sob a mesma licença MIT. Veja o projeto original em https://github.com/mailhog/MailHog.

---

## MailHog (original)

Inspired by [MailCatcher](https://mailcatcher.me/), easier to install.

* Download and run MailHog
* Configure your outgoing SMTP server
* View your outgoing email in a web UI
* Release it to a real mail server

Built with Go - MailHog runs without installation on multiple platforms.

### Overview

MailHog is an email testing tool for developers:

* Configure your application to use MailHog for SMTP delivery
* View messages in the web UI, or retrieve them with the JSON API
* Optionally release messages to real SMTP servers for delivery

### Installation

#### Manual installation
[Download the latest release for your platform](/docs/RELEASES.md). Then
[read the deployment guide](/docs/DEPLOY.md) for deployment options.

#### MacOS
```bash
brew update && brew install mailhog
```

Then, start MailHog by running `mailhog` in the command line.

#### Debian / Ubuntu Go < v1.18
```bash
sudo apt-get -y install golang-go
go get github.com/mailhog/MailHog
```

#### Go >= v1.17 (Debian Bookworm) 
```bash
sudo apt-get -y install golang-go
go install github.com/mailhog/MailHog@latest
```

Then, start MailHog by running `/path/to/MailHog` in the command line.

E.g. the path to Go's bin files on Ubuntu is `~/go/bin/`, so to start the MailHog run:

```bash
~/go/bin/MailHog
```

#### FreeBSD
```bash
pkg install mailhog
sysrc mailhog_enable="YES"
service mailhog start
```

#### Docker
[Run it from Docker Hub](https://registry.hub.docker.com/r/mailhog/mailhog/) or using the provided [Dockerfile](Dockerfile)

### Configuration

Check out how to [configure MailHog](/docs/CONFIG.md), or use the default settings:
  * the SMTP server starts on port 1025
  * the HTTP server starts on port 8025
  * in-memory message storage

### Features

See [MailHog libraries](docs/LIBRARIES.md) for a list of MailHog client libraries.

* ESMTP server implementing RFC5321
* Support for SMTP AUTH (RFC4954) and PIPELINING (RFC2920)
* Web interface to view messages (plain text, HTML or source)
  * Supports RFC2047 encoded headers
* Real-time updates using EventSource
* Release messages to real SMTP servers
* Chaos Monkey for failure testing
  * See [Introduction to Jim](/docs/JIM.md) for more information
* HTTP API to list, retrieve and delete messages
  * See [APIv1](/docs/APIv1.md) and [APIv2](/docs/APIv2.md) documentation for more information
* [HTTP basic authentication](docs/Auth.md) for MailHog UI and API
* Multipart MIME support
* Download individual MIME parts
* In-memory message storage
* MongoDB and file based storage for message persistence
* Lightweight and portable
* No installation required

#### sendmail

[mhsendmail](https://github.com/mailhog/mhsendmail) is a sendmail replacement for MailHog.

It redirects mail to MailHog using SMTP.

You can also use `MailHog sendmail ...` instead of the separate mhsendmail binary.

Alternatively, you can use your native `sendmail` command by providing `-S`, for example:

```bash
/usr/sbin/sendmail -S mail:1025
```

For example, in PHP you could add either of these lines to `php.ini`:

```
sendmail_path = /usr/local/bin/mhsendmail
sendmail_path = /usr/sbin/sendmail -S mail:1025
```

#### Web UI

![Screenshot of MailHog web interface](/docs/MailHog.png "MailHog web interface")

### Contributing

MailHog is a rewritten version of [MailHog](https://github.com/ian-kent/MailHog), which was born out of [M3MTA](https://github.com/ian-kent/M3MTA).

Clone this repository to ```$GOPATH/src/github.com/mailhog/MailHog``` and type ```make deps```.

See the [Building MailHog](/docs/BUILD.md) guide.

Requires Go 1.4+ to build.

Run tests using ```make test``` or ```goconvey```.

If you make any changes, run ```go fmt ./...``` before submitting a pull request.

### Licence

Copyright ©‎ 2014 - 2017, Ian Kent (http://iankent.uk)

Released under MIT license, see [LICENSE](LICENSE.md) for details.
