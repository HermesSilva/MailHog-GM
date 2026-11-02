package config

import (
	"crypto/tls"
	"encoding/json"
	"flag"
	"io/ioutil"
	"log"
	"strings"

	"github.com/ian-kent/envconf"
	"github.com/mailhog/MailHog-Server/monkey"
	"github.com/mailhog/data"
	"github.com/mailhog/storage"
)

// defaultAllowedDomains é o domínio de destinatário aceito quando nada é configurado.
const defaultAllowedDomains = "pildra.com.br"

// DefaultConfig is the default config
func DefaultConfig() *Config {
	return &Config{
		SMTPBindAddr: "0.0.0.0:1025",
		APIBindAddr:  "0.0.0.0:8025",
		Hostname:     "mailhog.example",
		MongoURI:     "127.0.0.1:27017",
		MongoDb:      "mailhog",
		MongoColl:    "messages",
		MaildirPath:  "",
		StorageType:  "memory",
		CORSOrigin:   "",
		WebPath:      "",
		MessageChan:  make(chan *data.Message),
		OutgoingSMTP: make(map[string]*OutgoingSMTP),

		SMTPSBindAddr:    "",
		SMTPAuthAllowAny: true,
		SMTPAuthFile:     "",
		SMTPRequireTLS:   false,
		SMTPTLSCert:      "",
		SMTPTLSKey:       "",
		AuthCredentials:  make(map[string]string),

		SMTPAllowedDomains: defaultAllowedDomains,
	}
}

// Config is the config, kind of
type Config struct {
	SMTPBindAddr     string
	APIBindAddr      string
	Hostname         string
	MongoURI         string
	MongoDb          string
	MongoColl        string
	StorageType      string
	CORSOrigin       string
	MaildirPath      string
	InviteJim        bool
	Storage          storage.Storage
	MessageChan      chan *data.Message
	Assets           func(asset string) ([]byte, error)
	Monkey           monkey.ChaosMonkey
	OutgoingSMTPFile string
	OutgoingSMTP     map[string]*OutgoingSMTP
	WebPath          string

	// SMTPSBindAddr, quando informado, liga um listener SMTPS (TLS implícito,
	// ex.: 0.0.0.0:1465). Vazio desabilita.
	SMTPSBindAddr string

	// --- Autenticação SMTP nível Gmail (placebo de teste) ---

	// SMTPAuthAllowAny aceita qualquer credencial em AUTH (PLAIN/LOGIN/CRAM-MD5/
	// XOAUTH2). É o modo padrão: permite substituir o Gmail sem conhecer a senha
	// real usada pela aplicação sob teste.
	SMTPAuthAllowAny bool
	// SMTPAuthFile aponta para um arquivo "user:password" por linha. Quando
	// informado, ativa o modo estrito: apenas essas credenciais são aceitas.
	SMTPAuthFile string
	// SMTPRequireTLS exige STARTTLS antes de AUTH/MAIL, como faz o Gmail.
	SMTPRequireTLS bool
	// SMTPTLSCert / SMTPTLSKey são um par PEM opcional para o servidor SMTP.
	// Se vazios, um certificado self-signed é gerado em memória.
	SMTPTLSCert string
	SMTPTLSKey  string

	// AuthCredentials é preenchido a partir de SMTPAuthFile (user -> password).
	AuthCredentials map[string]string
	// TLSConfig é montado em Configure() e usado por STARTTLS/SMTPS.
	TLSConfig *tls.Config

	// SMTPAllowedDomains lista, separados por vírgula, os domínios de
	// destinatário aceitos. Vazio desabilita o filtro.
	SMTPAllowedDomains string
	// AllowedDomains é preenchido a partir de SMTPAllowedDomains em Configure().
	AllowedDomains map[string]bool
}

// OutgoingSMTP is an outgoing SMTP server config
type OutgoingSMTP struct {
	Name      string
	Save      bool
	Email     string
	Host      string
	Port      string
	Username  string
	Password  string
	Mechanism string
}

var cfg = DefaultConfig()

// Jim is a monkey
var Jim = &monkey.Jim{}

