package handlers

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
	"io"
	"io/ioutil"
	"bytes"
	"strconv"

	"github.com/base58btc/btcpp-web/internal/config"
	"github.com/base58btc/btcpp-web/internal/types"
	mailer "github.com/base58btc/mailer/mail"
)

/* Send a request to our mailer to send a ticket at time */
func SendTickets(ctx *config.AppContext, tickets []*types.Ticket, email string, sendAt time.Time) error {
	/* Send the ticket email! */
	var htmlBody bytes.Buffer
	err := ctx.TemplateCache["register"].Execute(io.Writer(&htmlBody), &EmailTmpl{
		URI: ctx.Env.GetURI(),
		CSS: MiniCss(),
	})
	if err != nil {
		return err
	}

	if len(tickets) == 0 {
		return fmt.Errorf("No tickets present!")
	}

	/*
	textBody, err := ctx.TemplateCache["register-text"].ExecuteBytes(&EmailTmpl{
		Domain: ctx.Env.GetURI(),
	})
	if err != nil {
		return err
	}
	*/

	var attaches mailer.AttachSet
	attaches = make([]*mailer.Attachment, len(tickets))
	for i, ticket:= range tickets {
		attaches[i] = &mailer.Attachment{
			Content: ticket.Pdf,
			Type: "application/pdf",
			Name: fmt.Sprintf("btcpp23_ticket_%s.pdf", ticket.Id[:6]),
		}
	}

	ticketJob := tickets[0].Id
	if ticketJob == "testticket" {
		ticketJob = ticketJob + strconv.Itoa(int(sendAt.UTC().Unix()))
	}
	/* Build a mail to send */
	mail := &mailer.MailRequest{
		JobKey: fmt.Sprintf("%s-%s", "btcpp23", ticketJob),
		ToAddr: email,
		FromAddr: "hello@btcpp.dev",
		FromName: "bitcoin++",
		Title: "[bitcoin++ Ticket] You're Going! ... Austin, April 28-30",
		HTMLBody: htmlBody.String(),
		TextBody: "TODO: this!",
		Attachments: attaches,
		SendAt: float64(sendAt.UTC().Unix()),
	}

	return SendMailRequest(ctx, mail)
}

func makeAuthStamp(secret string, timestamp string, r *http.Request) string {
	h := sha256.New()
	h.Write([]byte(secret))
	h.Write([]byte(timestamp))
	h.Write([]byte(r.URL.Path))
	h.Write([]byte(r.Method))
	return hex.EncodeToString(h.Sum(nil))
}

func SendMailRequest(ctx *config.AppContext, mail *mailer.MailRequest) error {
	/* Send as a PUT request w/ JSON body */
	payload, err := json.Marshal(mail)
	if err != nil {
		return err
	}

	client := &http.Client{}

	url := "http://104.131.77.55:9998/job"
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewBuffer(payload))
	if err != nil {
		return err
	}

	timestamp := strconv.Itoa(int(time.Now().UTC().Unix()))
	secret := ctx.Env.MailerSecret
	authStamp := makeAuthStamp(secret, timestamp, req)

	req.Header.Set("Authorization", authStamp)
	req.Header.Set("X-Base58-Timestamp", timestamp)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close()

	var ret mailer.ReturnVal
	data, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(data, &ret); err != nil {
		return err
	}

	if !ret.Success {
		return fmt.Errorf("Unable to schedule mail: %s", ret.Message)
	}
	return nil
}

