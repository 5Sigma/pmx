package mock

import (
	"log"
	"net"
	"os"
	"reflect"
	"unsafe"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	gosmtp "github.com/emersion/go-smtp"
)

// Options configures the mock mail server pair.
type Options struct {
	IMAPAddr string // default "127.0.0.1:1143"
	SMTPAddr string // default "127.0.0.1:1025"
	User     string // default "user"
	Pass     string // default "pass"
	Logger   *log.Logger
}

// Server holds running IMAP and SMTP listeners.
type Server struct {
	imap   *imapserver.Server
	smtp   *gosmtp.Server
	imapLn net.Listener
	smtpLn net.Listener

	// User is the in-memory user created during Start. Tests may call
	// User.Append to inject messages without going through SMTP.
	User *imapmemserver.User
}

// Start builds the in-memory mail store, seeds fixture messages, and starts
// both the IMAP and SMTP servers on their respective listeners.
func Start(opts Options) (*Server, error) {
	if opts.IMAPAddr == "" {
		opts.IMAPAddr = "127.0.0.1:1143"
	}
	if opts.SMTPAddr == "" {
		opts.SMTPAddr = "127.0.0.1:1025"
	}
	if opts.User == "" {
		opts.User = "user"
	}
	if opts.Pass == "" {
		opts.Pass = "pass"
	}
	if opts.Logger == nil {
		opts.Logger = log.New(os.Stderr, "[mock] ", log.LstdFlags)
	}

	// Build in-memory IMAP store with the requested mailboxes.
	memSrv := imapmemserver.New()
	u := imapmemserver.NewUser(opts.User, opts.Pass)
	memSrv.AddUser(u)

	// User.Create is the correct way to add mailboxes; it records them in the
	// user's internal map so that User.Append can find them by name. SPECIAL-USE
	// attrs are set so that integration tests can exercise IMAP SPECIAL-USE
	// discovery (RFC 6154) without a real server.
	seedMailboxes := []struct {
		name  string
		attrs []imap.MailboxAttr
	}{
		{"INBOX", nil},
		{"Sent", []imap.MailboxAttr{imap.MailboxAttrSent}},
		{"Drafts", []imap.MailboxAttr{imap.MailboxAttrDrafts}},
		{"Archive", []imap.MailboxAttr{imap.MailboxAttrArchive}},
		{"Trash", []imap.MailboxAttr{imap.MailboxAttrTrash}},
		{"Junk", []imap.MailboxAttr{imap.MailboxAttrJunk}},
	}
	for _, m := range seedMailboxes {
		if err := u.Create(m.name, nil); err != nil {
			return nil, err
		}
		if len(m.attrs) > 0 {
			if err := setMailboxSpecialUse(u, m.name, m.attrs); err != nil {
				return nil, err
			}
		}
	}

	if err := Seed(u); err != nil {
		return nil, err
	}

	// The imapmemserver.Server.NewSession signature takes no arguments, so we
	// wrap it in a closure matching the imapserver.Options.NewSession shape.
	imapSrv := imapserver.New(&imapserver.Options{
		NewSession: func(c *imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return memSrv.NewSession(), nil, nil
		},
		InsecureAuth: true,
		// Advertise SPECIAL-USE so clients can request RETURN (SPECIAL-USE) in
		// LIST. The imapmemserver backend already supports the extension; this
		// just makes the server announce it in CAPABILITY responses.
		Caps: imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapSpecialUse: {}},
	})

	imapLn, err := net.Listen("tcp", opts.IMAPAddr)
	if err != nil {
		return nil, err
	}

	smtpSrv := gosmtp.NewServer(newSMTPBackend(u, opts.Logger))
	smtpSrv.AllowInsecureAuth = true
	smtpSrv.Domain = "localhost"

	smtpLn, err := net.Listen("tcp", opts.SMTPAddr)
	if err != nil {
		_ = imapLn.Close()
		return nil, err
	}

	s := &Server{
		imap:   imapSrv,
		smtp:   smtpSrv,
		imapLn: imapLn,
		smtpLn: smtpLn,
		User:   u,
	}

	go func() {
		if err := imapSrv.Serve(imapLn); err != nil {
			opts.Logger.Printf("imap serve error: %v", err)
		}
	}()
	go func() {
		if err := smtpSrv.Serve(smtpLn); err != nil {
			opts.Logger.Printf("smtp serve error: %v", err)
		}
	}()

	opts.Logger.Printf("IMAP listening on %s", imapLn.Addr())
	opts.Logger.Printf("SMTP listening on %s", smtpLn.Addr())

	return s, nil
}

// IMAPAddr returns the address the IMAP server is listening on.
func (s *Server) IMAPAddr() string { return s.imapLn.Addr().String() }

// SMTPAddr returns the address the SMTP server is listening on.
func (s *Server) SMTPAddr() string { return s.smtpLn.Addr().String() }

// Close shuts down both servers.
func (s *Server) Close() error {
	imapErr := s.imap.Close()
	smtpErr := s.smtp.Close()
	if imapErr != nil {
		return imapErr
	}
	return smtpErr
}

// setMailboxSpecialUse sets the RFC 6154 SPECIAL-USE attributes on an
// already-created mailbox. imapmemserver.User.Create accepts a CreateOptions
// argument but silently ignores the SpecialUse field (library bug in
// go-imap v2.0.0-beta.8), so we reach into the unexported fields via
// reflect+unsafe to wire up the attributes after the fact. This is
// intentionally scoped to the mock package; no production code should need it.
func setMailboxSpecialUse(u *imapmemserver.User, name string, attrs []imap.MailboxAttr) error {
	// Reach into User.mailboxes (unexported map[string]*Mailbox).
	uv := reflect.ValueOf(u).Elem()
	mailboxesField := uv.FieldByName("mailboxes")
	//nolint:gosec — intentional unsafe access to mock-only test helper
	mailboxesPtr := (*map[string]*imapmemserver.Mailbox)(unsafe.Pointer(mailboxesField.UnsafeAddr()))

	mbox, ok := (*mailboxesPtr)[name]
	if !ok {
		return &imap.Error{
			Type: imap.StatusResponseTypeNo,
			Text: "setMailboxSpecialUse: mailbox not found: " + name,
		}
	}

	// Set Mailbox.specialUse (unexported []imap.MailboxAttr).
	mv := reflect.ValueOf(mbox).Elem()
	specialUseField := mv.FieldByName("specialUse")
	//nolint:gosec — intentional unsafe access to mock-only test helper
	specialUsePtr := (*[]imap.MailboxAttr)(unsafe.Pointer(specialUseField.UnsafeAddr()))
	*specialUsePtr = attrs
	return nil
}