// Configure configures stuff
func Configure() *Config {
	switch cfg.StorageType {
	case "memory":
		log.Println("Using in-memory storage")
		cfg.Storage = storage.CreateInMemory()
	case "mongodb":
		log.Println("Using MongoDB message storage")
		s := storage.CreateMongoDB(cfg.MongoURI, cfg.MongoDb, cfg.MongoColl)
		if s == nil {
			log.Println("MongoDB storage unavailable, reverting to in-memory storage")
			cfg.Storage = storage.CreateInMemory()
		} else {
			log.Println("Connected to MongoDB")
			cfg.Storage = s
		}
	case "maildir":
		log.Println("Using maildir message storage")
		s := storage.CreateMaildir(cfg.MaildirPath)
		cfg.Storage = s
	default:
		log.Fatalf("Invalid storage type %s", cfg.StorageType)
	}

	Jim.Configure(func(message string, args ...interface{}) {
		log.Printf(message, args...)
	})
	if cfg.InviteJim {
		cfg.Monkey = Jim
	}

	if len(cfg.OutgoingSMTPFile) > 0 {
		b, err := ioutil.ReadFile(cfg.OutgoingSMTPFile)
		if err != nil {
			log.Fatal(err)
		}
		var o map[string]*OutgoingSMTP
		err = json.Unmarshal(b, &o)
		if err != nil {
			log.Fatal(err)
		}
		cfg.OutgoingSMTP = o
	}

	configureSMTPAuth()
	configureSMTPTLS()
	configureAllowedDomains()

	return cfg
}

// configureAllowedDomains monta o filtro de domínios de destinatário.
func configureAllowedDomains() {
	cfg.AllowedDomains = ParseAllowedDomains(cfg.SMTPAllowedDomains)
	if len(cfg.AllowedDomains) == 0 {
		log.Println("[SMTP] Filtro de domínio desabilitado: qualquer destinatário é aceito")
		return
	}
	log.Printf("[SMTP] Filtro de domínio ativo: aceitando apenas destinatários em %s", cfg.SMTPAllowedDomains)
}

// ParseAllowedDomains converte uma lista separada por vírgula em um conjunto de
// domínios normalizados.
func ParseAllowedDomains(raw string) map[string]bool {
	domains := make(map[string]bool)
	for _, domain := range strings.Split(raw, ",") {
		if domain = normalizeDomain(domain); domain != "" {
			domains[domain] = true
		}
	}
	return domains
}

// normalizeDomain ignora espaços, caixa e o ponto final de FQDN.
func normalizeDomain(domain string) string {
	return strings.ToLower(strings.TrimSuffix(strings.TrimSpace(domain), "."))
}

// FiltersRecipients indica se o filtro de domínios de destinatário está ativo.
func (c *Config) FiltersRecipients() bool {
	return len(c.AllowedDomains) > 0
}

// RecipientAllowed indica se o destinatário pode ser aceito. O domínio é o que
// vem após o último '@' e precisa coincidir exatamente: subdomínios só são
// aceitos se forem listados.
func (c *Config) RecipientAllowed(address string) bool {
	if !c.FiltersRecipients() {
		return true
	}
	at := strings.LastIndex(address, "@")
	if at < 0 {
		return false
	}
	return c.AllowedDomains[normalizeDomain(address[at+1:])]
}

// configureSMTPAuth carrega o arquivo de credenciais (modo estrito) quando
// informado. Formato: uma linha "user:password" por credencial; linhas vazias
// e iniciadas por '#' são ignoradas.
func configureSMTPAuth() {
	if len(cfg.SMTPAuthFile) == 0 {
		if cfg.SMTPAuthAllowAny {
			log.Println("[SMTP] AUTH em modo placebo: qualquer credencial é aceita")
		}
		return
	}

	b, err := ioutil.ReadFile(cfg.SMTPAuthFile)
	if err != nil {
		log.Fatalf("[SMTP] Erro lendo arquivo de auth %s: %s", cfg.SMTPAuthFile, err)
	}

	cfg.AuthCredentials = make(map[string]string)
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			log.Fatalf("[SMTP] Linha inválida no arquivo de auth (esperado user:password): %q", line)
		}
		cfg.AuthCredentials[parts[0]] = parts[1]
	}

	// Presença de arquivo de credenciais implica modo estrito.
	cfg.SMTPAuthAllowAny = false
	log.Printf("[SMTP] AUTH em modo estrito: %d credencial(is) carregada(s)", len(cfg.AuthCredentials))
}

// configureSMTPTLS monta o tls.Config usado por STARTTLS/SMTPS. Carrega o par
// cert/key informado ou, na ausência, gera um self-signed em memória.
func configureSMTPTLS() {
	var cert tls.Certificate
	var err error

	if len(cfg.SMTPTLSCert) > 0 && len(cfg.SMTPTLSKey) > 0 {
		cert, err = tls.LoadX509KeyPair(cfg.SMTPTLSCert, cfg.SMTPTLSKey)
		if err != nil {
			log.Fatalf("[SMTP] Erro carregando cert/key TLS: %s", err)
		}
		log.Println("[SMTP] TLS usando certificado informado")
	} else {
		cert, err = generateSelfSignedCert(cfg.Hostname)
		if err != nil {
			log.Fatalf("[SMTP] Erro gerando certificado self-signed: %s", err)
		}
		log.Println("[SMTP] TLS usando certificado self-signed gerado em memória")
	}

	cfg.TLSConfig = &tls.Config{
		Certificates: []tls.Certificate{cert},
	}
}

