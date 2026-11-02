package smtp

// http://www.rfc-editor.org/rfc/rfc5321.txt

import (
	"crypto/tls"
	"encoding/base64"
	"errors"
	"io"
	"log"
	"net"
	"strings"

	"github.com/ian-kent/linkio"
	"github.com/mailhog/MailHog-Server/config"
	"github.com/mailhog/MailHog-Server/monkey"
	"github.com/mailhog/data"
	"github.com/mailhog/smtp"
	"github.com/mailhog/storage"
)

// errInvalidAuth é retornado quando as credenciais SMTP não conferem no modo
// estrito (arquivo de auth configurado).
var errInvalidAuth = errors.New("Authentication credentials invalid")

// errNoAllowedRecipient é retornado quando chega DATA sem nenhum destinatário
// aceito pelo filtro de domínios: o protocolo não impede DATA nesse estado.
var errNoAllowedRecipient = errors.New("No allowed recipient")

// Session represents a SMTP session using net.TCPConn
type Session struct {
	conn          io.ReadWriteCloser
	proto         *smtp.Protocol
	storage       storage.Storage
	messageChan   chan *data.Message
	remoteAddress string
	isTLS         bool
	line          string
	link          *linkio.Link

	reader io.Reader
	writer io.Writer
	monkey monkey.ChaosMonkey

	config *config.Config
}

// Accept starts a new SMTP session using io.ReadWriteCloser
func Accept(remoteAddress string, conn io.ReadWriteCloser, storage storage.Storage, messageChan chan *data.Message, hostname string, monkey monkey.ChaosMonkey, cfg *config.Config) {
	accept(remoteAddress, conn, storage, messageChan, hostname, monkey, cfg, false)
}

// AcceptTLS starts a new SMTP session on a connection já criptografada
// (SMTPS / TLS implícito, ex.: porta 465). O AUTH é liberado imediatamente.
func AcceptTLS(remoteAddress string, conn io.ReadWriteCloser, storage storage.Storage, messageChan chan *data.Message, hostname string, monkey monkey.ChaosMonkey, cfg *config.Config) {
	accept(remoteAddress, conn, storage, messageChan, hostname, monkey, cfg, true)
}

func accept(remoteAddress string, conn io.ReadWriteCloser, storage storage.Storage, messageChan chan *data.Message, hostname string, monkey monkey.ChaosMonkey, cfg *config.Config, alreadyTLS bool) {
	defer conn.Close()

	proto := smtp.NewProtocol()
	proto.Hostname = hostname
	var link *linkio.Link
	reader := io.Reader(conn)
	writer := io.Writer(conn)
	if monkey != nil {
		linkSpeed := monkey.LinkSpeed()
		if linkSpeed != nil {
			link = linkio.NewLink(*linkSpeed * linkio.BytePerSecond)
			reader = link.NewLinkReader(io.Reader(conn))
			writer = link.NewLinkWriter(io.Writer(conn))
		}
	}

	session := &Session{conn, proto, storage, messageChan, remoteAddress, alreadyTLS, "", link, reader, writer, monkey, cfg}
	proto.LogHandler = session.logf
	proto.MessageReceivedHandler = session.acceptMessage
	proto.ValidateSenderHandler = session.validateSender
	proto.ValidateRecipientHandler = session.validateRecipient
	proto.ValidateAuthenticationHandler = session.validateAuthentication
	proto.GetAuthenticationMechanismsHandler = session.authMechanisms

	if alreadyTLS {
		// Conexão SMTPS: já está criptografada, marca o protocolo como TLS
		// para liberar AUTH imediatamente sem exigir STARTTLS.
		proto.TLSUpgraded = true
	} else if cfg != nil && cfg.TLSConfig != nil {
		// Autenticação nível Gmail: oferece STARTTLS e, opcionalmente, exige
		// TLS antes de AUTH/MAIL.
		proto.TLSHandler = session.tlsHandler
		proto.RequireTLS = cfg.SMTPRequireTLS
	}

	session.logf("Starting session")
	session.Write(proto.Start())
	for session.Read() == true {
		if monkey != nil && monkey.Disconnect != nil && monkey.Disconnect() {
			session.conn.Close()
			break
		}
	}
	session.logf("Session ended")
}

// authMechanisms anuncia os mecanismos de AUTH suportados, espelhando o Gmail
// (PLAIN e LOGIN sobre TLS, além de XOAUTH2). CRAM-MD5 não é anunciado por não
// ser verificável no modo estrito e não ser oferecido pelo Gmail.
func (c *Session) authMechanisms() []string {
	return []string{"PLAIN", "LOGIN", "XOAUTH2"}
}

// tlsHandler faz o upgrade da conexão para TLS quando o cliente envia STARTTLS.
func (c *Session) tlsHandler(done func(ok bool)) (errorReply *smtp.Reply, callback func(), ok bool) {
	c.logf("Got STARTTLS request")

	netConn, isNet := c.conn.(net.Conn)
	if !isNet || c.config == nil || c.config.TLSConfig == nil {
		c.logf("TLS not available for this connection")
		return smtp.ReplyUnrecognisedCommand(), nil, false
	}

	return nil, func() {
		c.logf("Upgrading connection to TLS")
		tConn := tls.Server(netConn, c.config.TLSConfig)
		if err := tConn.Handshake(); err != nil {
			c.logf("TLS handshake failed: %s", err)
			done(false)
			return
		}
		c.conn = tConn
		c.reader = tConn
		c.writer = tConn
		c.isTLS = true
		done(true)
	}, true
}

