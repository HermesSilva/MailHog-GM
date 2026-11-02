package smtp

import (
	"errors"
	"strings"
	"sync"
	"testing"

	. "github.com/smartystreets/goconvey/convey"

	"github.com/mailhog/MailHog-Server/config"
	"github.com/mailhog/data"
	"github.com/mailhog/storage"
)

type fakeRw struct {
	_read  func(p []byte) (n int, err error)
	_write func(p []byte) (n int, err error)
	_close func() error
}

func (rw *fakeRw) Read(p []byte) (n int, err error) {
	if rw._read != nil {
		return rw._read(p)
	}
	return 0, nil
}
func (rw *fakeRw) Close() error {
	if rw._close != nil {
		return rw._close()
	}
	return nil
}
func (rw *fakeRw) Write(p []byte) (n int, err error) {
	if rw._write != nil {
		return rw._write(p)
	}
	return len(p), nil
}

func TestAccept(t *testing.T) {
	Convey("Accept should handle a connection", t, func() {
		frw := &fakeRw{}
		mChan := make(chan *data.Message)
		Accept("1.1.1.1:11111", frw, storage.CreateInMemory(), mChan, "localhost", nil, nil)
	})
}

func TestSocketError(t *testing.T) {
	Convey("Socket errors should return from Accept", t, func() {
		frw := &fakeRw{
			_read: func(p []byte) (n int, err error) {
				return -1, errors.New("OINK")
			},
		}
		mChan := make(chan *data.Message)
		Accept("1.1.1.1:11111", frw, storage.CreateInMemory(), mChan, "localhost", nil, nil)
	})
}

func TestAcceptMessage(t *testing.T) {
	Convey("acceptMessage should be called", t, func() {
		mbuf := "EHLO localhost\nMAIL FROM:<test>\nRCPT TO:<test>\nDATA\nHi.\r\n.\r\nQUIT\n"
		var rbuf []byte
		frw := &fakeRw{
			_read: func(p []byte) (n int, err error) {
				if len(p) >= len(mbuf) {
					ba := []byte(mbuf)
					mbuf = ""
					for i, b := range ba {
						p[i] = b
					}
					return len(ba), nil
				}

				ba := []byte(mbuf[0:len(p)])
				mbuf = mbuf[len(p):]
				for i, b := range ba {
					p[i] = b
				}
				return len(ba), nil
			},
			_write: func(p []byte) (n int, err error) {
				rbuf = append(rbuf, p...)
				return len(p), nil
			},
			_close: func() error {
				return nil
			},
		}
		mChan := make(chan *data.Message)
		var wg sync.WaitGroup
		wg.Add(1)
		handlerCalled := false
		go func() {
			handlerCalled = true
			<-mChan
			//FIXME breaks some tests (in drone.io)
			//m := <-mChan
			//So(m, ShouldNotBeNil)
			wg.Done()
		}()
		Accept("1.1.1.1:11111", frw, storage.CreateInMemory(), mChan, "localhost", nil, nil)
		wg.Wait()
		So(handlerCalled, ShouldBeTrue)
	})
}

func TestValidateAuthentication(t *testing.T) {
	Convey("validateAuthentication is always successful", t, func() {
		c := &Session{}

		err, ok := c.validateAuthentication("OINK")
		So(err, ShouldBeNil)
		So(ok, ShouldBeTrue)

		err, ok = c.validateAuthentication("OINK", "arg1")
		So(err, ShouldBeNil)
		So(ok, ShouldBeTrue)

		err, ok = c.validateAuthentication("OINK", "arg1", "arg2")
		So(err, ShouldBeNil)
		So(ok, ShouldBeTrue)
	})
}

func TestValidateRecipient(t *testing.T) {
	Convey("validateRecipient is always successful", t, func() {
		c := &Session{}

		So(c.validateRecipient("OINK"), ShouldBeTrue)
		So(c.validateRecipient("foo@bar.mailhog"), ShouldBeTrue)
	})
}