// RegisterFlags registers flags
func RegisterFlags() {
	flag.StringVar(&cfg.SMTPBindAddr, "smtp-bind-addr", envconf.FromEnvP("MH_SMTP_BIND_ADDR", "0.0.0.0:1025").(string), "SMTP bind interface and port, e.g. 0.0.0.0:1025 or just :1025")
	flag.StringVar(&cfg.APIBindAddr, "api-bind-addr", envconf.FromEnvP("MH_API_BIND_ADDR", "0.0.0.0:8025").(string), "HTTP bind interface and port for API, e.g. 0.0.0.0:8025 or just :8025")
	flag.StringVar(&cfg.Hostname, "hostname", envconf.FromEnvP("MH_HOSTNAME", "mailhog.example").(string), "Hostname for EHLO/HELO response, e.g. mailhog.example")
	flag.StringVar(&cfg.StorageType, "storage", envconf.FromEnvP("MH_STORAGE", "memory").(string), "Message storage: 'memory' (default), 'mongodb' or 'maildir'")
	flag.StringVar(&cfg.MongoURI, "mongo-uri", envconf.FromEnvP("MH_MONGO_URI", "127.0.0.1:27017").(string), "MongoDB URI, e.g. 127.0.0.1:27017")
	flag.StringVar(&cfg.MongoDb, "mongo-db", envconf.FromEnvP("MH_MONGO_DB", "mailhog").(string), "MongoDB database, e.g. mailhog")
	flag.StringVar(&cfg.MongoColl, "mongo-coll", envconf.FromEnvP("MH_MONGO_COLLECTION", "messages").(string), "MongoDB collection, e.g. messages")
	flag.StringVar(&cfg.CORSOrigin, "cors-origin", envconf.FromEnvP("MH_CORS_ORIGIN", "").(string), "CORS Access-Control-Allow-Origin header for API endpoints")
	flag.StringVar(&cfg.MaildirPath, "maildir-path", envconf.FromEnvP("MH_MAILDIR_PATH", "").(string), "Maildir path (if storage type is 'maildir')")
	flag.BoolVar(&cfg.InviteJim, "invite-jim", envconf.FromEnvP("MH_INVITE_JIM", false).(bool), "Decide whether to invite Jim (beware, he causes trouble)")
	flag.StringVar(&cfg.OutgoingSMTPFile, "outgoing-smtp", envconf.FromEnvP("MH_OUTGOING_SMTP", "").(string), "JSON file containing outgoing SMTP servers")

	flag.StringVar(&cfg.SMTPSBindAddr, "smtps-bind-addr", envconf.FromEnvP("MH_SMTPS_BIND_ADDR", "").(string), "SMTPS (implicit TLS) bind interface and port, e.g. 0.0.0.0:1465. Empty disables it")
	flag.BoolVar(&cfg.SMTPAuthAllowAny, "smtp-auth-allow-any", envconf.FromEnvP("MH_SMTP_AUTH_ALLOW_ANY", true).(bool), "Accept any SMTP AUTH credentials (Gmail placebo mode). Ignored when -smtp-auth-file is set")
	flag.StringVar(&cfg.SMTPAuthFile, "smtp-auth-file", envconf.FromEnvP("MH_SMTP_AUTH_FILE", "").(string), "File with 'user:password' per line to enable strict SMTP AUTH")
	flag.BoolVar(&cfg.SMTPRequireTLS, "smtp-require-tls", envconf.FromEnvP("MH_SMTP_REQUIRE_TLS", false).(bool), "Require STARTTLS before AUTH/MAIL, like Gmail")
	flag.StringVar(&cfg.SMTPTLSCert, "smtp-tls-cert", envconf.FromEnvP("MH_SMTP_TLS_CERT", "").(string), "Path to PEM certificate for SMTP TLS (self-signed generated if empty)")
	flag.StringVar(&cfg.SMTPTLSKey, "smtp-tls-key", envconf.FromEnvP("MH_SMTP_TLS_KEY", "").(string), "Path to PEM private key for SMTP TLS (self-signed generated if empty)")

	flag.StringVar(&cfg.SMTPAllowedDomains, "smtp-allowed-domains", envconf.FromEnvP("MH_SMTP_ALLOWED_DOMAINS", defaultAllowedDomains).(string), "Comma-separated recipient domains to accept; other recipients are rejected. Empty disables the filter")

	Jim.RegisterFlags()
}