// validateAuthentication valida as credenciais recebidas. Em modo placebo
// (padrão) qualquer credencial é aceita, permitindo substituir o Gmail sem
// conhecer a senha real. Em modo estrito (arquivo de auth), valida user/senha.
func (c *Session) validateAuthentication(mechanism string, args ...string) (errorReply *smtp.Reply, ok bool) {
	if c.monkey != nil {
		if !c.monkey.ValidAUTH(mechanism, args...) {
			// FIXME better error?
			return smtp.ReplyUnrecognisedCommand(), false
		}
	}

	if c.config == nil || c.config.SMTPAuthAllowAny {
		return nil, true
	}

	user, pass, decoded := decodeCredentials(mechanism, args...)
	if !decoded {
		return smtp.ReplyError(errInvalidAuth), false
	}

	if expected, exists := c.config.AuthCredentials[user]; exists && expected == pass {
		return nil, true
	}

	c.logf("Authentication failed for user %q", user)
	return smtp.ReplyError(errInvalidAuth), false
}

// decodeCredentials extrai user/password dos argumentos de AUTH conforme o
// mecanismo. Retorna decoded=false para mecanismos sem senha em texto
// verificável (ex.: CRAM-MD5), que só são aceitos em modo placebo.
func decodeCredentials(mechanism string, args ...string) (user, pass string, decoded bool) {
	switch strings.ToUpper(mechanism) {
	case "PLAIN":
		// args = [user, pass] (já separados pela lib)
		if len(args) >= 2 {
			return args[0], args[1], true
		}
	case "LOGIN":
		// args = [base64(user), base64(pass)]
		if len(args) >= 2 {
			u, e1 := base64.StdEncoding.DecodeString(args[0])
			p, e2 := base64.StdEncoding.DecodeString(args[1])
			if e1 == nil && e2 == nil {
				return string(u), string(p), true
			}
		}
	case "XOAUTH2":
		// args[0] = base64("user=<email>\x01auth=Bearer <token>\x01\x01")
		if len(args) >= 1 {
			raw, err := base64.StdEncoding.DecodeString(args[0])
			if err == nil {
				for _, part := range strings.Split(string(raw), "\x01") {
					if strings.HasPrefix(part, "user=") {
						return strings.TrimPrefix(part, "user="), "", true
					}
				}
			}
		}
	}
	return "", "", false
}

func (c *Session) validateRecipient(to string) bool {
	if c.monkey != nil {
		ok := c.monkey.ValidRCPT(to)
		if !ok {
			return false
		}
	}
	if c.config != nil && !c.config.RecipientAllowed(to) {
		c.logf("Rejecting recipient %q: domain not allowed", to)
		return false
	}
	return true
}

func (c *Session) validateSender(from string) bool {
	if c.monkey != nil {
		ok := c.monkey.ValidMAIL(from)
		if !ok {
			return false
		}
	}
	return true
}

func (c *Session) acceptMessage(msg *data.SMTPMessage) (id string, err error) {
	if c.config != nil && c.config.FiltersRecipients() && len(msg.To) == 0 {
		c.logf("Discarding message: no allowed recipient")
		return "", errNoAllowedRecipient
	}

	m := msg.Parse(c.proto.Hostname)
	c.logf("Storing message %s", m.ID)
	id, err = c.storage.Store(m)
	c.messageChan <- m
	return
}

func (c *Session) logf(message string, args ...interface{}) {
	message = strings.Join([]string{"[SMTP %s]", message}, " ")
	args = append([]interface{}{c.remoteAddress}, args...)
	log.Printf(message, args...)
}

// Read reads from the underlying net.TCPConn
func (c *Session) Read() bool {
	buf := make([]byte, 1024)
	n, err := c.reader.Read(buf)

	if n == 0 {
		c.logf("Connection closed by remote host\n")
		io.Closer(c.conn).Close() // not sure this is necessary?
		return false
	}

	if err != nil {
		c.logf("Error reading from socket: %s\n", err)
		return false
	}

	text := string(buf[0:n])
	logText := strings.Replace(text, "\n", "\\n", -1)
	logText = strings.Replace(logText, "\r", "\\r", -1)
	c.logf("Received %d bytes: '%s'\n", n, logText)

	c.line += text

	for strings.Contains(c.line, "\r\n") {
		line, reply := c.proto.Parse(c.line)
		c.line = line

		if reply != nil {
			c.Write(reply)
			if reply.Status == 221 {
				io.Closer(c.conn).Close()
				return false
			}
		}
	}

	return true
}

// Write writes a reply to the underlying net.TCPConn
func (c *Session) Write(reply *smtp.Reply) {
	lines := reply.Lines()
	for _, l := range lines {
		logText := strings.Replace(l, "\n", "\\n", -1)
		logText = strings.Replace(logText, "\r", "\\r", -1)
		c.logf("Sent %d bytes: '%s'", len(l), logText)
		c.writer.Write([]byte(l))
	}

	// Done é usado pelo STARTTLS: após enviar o "220 Ready to start TLS" a
	// conexão precisa ser promovida a TLS antes da próxima leitura.
	if reply.Done != nil {
		reply.Done()
	}
}
