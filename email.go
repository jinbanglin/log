package log

import (
    "net/smtp"
    "github.com/spf13/viper"
    "strings"
    "github.com/jinbanglin/bytebufferpool"
)

type Email struct {
    UserName    string
    Host        string
    Port        string
    Password    string
    To          []string
    ToString    string
    EmailAlias  string
    Subject     string
    buf         chan string
    ContentType []byte
    auth        smtp.Auth
}

func (e *Email) SendMail(content string) {
    e.buf <- content
}

func (e *Email) sendMail(content string) error {
    b := bytebufferpool.Get()
    b.WriteString("To: " + e.ToString + "\r\n")
    b.WriteString("From: " + e.EmailAlias + "<" + e.UserName + ">\r\n")
    b.WriteString("Subject: " + e.Subject + "\r\n")
    b.Write(e.ContentType)
    b.WriteString("\r\n\r\n")
    b.WriteString(content)
    err := smtp.SendMail(e.Host+e.Port, e.auth, e.UserName, e.To, b.Bytes())
    b.Release()
    return err
}

var gEmail *Email

func EmailInstance() *Email {
    if gEmail == nil {
        to := viper.GetString("email.to")
        gEmail = &Email{
            UserName:    viper.GetString("email.user_name"),
            Host:        viper.GetString("email.host"),
            Port:        viper.GetString("email.port"),
            Password:    viper.GetString("email.password"),
            To:          strings.Split(to, ";"),
            EmailAlias:  viper.GetString("email.email_alias"),
            Subject:     viper.GetString("email.subject"),
            ToString:    to,
            ContentType: []byte(`Content-Type: text/plain; charset=UTF-8`),
            buf:         make(chan string, 1024),
        }
        gEmail.auth = smtp.PlainAuth(gEmail.Password, gEmail.UserName, gEmail.Password, gEmail.Host)
        go gEmail.run()
    }
    return gEmail
}

func (e *Email) run() {
    for {
        select {
        case data := <-e.buf:
            e.sendMail(data)
        }
    }
}
