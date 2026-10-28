package oauth

import "fmt"

// XOAuth2Client implements sasl.Client for the XOAUTH2 mechanism (used by
// Outlook IMAP and SMTP).
type XOAuth2Client struct {
	User  string
	Token string
}

func (c *XOAuth2Client) Start() (mech string, ir []byte, err error) {
	ir = []byte(fmt.Sprintf("user=%s\x01auth=Bearer %s\x01\x01", c.User, c.Token))
	return "XOAUTH2", ir, nil
}

func (c *XOAuth2Client) Next(challenge []byte) ([]byte, error) {
	return nil, nil
}