// Os testes do filtro usam testing puro: o So do goconvey vendorizado entra em
// pânico nas versões atuais do Go.
func TestValidateRecipientDomainFilter(t *testing.T) {
	c := &Session{remoteAddress: "1.1.1.1:11111", config: &config.Config{
		AllowedDomains: config.ParseAllowedDomains(" Pildra.com.br , outro.test "),
	}}

	cases := map[string]bool{
		"foo@pildra.com.br":           true,
		"FOO@PILDRA.COM.BR":           true,
		"foo@pildra.com.br.":          true,
		"foo@outro.test":              true,
		"foo@gmail.com":               false,
		"foo@sub.pildra.com.br":       false,
		"foo@pildra.com.br.evil.test": false,
		"foo@notpildra.com.br":        false,
		"foo@pildra.com.br@evil.test": false,
		"pildra.com.br":               false,
	}
	for address, expected := range cases {
		if got := c.validateRecipient(address); got != expected {
			t.Errorf("validateRecipient(%q) = %v, esperado %v", address, got, expected)
		}
	}
}

func TestValidateRecipientFilterDisabled(t *testing.T) {
	c := &Session{config: &config.Config{AllowedDomains: config.ParseAllowedDomains("")}}

	if !c.validateRecipient("foo@gmail.com") {
		t.Error("lista vazia deveria desabilitar o filtro")
	}
}

// runSession executa uma sessão SMTP completa e devolve as respostas do servidor
// e as mensagens armazenadas.
func runSession(t *testing.T, script string) (string, []data.Message) {
	t.Helper()

	var replies []byte
	frw := &fakeRw{
		_read: func(p []byte) (int, error) {
			n := copy(p, script)
			script = script[n:]
			return n, nil
		},
		_write: func(p []byte) (int, error) {
			replies = append(replies, p...)
			return len(p), nil
		},
	}
	store := storage.CreateInMemory()
	cfg := &config.Config{AllowedDomains: config.ParseAllowedDomains("pildra.com.br")}
	Accept("1.1.1.1:11111", frw, store, make(chan *data.Message, 1), "localhost", nil, cfg)

	messages, err := store.List(0, 10)
	if err != nil {
		t.Fatal(err)
	}
	return string(replies), []data.Message(*messages)
}

func TestDomainFilterSession(t *testing.T) {
	const hello = "EHLO localhost\r\nMAIL FROM:<app@gmail.com>\r\n"
	const body = "DATA\r\nHi.\r\n.\r\nQUIT\r\n"

	t.Run("destinatario permitido e armazenado, qualquer que seja o remetente", func(t *testing.T) {
		replies, messages := runSession(t, hello+"RCPT TO:<foo@pildra.com.br>\r\n"+body)

		if !strings.Contains(replies, "250 Ok: queued as") {
			t.Errorf("mensagem deveria ser aceita: %q", replies)
		}
		if len(messages) != 1 {
			t.Errorf("esperava 1 mensagem, obteve %d", len(messages))
		}
	})

	t.Run("destinatario recusado responde 550 e DATA nao armazena", func(t *testing.T) {
		replies, messages := runSession(t, hello+"RCPT TO:<foo@gmail.com>\r\n"+body)

		if !strings.Contains(replies, "550 Invalid recipient foo@gmail.com") {
			t.Errorf("esperava 550: %q", replies)
		}
		if strings.Contains(replies, "queued as") {
			t.Errorf("mensagem nao deveria ser aceita: %q", replies)
		}
		if len(messages) != 0 {
			t.Errorf("esperava 0 mensagens, obteve %d", len(messages))
		}
	})

	t.Run("DATA sem RCPT nao armazena", func(t *testing.T) {
		replies, messages := runSession(t, hello+body)

		if strings.Contains(replies, "queued as") {
			t.Errorf("mensagem nao deveria ser aceita: %q", replies)
		}
		if len(messages) != 0 {
			t.Errorf("esperava 0 mensagens, obteve %d", len(messages))
		}
	})

	t.Run("destinatarios mistos mantem apenas os permitidos", func(t *testing.T) {
		_, messages := runSession(t, hello+"RCPT TO:<foo@gmail.com>\r\nRCPT TO:<bar@pildra.com.br>\r\n"+body)

		if len(messages) != 1 {
			t.Fatalf("esperava 1 mensagem, obteve %d", len(messages))
		}
		if to := messages[0].To; len(to) != 1 || to[0].Domain != "pildra.com.br" {
			t.Errorf("destinatarios inesperados: %+v", to)
		}
	})
}

func TestValidateSender(t *testing.T) {
	Convey("validateSender is always successful", t, func() {
		c := &Session{}

		So(c.validateSender("OINK"), ShouldBeTrue)
		So(c.validateSender("foo@bar.mailhog"), ShouldBeTrue)
	})
}
